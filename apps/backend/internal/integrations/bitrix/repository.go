package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) ClaimOutbox(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	maxAttempts int,
) (OutboxEvent, bool, error) {
	var event OutboxEvent
	err := repository.database.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT id
			FROM integration_outbox
			WHERE
				attempts < $3
				AND (
					(status IN ('pending', 'failed') AND next_attempt_at <= $1)
					OR (status = 'processing' AND locked_at <= $2)
				)
			ORDER BY
				CASE event_type
					WHEN 'bitrix.contact.upsert' THEN 0
					WHEN 'bitrix.deal.create' THEN 1
					ELSE 2
				END,
				next_attempt_at,
				created_at,
				id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE integration_outbox AS event
		SET
			status = 'processing',
			attempts = event.attempts + 1,
			locked_at = $1
		FROM candidate
		WHERE event.id = candidate.id
		RETURNING
			event.id::text,
			event.event_type,
			event.aggregate_id::text,
			event.payload,
			event.attempts,
			event.next_attempt_at`,
		now,
		staleBefore,
		maxAttempts,
	).Scan(
		&event.ID,
		&event.EventType,
		&event.AggregateID,
		&event.Payload,
		&event.Attempts,
		&event.NextAttemptAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboxEvent{}, false, nil
	}
	if err != nil {
		return OutboxEvent{}, false, fmt.Errorf("claim Bitrix outbox event: %w", err)
	}
	return event, true, nil
}

func (repository *PostgresRepository) CompleteOutbox(
	ctx context.Context,
	eventID string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE integration_outbox
		 SET
			status = 'completed',
			locked_at = NULL,
			last_error = NULL,
			processed_at = $2
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		now,
	)
	if err != nil {
		return fmt.Errorf("complete Bitrix outbox event: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RetryOutbox(
	ctx context.Context,
	eventID string,
	nextAttemptAt time.Time,
	lastError string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE integration_outbox
		 SET
			status = 'failed',
			next_attempt_at = $2,
			locked_at = NULL,
			last_error = $3
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		nextAttemptAt,
		truncateError(lastError),
	)
	if err != nil {
		return fmt.Errorf("schedule Bitrix outbox retry: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) FailOutbox(
	ctx context.Context,
	eventID string,
	maxAttempts int,
	lastError string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE integration_outbox
		 SET
			status = 'failed',
			attempts = GREATEST(attempts, $2),
			locked_at = NULL,
			last_error = $3
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		maxAttempts,
		truncateError(lastError),
	)
	if err != nil {
		return fmt.Errorf("fail Bitrix outbox event: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) GetClientSyncData(
	ctx context.Context,
	clientID string,
) (ClientSyncData, error) {
	var data ClientSyncData
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			id::text,
			COALESCE(full_name, ''),
			phone,
			COALESCE(bitrix_contact_id, '')
		 FROM clients
		 WHERE id = $1::uuid`,
		clientID,
	).Scan(&data.ClientID, &data.FullName, &data.Phone, &data.ContactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientSyncData{}, fmt.Errorf("Bitrix client %s not found", clientID)
	}
	if err != nil {
		return ClientSyncData{}, fmt.Errorf("get Bitrix client sync data: %w", err)
	}
	return data, nil
}

func (repository *PostgresRepository) SaveContactID(
	ctx context.Context,
	clientID string,
	contactID string,
) error {
	commandTag, err := repository.database.Exec(
		ctx,
		`UPDATE clients
		 SET bitrix_contact_id = $2, updated_at = NOW()
		 WHERE
			id = $1::uuid
			AND (bitrix_contact_id IS NULL OR bitrix_contact_id = $2)`,
		clientID,
		contactID,
	)
	if err != nil {
		return fmt.Errorf("save Bitrix contact id: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("client already has a different Bitrix contact id")
	}
	return nil
}

func (repository *PostgresRepository) GetRentalSyncData(
	ctx context.Context,
	rentalID string,
) (RentalSyncData, error) {
	var data RentalSyncData
	var address pgtype.Text
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			rr.id::text,
			c.id::text,
			COALESCE(c.full_name, ''),
			c.phone,
			COALESCE(c.bitrix_contact_id, ''),
			COALESCE(rr.bitrix_deal_id, ''),
			t.name,
			rr.start_date::text,
			rr.end_date::text,
			rr.rental_price,
			rr.deposit_amount,
			rr.delivery_cost,
			rr.total_amount,
			rr.delivery_method,
			rr.delivery_address,
			rr.status
		 FROM rental_requests AS rr
		 JOIN clients AS c ON c.id = rr.client_id
		 JOIN tools AS t ON t.id = rr.tool_id
		 WHERE rr.id = $1::uuid`,
		rentalID,
	).Scan(
		&data.RentalID,
		&data.ClientID,
		&data.ClientFullName,
		&data.ClientPhone,
		&data.ContactID,
		&data.DealID,
		&data.ToolName,
		&data.StartDate,
		&data.EndDate,
		&data.RentalPrice,
		&data.DepositAmount,
		&data.DeliveryCost,
		&data.TotalAmount,
		&data.DeliveryMethod,
		&address,
		&data.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalSyncData{}, fmt.Errorf("Bitrix rental %s not found", rentalID)
	}
	if err != nil {
		return RentalSyncData{}, fmt.Errorf("get Bitrix rental sync data: %w", err)
	}
	if address.Valid {
		data.DeliveryAddress = address.String
	}
	return data, nil
}

func (repository *PostgresRepository) SaveDealID(
	ctx context.Context,
	rentalID string,
	dealID string,
) error {
	commandTag, err := repository.database.Exec(
		ctx,
		`UPDATE rental_requests
		 SET bitrix_deal_id = $2, updated_at = NOW()
		 WHERE
			id = $1::uuid
			AND (bitrix_deal_id IS NULL OR bitrix_deal_id = $2)`,
		rentalID,
		dealID,
	)
	if err != nil {
		return fmt.Errorf("save Bitrix deal id: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("rental already has a different Bitrix deal id")
	}
	return nil
}

func (repository *PostgresRepository) StoreInboundEvent(
	ctx context.Context,
	eventKey string,
	dealID string,
	rawPayload json.RawMessage,
	now time.Time,
) (string, bool, error) {
	var eventID string
	err := repository.database.QueryRow(
		ctx,
		`INSERT INTO bitrix_inbound_events (
			event_key,
			bitrix_deal_id,
			raw_payload,
			status,
			next_attempt_at,
			created_at
		)
		VALUES ($1, $2, $3::jsonb, 'pending', $4, $4)
		ON CONFLICT (event_key) DO NOTHING
		RETURNING id::text`,
		eventKey,
		dealID,
		rawPayload,
		now,
	).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = repository.database.QueryRow(
			ctx,
			`SELECT id::text FROM bitrix_inbound_events WHERE event_key = $1`,
			eventKey,
		).Scan(&eventID)
		if err != nil {
			return "", false, fmt.Errorf("get duplicate Bitrix inbound event: %w", err)
		}
		return eventID, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store Bitrix inbound event: %w", err)
	}
	return eventID, true, nil
}

func (repository *PostgresRepository) ClaimInbound(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	maxAttempts int,
) (InboundEvent, bool, error) {
	var event InboundEvent
	err := repository.database.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT event.id
			FROM bitrix_inbound_events AS event
			WHERE
				event.attempts < $3
				AND (
					(event.status IN ('pending', 'failed') AND event.next_attempt_at <= $1)
					OR (event.status = 'processing' AND event.locked_at <= $2)
				)
				AND NOT EXISTS (
					SELECT 1
					FROM bitrix_inbound_events AS earlier
					WHERE
						earlier.bitrix_deal_id = event.bitrix_deal_id
						AND earlier.status IN ('pending', 'processing', 'failed')
						AND (earlier.created_at, earlier.id) < (event.created_at, event.id)
				)
				AND NOT EXISTS (
					SELECT 1
					FROM bitrix_inbound_events AS active
					WHERE
						active.bitrix_deal_id = event.bitrix_deal_id
						AND active.id <> event.id
						AND active.status = 'processing'
						AND active.locked_at > $2
				)
			ORDER BY event.next_attempt_at, event.created_at, event.id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE bitrix_inbound_events AS event
		SET
			status = 'processing',
			attempts = event.attempts + 1,
			locked_at = $1
		FROM candidate
		WHERE event.id = candidate.id
		RETURNING
			event.id::text,
			event.event_key,
			event.bitrix_deal_id,
			event.raw_payload,
			event.attempts`,
		now,
		staleBefore,
		maxAttempts,
	).Scan(
		&event.ID,
		&event.EventKey,
		&event.BitrixDealID,
		&event.RawPayload,
		&event.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InboundEvent{}, false, nil
	}
	if err != nil {
		return InboundEvent{}, false, fmt.Errorf("claim Bitrix inbound event: %w", err)
	}
	return event, true, nil
}

func (repository *PostgresRepository) CompleteInbound(
	ctx context.Context,
	eventID string,
	rentalID string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE bitrix_inbound_events
		 SET
			status = 'completed',
			rental_request_id = $2::uuid,
			locked_at = NULL,
			last_error = NULL,
			processed_at = $3
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		rentalID,
		now,
	)
	if err != nil {
		return fmt.Errorf("complete Bitrix inbound event: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RetryInbound(
	ctx context.Context,
	eventID string,
	nextAttemptAt time.Time,
	lastError string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE bitrix_inbound_events
		 SET
			status = 'failed',
			next_attempt_at = $2,
			locked_at = NULL,
			last_error = $3
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		nextAttemptAt,
		truncateError(lastError),
	)
	if err != nil {
		return fmt.Errorf("schedule Bitrix inbound retry: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RejectInbound(
	ctx context.Context,
	eventID string,
	maxAttempts int,
	lastError string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE bitrix_inbound_events
		 SET
			status = 'rejected',
			attempts = GREATEST(attempts, $2),
			locked_at = NULL,
			last_error = $3,
			processed_at = $4
		 WHERE id = $1::uuid AND status = 'processing'`,
		eventID,
		maxAttempts,
		truncateError(lastError),
		now,
	)
	if err != nil {
		return fmt.Errorf("reject Bitrix inbound event: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) Reconcile(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin Bitrix reconciliation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var count int64
	err = transaction.QueryRow(
		ctx,
		`WITH contact_events AS (
			INSERT INTO integration_outbox (
				event_type,
				aggregate_type,
				aggregate_id,
				dedupe_key,
				payload,
				status,
				attempts,
				next_attempt_at,
				locked_at,
				last_error,
				created_at
			)
			SELECT DISTINCT
				'bitrix.contact.upsert',
				'client',
				rr.client_id,
				'bitrix.contact.upsert:' || rr.client_id::text,
				jsonb_build_object('client_id', rr.client_id),
				'pending',
				0,
				$1::timestamptz,
				NULL::timestamptz,
				NULL::text,
				$1::timestamptz
			FROM rental_requests AS rr
			JOIN clients AS c ON c.id = rr.client_id
			WHERE c.bitrix_contact_id IS NULL
			ON CONFLICT (dedupe_key) DO UPDATE SET
				status = CASE
					WHEN integration_outbox.status = 'completed' THEN 'completed'
					ELSE 'pending'
				END,
				attempts = CASE
					WHEN integration_outbox.status = 'completed' THEN integration_outbox.attempts
					ELSE 0
				END,
				next_attempt_at = CASE
					WHEN integration_outbox.status = 'completed'
						THEN integration_outbox.next_attempt_at
					ELSE $1::timestamptz
				END,
				locked_at = NULL,
				last_error = CASE
					WHEN integration_outbox.status = 'completed'
						THEN integration_outbox.last_error
					ELSE NULL
				END
			RETURNING 1
		),
		deal_create_events AS (
			INSERT INTO integration_outbox (
				event_type,
				aggregate_type,
				aggregate_id,
				dedupe_key,
				payload,
				status,
				attempts,
				next_attempt_at,
				locked_at,
				last_error,
				created_at
			)
			SELECT
				'bitrix.deal.create',
				'rental_request',
				rr.id,
				'bitrix.deal.create:' || rr.id::text,
				jsonb_build_object('rental_id', rr.id),
				'pending',
				0,
				$1::timestamptz,
				NULL::timestamptz,
				NULL::text,
				$1::timestamptz
			FROM rental_requests AS rr
			WHERE rr.bitrix_deal_id IS NULL
			ON CONFLICT (dedupe_key) DO UPDATE SET
				status = CASE
					WHEN integration_outbox.status = 'completed' THEN 'completed'
					ELSE 'pending'
				END,
				attempts = CASE
					WHEN integration_outbox.status = 'completed' THEN integration_outbox.attempts
					ELSE 0
				END,
				next_attempt_at = CASE
					WHEN integration_outbox.status = 'completed'
						THEN integration_outbox.next_attempt_at
					ELSE $1::timestamptz
				END,
				locked_at = NULL,
				last_error = CASE
					WHEN integration_outbox.status = 'completed'
						THEN integration_outbox.last_error
					ELSE NULL
				END
			RETURNING 1
		),
		deal_update_events AS (
			INSERT INTO integration_outbox (
				event_type,
				aggregate_type,
				aggregate_id,
				dedupe_key,
				payload,
				status,
				next_attempt_at,
				created_at
			)
			SELECT
				'bitrix.deal.update',
				'rental_request',
				rr.id,
				'bitrix.deal.update:' || rr.id::text || ':' || rr.status,
				jsonb_build_object('rental_id', rr.id, 'status', rr.status),
				'pending',
				$1::timestamptz,
				$1::timestamptz
			FROM rental_requests AS rr
			WHERE rr.bitrix_deal_id IS NOT NULL
			ON CONFLICT (dedupe_key) DO NOTHING
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM contact_events)
			+ (SELECT COUNT(*) FROM deal_create_events)
			+ (SELECT COUNT(*) FROM deal_update_events)`,
		now,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("enqueue Bitrix reconciliation: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit Bitrix reconciliation: %w", err)
	}
	return count, nil
}

func truncateError(message string) string {
	message = strings.TrimSpace(message)
	if len([]rune(message)) <= 2000 {
		return message
	}
	return string([]rune(message)[:2000])
}

package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

func (repository *PostgresRepository) FindClientIDByBitrixContact(
	ctx context.Context,
	contactID string,
	phone string,
) (string, error) {
	contactID = strings.TrimSpace(contactID)
	phone = strings.TrimSpace(phone)

	var clientID string
	err := repository.database.QueryRow(
		ctx,
		`SELECT id::text
		 FROM clients
		 WHERE ($1 <> '' AND bitrix_contact_id = $1)
		    OR ($2 <> '' AND phone = $2)
		 ORDER BY CASE WHEN bitrix_contact_id = $1 THEN 0 ELSE 1 END
		 LIMIT 1`,
		contactID,
		phone,
	).Scan(&clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find client by Bitrix contact: %w", err)
	}

	if contactID != "" && clientID != "" {
		_, _ = repository.database.Exec(
			ctx,
			`UPDATE clients SET bitrix_contact_id = $2 WHERE id = $1::uuid AND bitrix_contact_id IS NULL`,
			clientID,
			contactID,
		)
	}

	return clientID, nil
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
			rr.order_number,
			c.id::text,
			COALESCE(c.full_name, ''),
			c.phone,
			COALESCE(s.client_type, c.client_type),
			COALESCE(s.company_name, ''),
			COALESCE(s.inn, ''),
			COALESCE(s.company_contact, ''),
			COALESCE(c.bitrix_contact_id, ''),
			COALESCE(rr.bitrix_deal_id, ''),
			t.name,
			rr.rental_days,
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
		 LEFT JOIN rental_customer_snapshots AS s ON s.rental_request_id = rr.id
		 JOIN tools AS t ON t.id = rr.tool_id
		 WHERE rr.id = $1::uuid`,
		rentalID,
	).Scan(
		&data.RentalID,
		&data.OrderNumber,
		&data.ClientID,
		&data.ClientFullName,
		&data.ClientPhone,
		&data.ClientType,
		&data.OrganizationName,
		&data.INN,
		&data.ContactFullName,
		&data.ContactID,
		&data.DealID,
		&data.ToolName,
		&data.RentalDays,
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

func (repository *PostgresRepository) CreateRentalFromBitrix(
	ctx context.Context,
	deal DealFull,
	contact ContactDetails,
	targetStatus string,
) (string, error) {
	if deal.ID == "" {
		return "", errors.New("empty Bitrix deal ID")
	}

	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin rental import from Bitrix: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var existingID string
	err = transaction.QueryRow(
		ctx,
		`SELECT id::text FROM rental_requests WHERE bitrix_deal_id = $1`,
		deal.ID,
	).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}

	phone := normalizePhone(contact.Phone)
	if phone == "+79990000000" && deal.ClientPhone != "" {
		phone = normalizePhone(deal.ClientPhone)
	}

	fullName := strings.TrimSpace(contact.FullName)
	if fullName == "" {
		fullName = strings.TrimSpace(deal.ClientName)
	}
	if fullName == "" {
		fullName = strings.TrimSpace(deal.Title)
	}
	if fullName == "" {
		fullName = "Клиент Битрикс24"
	}

	var clientID string
	err = transaction.QueryRow(
		ctx,
		`SELECT id::text FROM clients WHERE phone = $1`,
		phone,
	).Scan(&clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.QueryRow(
			ctx,
			`INSERT INTO clients (phone, full_name, bitrix_contact_id, status)
			 VALUES ($1, $2, NULLIF($3, ''), 'verified')
			 RETURNING id::text`,
			phone,
			fullName,
			deal.ContactID,
		).Scan(&clientID)
		if err != nil {
			return "", fmt.Errorf("create client for Bitrix deal: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("lookup client for Bitrix deal: %w", err)
	} else if deal.ContactID != "" {
		_, _ = transaction.Exec(
			ctx,
			`UPDATE clients
			 SET bitrix_contact_id = $2
			 WHERE id = $1::uuid AND (bitrix_contact_id IS NULL OR bitrix_contact_id = '')`,
			clientID,
			deal.ContactID,
		)
	}

	searchTool := strings.TrimSpace(deal.ToolName)
	if searchTool == "" {
		searchTool = strings.TrimSpace(deal.Title)
	}

	var toolID string
	var defaultDailyPrice, defaultDeposit int64
	err = transaction.QueryRow(
		ctx,
		`SELECT id::text, daily_price, deposit_amount
		 FROM tools
		 WHERE $1 <> '' AND (name ILIKE '%' || $1 || '%' OR $1 ILIKE '%' || name || '%')
		 ORDER BY id
		 LIMIT 1`,
		searchTool,
	).Scan(&toolID, &defaultDailyPrice, &defaultDeposit)
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.QueryRow(
			ctx,
			`SELECT id::text, daily_price, deposit_amount
			 FROM tools
			 ORDER BY id
			 LIMIT 1`,
		).Scan(&toolID, &defaultDailyPrice, &defaultDeposit)
	}
	if err != nil {
		return "", fmt.Errorf("find tool for Bitrix deal: %w", err)
	}

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	startDate := parseDateOrDefault(deal.BeginDate, today)
	endDate := parseDateOrDefault(deal.CloseDate, startDate.AddDate(0, 0, 1))
	if endDate.Before(startDate) {
		endDate = startDate.AddDate(0, 0, 1)
	}
	rentalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	if rentalDays < 1 {
		rentalDays = 1
	}
	endDate = startDate.AddDate(0, 0, rentalDays-1)

	var toolUnitID string
	err = transaction.QueryRow(
		ctx,
		`SELECT tu.id::text
		 FROM tool_units AS tu
		 WHERE tu.tool_id = $1::uuid
		   AND tu.status = 'available'
		   AND NOT EXISTS (
			SELECT 1 FROM rental_requests AS rr
			WHERE rr.tool_unit_id = tu.id
			  AND rr.status IN (
				'pending_manager', 'awaiting_payment', 'paid', 'preparing',
				'ready', 'handed_to_courier', 'rented', 'awaiting_return', 'inspection'
			  )
			  AND rr.rental_period && daterange($2::date, $3::date, '[]')
		   )
		 ORDER BY tu.inventory_number
		 LIMIT 1`,
		toolID,
		startDate,
		endDate,
	).Scan(&toolUnitID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.QueryRow(
			ctx,
			`SELECT tu.id::text
			 FROM tool_units AS tu
			 WHERE tu.tool_id = $1::uuid
			 ORDER BY tu.inventory_number
			 LIMIT 1`,
			toolID,
		).Scan(&toolUnitID)
	}
	if err != nil {
		return "", fmt.Errorf("select tool unit for Bitrix deal: %w", err)
	}

	depositAmount := parseKopecks(deal.Deposit)
	if depositAmount <= 0 {
		depositAmount = defaultDeposit
	}
	rentalPrice := defaultDailyPrice * int64(rentalDays)
	deliveryCost := int64(0)
	totalAmount := rentalPrice + depositAmount + deliveryCost

	deliveryMethod := "self_pickup"
	var deliveryAddress *string
	if strings.TrimSpace(deal.Address) != "" {
		deliveryMethod = "courier"
		trimmedAddr := strings.TrimSpace(deal.Address)
		deliveryAddress = &trimmedAddr
	}

	status := targetStatus
	if !isValidRentalStatus(status) {
		status = "pending_manager"
	}

	var rentalID, orderNumber string
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO rental_requests (
			client_id,
			tool_id,
			tool_unit_id,
			start_date,
			end_date,
			rental_days,
			rental_price,
			deposit_amount,
			delivery_cost,
			total_amount,
			delivery_method,
			delivery_address,
			status,
			expires_at,
			bitrix_deal_id,
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, NOW(), NOW()
		)
		RETURNING id::text, order_number`,
		clientID,
		toolID,
		toolUnitID,
		startDate,
		endDate,
		rentalDays,
		rentalPrice,
		depositAmount,
		deliveryCost,
		totalAmount,
		deliveryMethod,
		deliveryAddress,
		status,
		now.Add(24*time.Hour),
		deal.ID,
	).Scan(&rentalID, &orderNumber)
	if err != nil {
		return "", fmt.Errorf("insert imported rental from Bitrix: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO rental_customer_snapshots (
			rental_request_id, order_number, client_type, full_name, created_at
		)
		VALUES ($1::uuid, $2, 'individual', $3, NOW())
		ON CONFLICT (rental_request_id) DO NOTHING`,
		rentalID,
		orderNumber,
		fullName,
	); err != nil {
		return "", fmt.Errorf("insert rental customer snapshot: %w", err)
	}

	holdStatus := "active"
	if status == "paid" || status == "rented" || status == "ready" {
		holdStatus = "confirmed"
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tool_holds (
			rental_request_id, tool_unit_id, start_date, end_date, status, expires_at, created_at, updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (rental_request_id) DO NOTHING`,
		rentalID,
		toolUnitID,
		startDate,
		endDate,
		holdStatus,
		now.Add(24*time.Hour),
	); err != nil {
		return "", fmt.Errorf("insert tool hold: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO rental_status_history (
			rental_request_id, from_status, to_status, actor_type, actor_id, created_at
		)
		VALUES ($1::uuid, NULL, $2, 'integration', $3, NOW())`,
		rentalID,
		status,
		deal.ID,
	); err != nil {
		return "", fmt.Errorf("insert rental status history: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit imported Bitrix rental: %w", err)
	}
	return rentalID, nil
}

func normalizePhone(phone string) string {
	digits := ""
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits += string(r)
		}
	}
	if len(digits) == 11 && (digits[0] == '7' || digits[0] == '8') {
		return "+7" + digits[1:]
	}
	if len(digits) == 10 {
		return "+7" + digits
	}
	if len(digits) >= 10 {
		return "+" + digits
	}
	return "+79990000000"
}

func parseKopecks(val string) int64 {
	val = strings.TrimSpace(val)
	if idx := strings.Index(val, "|"); idx != -1 {
		val = val[:idx]
	}
	val = strings.ReplaceAll(val, " ", "")
	val = strings.ReplaceAll(val, ",", ".")
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0
	}
	return int64(f * 100)
}

func parseDateOrDefault(str string, fallback time.Time) time.Time {
	str = strings.TrimSpace(str)
	if str == "" {
		return fallback
	}
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t.Truncate(24 * time.Hour)
	}
	if t, err := time.Parse("2006-01-02", str); err == nil {
		return t
	}
	return fallback
}

func isValidRentalStatus(status string) bool {
	switch status {
	case "pending_manager", "awaiting_payment", "paid", "preparing",
		"ready", "handed_to_courier", "rented", "awaiting_return",
		"inspection", "completed", "rejected", "cancelled", "payment_expired":
		return true
	default:
		return false
	}
}

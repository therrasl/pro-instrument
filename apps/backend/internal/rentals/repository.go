package rentals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) GetAvailableToolPricing(
	ctx context.Context,
	toolID string,
	startDate time.Time,
	endDate time.Time,
	now time.Time,
) (ToolPricing, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return ToolPricing{}, fmt.Errorf("begin rental quote: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	if _, err := expireHoldsTx(ctx, transaction, now); err != nil {
		return ToolPricing{}, err
	}

	pricing, err := getToolPricing(ctx, transaction, toolID)
	if err != nil {
		return ToolPricing{}, err
	}

	var available bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM tool_units AS tu
			WHERE
				tu.tool_id = $1::uuid
				AND tu.status = 'available'
				AND NOT EXISTS (
					SELECT 1
					FROM tool_holds AS h
					WHERE
						h.tool_unit_id = tu.id
						AND h.status IN ('active', 'confirmed')
						AND h.expires_at > $4
						AND h.rental_period && daterange($2::date, $3::date, '[]')
				)
				AND NOT EXISTS (
					SELECT 1
					FROM rental_requests AS rr
					WHERE
						rr.tool_unit_id = tu.id
						AND rr.status IN (
							'pending_manager',
							'awaiting_payment',
							'paid',
							'preparing',
							'ready',
							'handed_to_courier',
							'rented',
							'awaiting_return',
							'inspection'
						)
						AND rr.rental_period && daterange($2::date, $3::date, '[]')
				)
		)`,
		toolID,
		startDate,
		endDate,
		now,
	).Scan(&available); err != nil {
		return ToolPricing{}, fmt.Errorf("check rental quote availability: %w", err)
	}
	if !available {
		return ToolPricing{}, ErrNoAvailableUnit
	}

	if err := transaction.Commit(ctx); err != nil {
		return ToolPricing{}, fmt.Errorf("commit rental quote: %w", err)
	}
	return pricing, nil
}

func (repository *PostgresRepository) Create(
	ctx context.Context,
	command CreateCommand,
) (RentalRequest, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return RentalRequest{}, fmt.Errorf("begin rental request: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	if _, err := expireHoldsTx(ctx, transaction, command.Now); err != nil {
		return RentalRequest{}, err
	}

	pricing, err := getToolPricing(ctx, transaction, command.ToolID)
	if err != nil {
		return RentalRequest{}, err
	}

	var toolUnitID string
	err = transaction.QueryRow(
		ctx,
		`SELECT tu.id::text
		 FROM tool_units AS tu
		 WHERE
			tu.tool_id = $1::uuid
			AND tu.status = 'available'
			AND NOT EXISTS (
				SELECT 1
				FROM tool_holds AS h
				WHERE
					h.tool_unit_id = tu.id
					AND h.status IN ('active', 'confirmed')
					AND h.expires_at > $4
					AND h.rental_period && daterange($2::date, $3::date, '[]')
			)
			AND NOT EXISTS (
				SELECT 1
				FROM rental_requests AS rr
				WHERE
					rr.tool_unit_id = tu.id
					AND rr.status IN (
						'pending_manager',
						'awaiting_payment',
						'paid',
						'preparing',
						'ready',
						'handed_to_courier',
						'rented',
						'awaiting_return',
						'inspection'
					)
					AND rr.rental_period && daterange($2::date, $3::date, '[]')
			)
		 ORDER BY tu.inventory_number, tu.id
		 FOR UPDATE OF tu SKIP LOCKED
		 LIMIT 1`,
		command.ToolID,
		command.StartDate,
		command.EndDate,
		command.Now,
	).Scan(&toolUnitID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalRequest{}, ErrNoAvailableUnit
	}
	if err != nil {
		return RentalRequest{}, fmt.Errorf("lock available tool unit: %w", err)
	}

	input := QuoteRequest{
		ToolID:          command.ToolID,
		StartDate:       command.StartDate,
		EndDate:         command.EndDate,
		DeliveryMethod:  command.DeliveryMethod,
		DeliveryAddress: stringValue(command.DeliveryAddress),
	}
	quote, err := calculateQuote(input, command.RentalDays, pricing, command.CourierFee)
	if err != nil {
		return RentalRequest{}, err
	}

	rental, err := scanRental(transaction.QueryRow(
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
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, 'pending_manager', $13, $14, $14
		)
		RETURNING
			id::text,
			client_id::text,
			tool_id::text,
			tool_unit_id::text,
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
			updated_at`,
		command.ClientID,
		command.ToolID,
		toolUnitID,
		command.StartDate,
		command.EndDate,
		command.RentalDays,
		quote.RentalPrice,
		quote.DepositAmount,
		quote.DeliveryCost,
		quote.TotalAmount,
		command.DeliveryMethod,
		command.DeliveryAddress,
		command.ExpiresAt,
		command.Now,
	))
	if isExclusionViolation(err) {
		return RentalRequest{}, ErrNoAvailableUnit
	}
	if err != nil {
		return RentalRequest{}, fmt.Errorf("insert rental request: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tool_holds (
			rental_request_id,
			tool_unit_id,
			start_date,
			end_date,
			status,
			expires_at,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, 'active', $5, $6, $6)`,
		rental.ID,
		rental.ToolUnitID,
		command.StartDate,
		command.EndDate,
		command.ExpiresAt,
		command.Now,
	); err != nil {
		if isExclusionViolation(err) {
			return RentalRequest{}, ErrNoAvailableUnit
		}
		return RentalRequest{}, fmt.Errorf("insert tool hold: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO rental_status_history (
			rental_request_id,
			from_status,
			to_status,
			actor_type,
			actor_id,
			created_at
		)
		VALUES ($1::uuid, NULL, 'pending_manager', 'client', $2, $3)`,
		rental.ID,
		command.ClientID,
		command.Now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("insert rental status history: %w", err)
	}

	contactPayload, err := json.Marshal(map[string]string{"client_id": rental.ClientID})
	if err != nil {
		return RentalRequest{}, fmt.Errorf("encode contact outbox payload: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO integration_outbox (
			event_type,
			aggregate_type,
			aggregate_id,
			dedupe_key,
			payload,
			status,
			next_attempt_at,
			created_at
		)
		VALUES (
			'bitrix.contact.upsert',
			'client',
			$1::uuid,
			$2,
			$3::jsonb,
			'pending',
			$4,
			$4
		)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		rental.ClientID,
		"bitrix.contact.upsert:"+rental.ClientID,
		contactPayload,
		command.Now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("insert contact outbox event: %w", err)
	}

	payload, err := json.Marshal(rental)
	if err != nil {
		return RentalRequest{}, fmt.Errorf("encode rental outbox payload: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO integration_outbox (
			event_type,
			aggregate_type,
			aggregate_id,
			dedupe_key,
			payload,
			status,
			next_attempt_at,
			created_at
		)
		VALUES (
			'bitrix.deal.create',
			'rental_request',
			$1::uuid,
			$2,
			$3::jsonb,
			'pending',
			$4,
			$4
		)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		rental.ID,
		"bitrix.deal.create:"+rental.ID,
		payload,
		command.Now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("insert rental outbox event: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		if isExclusionViolation(err) {
			return RentalRequest{}, ErrNoAvailableUnit
		}
		return RentalRequest{}, fmt.Errorf("commit rental request: %w", err)
	}
	return rental, nil
}

func (repository *PostgresRepository) ListByClient(
	ctx context.Context,
	clientID string,
	limit int,
	offset int,
) ([]RentalRequest, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			tool_id::text,
			tool_unit_id::text,
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
		 FROM rental_requests
		 WHERE client_id = $1::uuid
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2 OFFSET $3`,
		clientID,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list client rental requests: %w", err)
	}
	defer rows.Close()

	rentals := make([]RentalRequest, 0)
	for rows.Next() {
		rental, err := scanRental(rows)
		if err != nil {
			return nil, err
		}
		rentals = append(rentals, rental)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate client rental requests: %w", err)
	}
	return rentals, nil
}

func (repository *PostgresRepository) GetByClient(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	rental, err := scanRental(repository.database.QueryRow(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			tool_id::text,
			tool_unit_id::text,
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
		 FROM rental_requests
		 WHERE id = $1::uuid AND client_id = $2::uuid`,
		rentalID,
		clientID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalRequest{}, ErrRentalNotFound
	}
	if err != nil {
		return RentalRequest{}, err
	}
	return rental, nil
}

func (repository *PostgresRepository) CancelByClient(
	ctx context.Context,
	clientID string,
	rentalID string,
	now time.Time,
) (RentalRequest, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return RentalRequest{}, fmt.Errorf("begin rental cancellation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	if _, err := expireHoldsTx(ctx, transaction, now); err != nil {
		return RentalRequest{}, err
	}

	var currentStatus string
	err = transaction.QueryRow(
		ctx,
		`SELECT status
		 FROM rental_requests
		 WHERE id = $1::uuid AND client_id = $2::uuid
		 FOR UPDATE`,
		rentalID,
		clientID,
	).Scan(&currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalRequest{}, ErrRentalNotFound
	}
	if err != nil {
		return RentalRequest{}, fmt.Errorf("lock rental cancellation: %w", err)
	}

	if currentStatus == StatusCancelled {
		rental, err := getRentalByClientTx(ctx, transaction, clientID, rentalID)
		if err != nil {
			return RentalRequest{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return RentalRequest{}, fmt.Errorf("commit idempotent rental cancellation: %w", err)
		}
		return rental, nil
	}
	if currentStatus != StatusPendingManager && currentStatus != StatusAwaitingPayment {
		return RentalRequest{}, ErrRentalNotCancellable
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE rental_requests
		 SET status = 'cancelled', updated_at = $3
		 WHERE id = $1::uuid AND client_id = $2::uuid`,
		rentalID,
		clientID,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("cancel rental request: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tool_holds
		 SET status = 'cancelled', updated_at = $2
		 WHERE rental_request_id = $1::uuid
		   AND status IN ('active', 'confirmed')`,
		rentalID,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("release cancelled rental hold: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO payment_audit_logs (
			payment_id,
			rental_request_id,
			action,
			actor_type,
			actor_id,
			details,
			created_at
		)
		SELECT
			p.id,
			p.rental_request_id,
			'payment.invalidated_by_rental_cancel',
			'client',
			$2,
			jsonb_build_object('previous_status', p.status),
			$3
		FROM payments AS p
		WHERE p.rental_request_id = $1::uuid
		  AND p.status IN ('creating', 'pending', 'requires_review')`,
		rentalID,
		clientID,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("audit cancelled rental payment: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE payments
		 SET
			status = 'canceled',
			confirmation_url = NULL,
			updated_at = $2,
			cancelled_at = $2
		 WHERE rental_request_id = $1::uuid
		   AND status IN ('creating', 'pending', 'requires_review')`,
		rentalID,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("invalidate cancelled rental payment: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO rental_status_history (
			rental_request_id,
			from_status,
			to_status,
			actor_type,
			actor_id,
			created_at
		)
		VALUES ($1::uuid, $2, 'cancelled', 'client', $3, $4)`,
		rentalID,
		currentStatus,
		clientID,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("record rental cancellation: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"rental_id": rentalID,
		"status":    StatusCancelled,
	})
	if err != nil {
		return RentalRequest{}, fmt.Errorf("encode cancelled Bitrix outbox: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO integration_outbox (
			event_type,
			aggregate_type,
			aggregate_id,
			dedupe_key,
			payload,
			status,
			next_attempt_at,
			created_at
		)
		VALUES (
			'bitrix.deal.update',
			'rental_request',
			$1::uuid,
			$2,
			$3::jsonb,
			'pending',
			$4,
			$4
		)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		rentalID,
		"bitrix.deal.update:"+rentalID+":"+StatusCancelled,
		payload,
		now,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("insert cancelled Bitrix outbox: %w", err)
	}

	rental, err := getRentalByClientTx(ctx, transaction, clientID, rentalID)
	if err != nil {
		return RentalRequest{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RentalRequest{}, fmt.Errorf("commit rental cancellation: %w", err)
	}
	return rental, nil
}

func (repository *PostgresRepository) ExpireHolds(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire tool holds: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	count, err := expireHoldsTx(ctx, transaction, now)
	if err != nil {
		return 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expired tool holds: %w", err)
	}
	return count, nil
}

func getToolPricing(
	ctx context.Context,
	transaction pgx.Tx,
	toolID string,
) (ToolPricing, error) {
	var pricing ToolPricing
	err := transaction.QueryRow(
		ctx,
		`SELECT daily_price::bigint, deposit_amount::bigint
		 FROM tools
		 WHERE id = $1::uuid AND is_active = TRUE`,
		toolID,
	).Scan(&pricing.DailyPrice, &pricing.DepositAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolPricing{}, ErrToolNotFound
	}
	if err != nil {
		return ToolPricing{}, fmt.Errorf("get rental tool pricing: %w", err)
	}
	return pricing, nil
}

func expireHoldsTx(
	ctx context.Context,
	transaction pgx.Tx,
	now time.Time,
) (int64, error) {
	var count int64
	err := transaction.QueryRow(
		ctx,
		`WITH candidates AS MATERIALIZED (
			SELECT
				h.id AS hold_id,
				rr.id AS request_id,
				rr.status AS old_status
			FROM tool_holds AS h
			JOIN rental_requests AS rr ON rr.id = h.rental_request_id
			WHERE
				(
					(h.status = 'active' AND rr.status = 'pending_manager')
					OR (h.status = 'confirmed' AND rr.status = 'awaiting_payment')
				)
				AND h.expires_at <= $1
			ORDER BY h.id
			FOR UPDATE OF h, rr
		),
		updated_holds AS (
			UPDATE tool_holds AS h
			SET status = 'expired', updated_at = $1
			FROM candidates AS c
			WHERE h.id = c.hold_id
			RETURNING h.id
		),
		updated_requests AS (
			UPDATE rental_requests AS rr
			SET status = 'payment_expired', updated_at = $1
			FROM candidates AS c
			WHERE
				rr.id = c.request_id
				AND c.old_status IN ('pending_manager', 'awaiting_payment')
				AND rr.expires_at <= $1
			RETURNING rr.id
		),
		updated_payments AS (
			UPDATE payments AS p
			SET status = 'expired', confirmation_url = NULL, updated_at = $1, cancelled_at = $1
			FROM updated_requests AS u
			WHERE
				p.rental_request_id = u.id
				AND p.status IN ('creating', 'pending', 'requires_review')
			RETURNING p.id
		),
		inserted_history AS (
			INSERT INTO rental_status_history (
				rental_request_id,
				from_status,
				to_status,
				actor_type,
				actor_id,
				created_at
			)
			SELECT
				c.request_id,
				c.old_status,
				'payment_expired',
				'system',
				NULL,
				$1
			FROM candidates AS c
			JOIN updated_requests AS u ON u.id = c.request_id
			RETURNING id
		)
		SELECT COUNT(*)::bigint FROM updated_holds`,
		now,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("expire tool holds: %w", err)
	}
	return count, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRental(row rowScanner) (RentalRequest, error) {
	var rental RentalRequest
	var startDate time.Time
	var endDate time.Time
	var deliveryAddress pgtype.Text
	var bitrixDealID pgtype.Text
	if err := row.Scan(
		&rental.ID,
		&rental.ClientID,
		&rental.ToolID,
		&rental.ToolUnitID,
		&startDate,
		&endDate,
		&rental.RentalDays,
		&rental.RentalPrice,
		&rental.DepositAmount,
		&rental.DeliveryCost,
		&rental.TotalAmount,
		&rental.DeliveryMethod,
		&deliveryAddress,
		&rental.Status,
		&rental.ExpiresAt,
		&bitrixDealID,
		&rental.CreatedAt,
		&rental.UpdatedAt,
	); err != nil {
		return RentalRequest{}, err
	}
	rental.StartDate = startDate.Format(time.DateOnly)
	rental.EndDate = endDate.Format(time.DateOnly)
	rental.ExpiresAt = rental.ExpiresAt.UTC()
	rental.CreatedAt = rental.CreatedAt.UTC()
	rental.UpdatedAt = rental.UpdatedAt.UTC()
	rental.PaymentAvailable = rental.Status == StatusAwaitingPayment &&
		rental.ExpiresAt.After(time.Now().UTC())
	if rental.Status == StatusAwaitingPayment || rental.Status == StatusPaymentExpired {
		paymentExpiresAt := rental.ExpiresAt
		rental.PaymentExpiresAt = &paymentExpiresAt
	}
	if deliveryAddress.Valid {
		rental.DeliveryAddress = &deliveryAddress.String
	}
	if bitrixDealID.Valid {
		rental.BitrixDealID = &bitrixDealID.String
	}
	return rental, nil
}

func getRentalByClientTx(
	ctx context.Context,
	transaction pgx.Tx,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	rental, err := scanRental(transaction.QueryRow(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			tool_id::text,
			tool_unit_id::text,
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
		 FROM rental_requests
		 WHERE id = $1::uuid AND client_id = $2::uuid`,
		rentalID,
		clientID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalRequest{}, ErrRentalNotFound
	}
	if err != nil {
		return RentalRequest{}, fmt.Errorf("get cancelled rental request: %w", err)
	}
	return rental, nil
}

func isExclusionViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23P01"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

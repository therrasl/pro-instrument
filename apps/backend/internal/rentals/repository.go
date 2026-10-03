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

	var customerReady bool
	if err := transaction.QueryRow(ctx, `SELECT c.client_type = 'individual' OR o.client_id IS NOT NULL
		FROM clients AS c LEFT JOIN client_organizations AS o ON o.client_id=c.id
		WHERE c.id=$1::uuid`, command.ClientID).Scan(&customerReady); err != nil {
		return RentalRequest{}, fmt.Errorf("check rental customer: %w", err)
	}
	if !customerReady {
		return RentalRequest{}, ErrOnboardingIncomplete
	}

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
			order_number,
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
		`INSERT INTO rental_customer_snapshots (
			rental_request_id, order_number, client_type, full_name, email,
			company_name, inn, kpp, ogrn, legal_address, company_contact,
			actual_address, settlement_account, bik, correspondent_account,
			bank_name, organization_phone, contact_position, created_at
		)
		SELECT
			$1::uuid, $2, c.client_type, c.full_name, c.email,
			COALESCE(o.company_name, c.company_name), COALESCE(o.inn, c.inn),
			COALESCE(o.kpp, c.kpp), COALESCE(o.ogrn, c.ogrn),
			COALESCE(o.legal_address, c.legal_address),
			COALESCE(o.contact_full_name, c.company_contact),
			o.actual_address, o.settlement_account, o.bik, o.correspondent_account,
			o.bank_name, o.phone, o.contact_position, $3
		FROM clients AS c
		LEFT JOIN client_organizations AS o ON o.client_id = c.id
		WHERE c.id = $4::uuid`,
		rental.ID,
		rental.OrderNumber,
		command.Now,
		command.ClientID,
	); err != nil {
		return RentalRequest{}, fmt.Errorf("snapshot rental customer: %w", err)
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
			order_number,
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
			order_number,
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

func (repository *PostgresRepository) ListDocumentsByClient(
	ctx context.Context,
	clientID string,
	rentalID string,
) ([]OrderDocument, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT d.id::text, d.order_number, d.document_type, d.title,
		        d.storage_key, d.mime_type, d.created_at
		 FROM order_documents AS d
		 JOIN rental_requests AS rr ON rr.id = d.rental_request_id
		 WHERE rr.id = $1::uuid AND rr.client_id = $2::uuid AND d.storage_key IS NOT NULL
		 ORDER BY d.created_at, d.id`,
		rentalID,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list order documents: %w", err)
	}
	defer rows.Close()
	documents := make([]OrderDocument, 0)
	for rows.Next() {
		var document OrderDocument
		var mimeType pgtype.Text
		if err := rows.Scan(
			&document.ID, &document.OrderNumber, &document.DocumentType,
			&document.Title, &document.StorageKey, &mimeType, &document.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan order document: %w", err)
		}
		if mimeType.Valid {
			document.MIMEType = &mimeType.String
		}
		document.CreatedAt = document.CreatedAt.UTC()
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order documents: %w", err)
	}
	return documents, nil
}

func (repository *PostgresRepository) GetDocumentByClient(ctx context.Context, clientID, rentalID, documentID string) (OrderDocument, error) {
	var document OrderDocument
	var mimeType pgtype.Text
	err := repository.database.QueryRow(ctx, `SELECT d.id::text, d.order_number, d.document_type, d.title,
		d.storage_key, d.mime_type, d.created_at
		FROM order_documents AS d
		JOIN rental_requests AS rr ON rr.id=d.rental_request_id
		WHERE rr.id=$1::uuid AND rr.client_id=$2::uuid AND d.id=$3::uuid`, rentalID, clientID, documentID).Scan(
		&document.ID, &document.OrderNumber, &document.DocumentType, &document.Title,
		&document.StorageKey, &mimeType, &document.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderDocument{}, ErrDocumentNotFound
	}
	if err != nil {
		return OrderDocument{}, fmt.Errorf("get order document: %w", err)
	}
	if mimeType.Valid {
		document.MIMEType = &mimeType.String
	}
	document.CreatedAt = document.CreatedAt.UTC()
	return document, nil
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
		&rental.OrderNumber,
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
			order_number,
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

func (repository *PostgresRepository) GetActiveRentalForExtension(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	rental, err := scanRental(repository.database.QueryRow(
		ctx,
		`SELECT
			id::text,
			COALESCE(order_number, ''),
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
		return RentalRequest{}, fmt.Errorf("get active rental for extension: %w", err)
	}
	switch rental.Status {
	case StatusRented, StatusReady, StatusHandedToCourier, StatusAwaitingReturn:
		return rental, nil
	default:
		return RentalRequest{}, ErrRentalNotActive
	}
}

func (repository *PostgresRepository) CheckExtensionAvailability(
	ctx context.Context,
	toolUnitID string,
	currentEndDate time.Time,
	newEndDate time.Time,
	currentRentalID string,
	now time.Time,
) (bool, error) {
	var available bool
	err := repository.database.QueryRow(
		ctx,
		`SELECT NOT EXISTS (
			SELECT 1
			FROM tool_holds AS h
			WHERE
				h.tool_unit_id = $1::uuid
				AND h.rental_request_id != $2::uuid
				AND h.status IN ('active', 'confirmed')
				AND h.expires_at > $5
				AND h.rental_period && daterange(($3::date + INTERVAL '1 day')::date, $4::date, '[]')
		) AND NOT EXISTS (
			SELECT 1
			FROM rental_requests AS other
			WHERE
				other.tool_unit_id = $1::uuid
				AND other.id != $2::uuid
				AND other.status IN (
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
				AND other.rental_period && daterange(($3::date + INTERVAL '1 day')::date, $4::date, '[]')
		)`,
		toolUnitID,
		currentRentalID,
		currentEndDate,
		newEndDate,
		now,
	).Scan(&available)
	if err != nil {
		return false, fmt.Errorf("check extension availability: %w", err)
	}
	return available, nil
}

func (repository *PostgresRepository) CreateExtension(
	ctx context.Context,
	rentalID string,
	previousEndDate time.Time,
	newEndDate time.Time,
	additionalDays int,
	dailyPrice int64,
	amount int64,
	now time.Time,
) (RentalExtension, error) {
	var ext RentalExtension
	var prevDate, nDate time.Time
	var paidAt *time.Time
	err := repository.database.QueryRow(
		ctx,
		`INSERT INTO rental_extensions (
			rental_request_id,
			previous_end_date,
			new_end_date,
			additional_days,
			daily_price,
			amount,
			status,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::date, $3::date, $4, $5, $6, 'pending_payment', $7, $7)
		RETURNING
			id::text,
			rental_request_id::text,
			previous_end_date,
			new_end_date,
			additional_days,
			daily_price,
			amount,
			status,
			payment_id::text,
			created_at,
			updated_at,
			paid_at`,
		rentalID,
		previousEndDate,
		newEndDate,
		additionalDays,
		dailyPrice,
		amount,
		now,
	).Scan(
		&ext.ID,
		&ext.RentalRequestID,
		&prevDate,
		&nDate,
		&ext.AdditionalDays,
		&ext.DailyPrice,
		&ext.Amount,
		&ext.Status,
		&ext.PaymentID,
		&ext.CreatedAt,
		&ext.UpdatedAt,
		&paidAt,
	)
	if err != nil {
		return RentalExtension{}, fmt.Errorf("create rental extension: %w", err)
	}
	ext.PreviousEndDate = prevDate.Format(time.DateOnly)
	ext.NewEndDate = nDate.Format(time.DateOnly)
	ext.PaidAt = paidAt
	return ext, nil
}

func (repository *PostgresRepository) GetExtension(
	ctx context.Context,
	extensionID string,
) (RentalExtension, error) {
	var ext RentalExtension
	var prevDate, nDate time.Time
	var paidAt *time.Time
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			e.id::text,
			e.rental_request_id::text,
			e.previous_end_date,
			e.new_end_date,
			e.additional_days,
			e.daily_price,
			e.amount,
			e.status,
			e.payment_id::text,
			p.confirmation_url,
			e.created_at,
			e.updated_at,
			e.paid_at
		 FROM rental_extensions AS e
		 LEFT JOIN payments AS p ON p.id = e.payment_id
		 WHERE e.id = $1::uuid`,
		extensionID,
	).Scan(
		&ext.ID,
		&ext.RentalRequestID,
		&prevDate,
		&nDate,
		&ext.AdditionalDays,
		&ext.DailyPrice,
		&ext.Amount,
		&ext.Status,
		&ext.PaymentID,
		&ext.ConfirmationURL,
		&ext.CreatedAt,
		&ext.UpdatedAt,
		&paidAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalExtension{}, errors.New("rental extension not found")
	}
	if err != nil {
		return RentalExtension{}, fmt.Errorf("get rental extension: %w", err)
	}
	ext.PreviousEndDate = prevDate.Format(time.DateOnly)
	ext.NewEndDate = nDate.Format(time.DateOnly)
	ext.PaidAt = paidAt
	return ext, nil
}

func (repository *PostgresRepository) ListExtensionsByRental(
	ctx context.Context,
	clientID string,
	rentalID string,
) ([]RentalExtension, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT
			e.id::text,
			e.rental_request_id::text,
			e.previous_end_date,
			e.new_end_date,
			e.additional_days,
			e.daily_price,
			e.amount,
			e.status,
			e.payment_id::text,
			p.confirmation_url,
			e.created_at,
			e.updated_at,
			e.paid_at
		 FROM rental_extensions AS e
		 JOIN rental_requests AS rr ON rr.id = e.rental_request_id
		 LEFT JOIN payments AS p ON p.id = e.payment_id
		 WHERE e.rental_request_id = $1::uuid AND rr.client_id = $2::uuid
		 ORDER BY e.created_at DESC`,
		rentalID,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rental extensions: %w", err)
	}
	defer rows.Close()

	var result []RentalExtension
	for rows.Next() {
		var ext RentalExtension
		var prevDate, nDate time.Time
		var paidAt *time.Time
		if err := rows.Scan(
			&ext.ID,
			&ext.RentalRequestID,
			&prevDate,
			&nDate,
			&ext.AdditionalDays,
			&ext.DailyPrice,
			&ext.Amount,
			&ext.Status,
			&ext.PaymentID,
			&ext.ConfirmationURL,
			&ext.CreatedAt,
			&ext.UpdatedAt,
			&paidAt,
		); err != nil {
			return nil, fmt.Errorf("scan rental extension: %w", err)
		}
		ext.PreviousEndDate = prevDate.Format(time.DateOnly)
		ext.NewEndDate = nDate.Format(time.DateOnly)
		ext.PaidAt = paidAt
		result = append(result, ext)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rental extensions: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) UpdateExtensionPaymentID(
	ctx context.Context,
	extensionID string,
	paymentID string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE rental_extensions
		 SET payment_id = $2::uuid, updated_at = NOW()
		 WHERE id = $1::uuid`,
		extensionID,
		paymentID,
	)
	return err
}

func (repository *PostgresRepository) SaveInspectionPhoto(
	ctx context.Context,
	photo InspectionPhoto,
) (InspectionPhoto, error) {
	var saved InspectionPhoto
	err := repository.database.QueryRow(
		ctx,
		`INSERT INTO rental_inspection_photos (
			rental_request_id,
			phase,
			photo_type,
			storage_key,
			file_name,
			mime_type,
			file_size,
			comment
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (rental_request_id, phase, photo_type)
		DO UPDATE SET
			storage_key = EXCLUDED.storage_key,
			file_name = EXCLUDED.file_name,
			mime_type = EXCLUDED.mime_type,
			file_size = EXCLUDED.file_size,
			comment = EXCLUDED.comment,
			created_at = NOW()
		RETURNING
			id::text,
			rental_request_id::text,
			phase,
			photo_type,
			storage_key,
			file_name,
			mime_type,
			file_size,
			COALESCE(comment, ''),
			created_at`,
		photo.RentalRequestID,
		photo.Phase,
		photo.PhotoType,
		photo.StorageKey,
		photo.FileName,
		photo.MIMEType,
		photo.FileSize,
		photo.Comment,
	).Scan(
		&saved.ID,
		&saved.RentalRequestID,
		&saved.Phase,
		&saved.PhotoType,
		&saved.StorageKey,
		&saved.FileName,
		&saved.MIMEType,
		&saved.FileSize,
		&saved.Comment,
		&saved.CreatedAt,
	)
	if err != nil {
		return InspectionPhoto{}, fmt.Errorf("save inspection photo: %w", err)
	}
	return saved, nil
}

func (repository *PostgresRepository) ListInspectionPhotos(
	ctx context.Context,
	clientID string,
	rentalID string,
) ([]InspectionPhoto, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT
			p.id::text,
			p.rental_request_id::text,
			p.phase,
			p.photo_type,
			p.storage_key,
			p.file_name,
			p.mime_type,
			p.file_size,
			COALESCE(p.comment, ''),
			p.created_at
		 FROM rental_inspection_photos AS p
		 JOIN rental_requests AS rr ON rr.id = p.rental_request_id
		 WHERE p.rental_request_id = $1::uuid AND rr.client_id = $2::uuid
		 ORDER BY p.created_at ASC`,
		rentalID,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list inspection photos: %w", err)
	}
	defer rows.Close()

	var result []InspectionPhoto
	for rows.Next() {
		var p InspectionPhoto
		if err := rows.Scan(
			&p.ID,
			&p.RentalRequestID,
			&p.Phase,
			&p.PhotoType,
			&p.StorageKey,
			&p.FileName,
			&p.MIMEType,
			&p.FileSize,
			&p.Comment,
			&p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inspection photo: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inspection photos: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) GetInspectionPhoto(
	ctx context.Context,
	clientID string,
	rentalID string,
	photoID string,
) (InspectionPhoto, error) {
	var p InspectionPhoto
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			p.id::text,
			p.rental_request_id::text,
			p.phase,
			p.photo_type,
			p.storage_key,
			p.file_name,
			p.mime_type,
			p.file_size,
			COALESCE(p.comment, ''),
			p.created_at
		 FROM rental_inspection_photos AS p
		 JOIN rental_requests AS rr ON rr.id = p.rental_request_id
		 WHERE p.id = $1::uuid AND p.rental_request_id = $2::uuid AND ($3 = '' OR rr.client_id = $3::uuid)`,
		photoID,
		rentalID,
		clientID,
	).Scan(
		&p.ID,
		&p.RentalRequestID,
		&p.Phase,
		&p.PhotoType,
		&p.StorageKey,
		&p.FileName,
		&p.MIMEType,
		&p.FileSize,
		&p.Comment,
		&p.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InspectionPhoto{}, ErrPhotoNotFound
	}
	if err != nil {
		return InspectionPhoto{}, fmt.Errorf("get inspection photo: %w", err)
	}
	return p, nil
}

func (repository *PostgresRepository) GetInspectionViewData(
	ctx context.Context,
	rentalID string,
) (InspectionViewData, error) {
	var data InspectionViewData
	var startDate, endDate time.Time
	var dealID *string

	err := repository.database.QueryRow(
		ctx,
		`SELECT
			rr.id::text,
			rr.order_number,
			rr.status,
			COALESCE(t.name, 'Инструмент'),
			COALESCE(tu.inventory_number, ''),
			COALESCE(c.full_name, 'Без имени'),
			c.phone,
			rr.start_date,
			rr.end_date,
			rr.rental_days,
			rr.rental_price,
			rr.deposit_amount,
			COALESCE(d.status, 'none'),
			COALESCE(d.refundable_amount, 0),
			COALESCE(d.refunded_amount, 0),
			COALESCE(d.withheld_amount, 0),
			rr.bitrix_deal_id
		 FROM rental_requests AS rr
		 JOIN clients AS c ON c.id = rr.client_id
		 LEFT JOIN tool_units AS tu ON tu.id = rr.tool_unit_id
		 LEFT JOIN tools AS t ON t.id = tu.tool_id
		 LEFT JOIN deposits AS d ON d.rental_request_id = rr.id
		 WHERE rr.id = $1::uuid`,
		rentalID,
	).Scan(
		&data.RentalID,
		&data.OrderNumber,
		&data.Status,
		&data.ToolName,
		&data.InventoryNumber,
		&data.ClientName,
		&data.ClientPhone,
		&startDate,
		&endDate,
		&data.RentalDays,
		&data.RentalPrice,
		&data.DepositAmount,
		&data.DepositStatus,
		&data.RefundableAmount,
		&data.RefundedAmount,
		&data.WithheldAmount,
		&dealID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InspectionViewData{}, ErrRentalNotFound
	}
	if err != nil {
		return InspectionViewData{}, fmt.Errorf("get inspection view data: %w", err)
	}

	data.StartDate = startDate.Format(time.DateOnly)
	data.EndDate = endDate.Format(time.DateOnly)
	if dealID != nil {
		data.BitrixDealID = *dealID
	}

	data.HandoverPhotos = make(map[string]InspectionPhoto)
	data.ReturnPhotos = make(map[string]InspectionPhoto)

	rows, err := repository.database.Query(
		ctx,
		`SELECT
			p.id::text,
			p.rental_request_id::text,
			p.phase,
			p.photo_type,
			p.storage_key,
			p.file_name,
			p.mime_type,
			p.file_size,
			COALESCE(p.comment, ''),
			p.created_at
		 FROM rental_inspection_photos AS p
		 WHERE p.rental_request_id = $1::uuid
		 ORDER BY p.created_at ASC`,
		rentalID,
	)
	if err != nil {
		return data, fmt.Errorf("list inspection photos for view: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p InspectionPhoto
		if err := rows.Scan(
			&p.ID,
			&p.RentalRequestID,
			&p.Phase,
			&p.PhotoType,
			&p.StorageKey,
			&p.FileName,
			&p.MIMEType,
			&p.FileSize,
			&p.Comment,
			&p.CreatedAt,
		); err != nil {
			return data, fmt.Errorf("scan photo for view: %w", err)
		}
		p.URL = fmt.Sprintf("/api/v1/rentals/%s/photos/%s", rentalID, p.ID)
		if p.Phase == PhotoPhaseHandover {
			data.HandoverPhotos[p.PhotoType] = p
		} else if p.Phase == PhotoPhaseReturn {
			data.ReturnPhotos[p.PhotoType] = p
		}
	}

	return data, rows.Err()
}

func (repository *PostgresRepository) UpdateRentalStatus(
	ctx context.Context,
	rentalID string,
	targetStatus string,
	actorType string,
	actorID string,
	now time.Time,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update rental status: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var currentStatus string
	err = transaction.QueryRow(
		ctx,
		`SELECT status FROM rental_requests WHERE id = $1::uuid FOR UPDATE`,
		rentalID,
	).Scan(&currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRentalNotFound
	}
	if err != nil {
		return fmt.Errorf("lock rental for status update: %w", err)
	}

	if currentStatus == targetStatus {
		return transaction.Commit(ctx)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE rental_requests SET status = $2, updated_at = $3 WHERE id = $1::uuid`,
		rentalID,
		targetStatus,
		now,
	); err != nil {
		return fmt.Errorf("update rental status: %w", err)
	}

	if targetStatus == StatusRejected || targetStatus == StatusCompleted {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tool_holds
			 SET status = 'released', updated_at = $2
			 WHERE rental_request_id = $1::uuid
			   AND status IN ('active', 'confirmed')`,
			rentalID,
			now,
		); err != nil {
			return fmt.Errorf("release rental hold: %w", err)
		}
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
		VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		rentalID,
		currentStatus,
		targetStatus,
		actorType,
		actorID,
		now,
	); err != nil {
		return fmt.Errorf("insert rental status history: %w", err)
	}

	return transaction.Commit(ctx)
}


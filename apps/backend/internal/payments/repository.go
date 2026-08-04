package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) PreparePayment(
	ctx context.Context,
	clientID string,
	rentalID string,
	receiptsEnabled bool,
	now time.Time,
) (Payment, CreateData, bool, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("begin payment creation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var ownerID string
	var rentalStatus string
	var rentalAmount int64
	var depositAmount int64
	var deliveryAmount int64
	var totalAmount int64
	var clientStatus string
	var phoneVerified bool
	var profileCompleted bool
	var offerAccepted bool
	var email pgtype.Text
	var holdStatus string
	var holdExpiresAt time.Time
	var paymentExpiresAt time.Time
	var toolName string
	err = transaction.QueryRow(
		ctx,
		`SELECT
			rr.client_id::text,
			rr.status,
			rr.rental_price,
			rr.deposit_amount,
			rr.delivery_cost,
			rr.total_amount,
			c.status,
			c.phone_verified_at IS NOT NULL,
			NULLIF(BTRIM(c.full_name), '') IS NOT NULL AND c.birth_date IS NOT NULL,
			EXISTS (
				SELECT 1
				FROM consent_acceptances AS ca
				WHERE ca.client_id = c.id
			),
			c.email,
			h.status,
			h.expires_at,
			rr.expires_at,
			t.name
		 FROM rental_requests AS rr
		 JOIN clients AS c ON c.id = rr.client_id
		 JOIN tool_holds AS h ON h.rental_request_id = rr.id
		 JOIN tools AS t ON t.id = rr.tool_id
		 WHERE rr.id = $1::uuid AND rr.client_id = $2::uuid
		 FOR UPDATE OF rr, c, h`,
		rentalID,
		clientID,
	).Scan(
		&ownerID,
		&rentalStatus,
		&rentalAmount,
		&depositAmount,
		&deliveryAmount,
		&totalAmount,
		&clientStatus,
		&phoneVerified,
		&profileCompleted,
		&offerAccepted,
		&email,
		&holdStatus,
		&holdExpiresAt,
		&paymentExpiresAt,
		&toolName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, CreateData{}, false, ErrRentalNotFound
	}
	if err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("lock rental for payment: %w", err)
	}

	data := CreateData{
		ToolName: toolName,
		ReceiptItems: receiptItems(
			toolName,
			rentalAmount,
			depositAmount,
			deliveryAmount,
		),
	}
	if email.Valid {
		data.ClientEmail = email.String
	}

	if ownerID != clientID ||
		clientStatus != "verified" ||
		!phoneVerified ||
		!profileCompleted ||
		!offerAccepted {
		return Payment{}, CreateData{}, false, ErrClientNotVerified
	}
	if rentalStatus != "awaiting_payment" {
		return Payment{}, CreateData{}, false, ErrRentalNotPayable
	}
	if !paymentExpiresAt.After(now) {
		return Payment{}, CreateData{}, false, ErrPaymentExpired
	}
	if holdStatus != "confirmed" || !holdExpiresAt.After(now) {
		return Payment{}, CreateData{}, false, ErrHoldNotConfirmed
	}
	if receiptsEnabled && data.ClientEmail == "" {
		return Payment{}, CreateData{}, false, ErrReceiptEmailRequired
	}

	existing, err := scanPayment(transaction.QueryRow(
		ctx,
		paymentSelect+`
		 WHERE rental_request_id = $1::uuid
		   AND status IN ('creating', 'pending', 'requires_review')
		 ORDER BY created_at DESC
		 LIMIT 1`,
		rentalID,
	))
	if err == nil {
		if err := transaction.Commit(ctx); err != nil {
			return Payment{}, CreateData{}, false, fmt.Errorf("commit existing payment: %w", err)
		}
		return existing, data, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, CreateData{}, false, fmt.Errorf("find existing rental payment: %w", err)
	}

	payment, err := scanPayment(transaction.QueryRow(
		ctx,
		`INSERT INTO payments (
			rental_request_id,
			idempotency_key,
			rental_amount,
			deposit_amount,
			delivery_amount,
			total_amount,
			currency,
			status,
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid,
			gen_random_uuid()::text,
			$2,
			$3,
			$4,
			$5,
			'RUB',
			'creating',
			$6,
			$6
		)
		RETURNING
			id::text,
			rental_request_id::text,
			provider_payment_id,
			idempotency_key,
			rental_amount,
			deposit_amount,
			delivery_amount,
			total_amount,
			currency,
			status,
			confirmation_url,
			provider_payload,
			created_at,
			updated_at,
			succeeded_at,
			cancelled_at`,
		rentalID,
		rentalAmount,
		depositAmount,
		deliveryAmount,
		totalAmount,
		now,
	))
	if err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("insert payment: %w", err)
	}

	items, err := json.Marshal(data.ReceiptItems)
	if err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("encode fiscal receipt items: %w", err)
	}
	receiptStatus := "disabled"
	if receiptsEnabled {
		receiptStatus = "pending"
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO fiscal_receipts (
			payment_id,
			status,
			items,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2, $3::jsonb, $4, $4)`,
		payment.ID,
		receiptStatus,
		items,
		now,
	); err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("insert fiscal receipt: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO deposits (
			rental_request_id,
			payment_id,
			original_amount,
			refundable_amount,
			status,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $3, 'pending', $4, $4)
		ON CONFLICT (rental_request_id) DO UPDATE
		SET
			payment_id = EXCLUDED.payment_id,
			original_amount = EXCLUDED.original_amount,
			refundable_amount = EXCLUDED.refundable_amount,
			refunded_amount = 0,
			withheld_amount = 0,
			status = 'pending',
			updated_at = EXCLUDED.updated_at
		WHERE deposits.status = 'pending'`,
		rentalID,
		payment.ID,
		depositAmount,
		now,
	); err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("insert deposit: %w", err)
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
		VALUES (
			$1::uuid,
			$2::uuid,
			'payment.created',
			'client',
			$3,
			jsonb_build_object('total_amount', $4::bigint, 'currency', 'RUB'),
			$5
		)`,
		payment.ID,
		rentalID,
		clientID,
		totalAmount,
		now,
	); err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("audit payment creation: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return Payment{}, CreateData{}, false, fmt.Errorf("commit payment creation: %w", err)
	}
	return payment, data, true, nil
}

func (repository *PostgresRepository) SaveProviderPayment(
	ctx context.Context,
	paymentID string,
	provider yookassa.Payment,
	now time.Time,
) (Payment, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Payment{}, fmt.Errorf("begin saving provider payment: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	confirmationURL := ""
	if provider.Confirmation != nil {
		confirmationURL = provider.Confirmation.ConfirmationURL
	}
	payload := provider.Raw
	if len(payload) == 0 {
		payload, err = json.Marshal(provider)
		if err != nil {
			return Payment{}, fmt.Errorf("encode provider payment: %w", err)
		}
	}
	payment, err := scanPayment(transaction.QueryRow(
		ctx,
		`UPDATE payments
		 SET
			provider_payment_id = $2,
			status = 'pending',
			confirmation_url = $3,
			provider_payload = $4::jsonb,
			updated_at = $5
		 WHERE id = $1::uuid AND status = 'creating'
		 RETURNING
			id::text,
			rental_request_id::text,
			provider_payment_id,
			idempotency_key,
			rental_amount,
			deposit_amount,
			delivery_amount,
			total_amount,
			currency,
			status,
			confirmation_url,
			provider_payload,
			created_at,
			updated_at,
			succeeded_at,
			cancelled_at`,
		paymentID,
		provider.ID,
		confirmationURL,
		payload,
		now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		payment, err = scanPayment(transaction.QueryRow(
			ctx,
			`UPDATE payments
			 SET
				provider_payment_id = COALESCE(provider_payment_id, $2),
				confirmation_url = NULL,
				provider_payload = $3::jsonb,
				updated_at = $4
			 WHERE id = $1::uuid
			   AND status IN ('canceled', 'expired')
			 RETURNING
				id::text,
				rental_request_id::text,
				provider_payment_id,
				idempotency_key,
				rental_amount,
				deposit_amount,
				delivery_amount,
				total_amount,
				currency,
				status,
				confirmation_url,
				provider_payload,
				created_at,
				updated_at,
				succeeded_at,
				cancelled_at`,
			paymentID,
			provider.ID,
			payload,
			now,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			payment, err = scanPayment(transaction.QueryRow(
				ctx,
				paymentSelect+" WHERE id = $1::uuid",
				paymentID,
			))
		}
	}
	if err != nil {
		return Payment{}, fmt.Errorf("save provider payment: %w", err)
	}

	receiptStatus := fiscalStatus(provider.ReceiptRegistration, true)
	if _, err := transaction.Exec(
		ctx,
		`UPDATE fiscal_receipts
		 SET
			status = CASE WHEN status = 'disabled' THEN 'disabled' ELSE $2 END,
			provider_registration_status = NULLIF($3, ''),
			provider_payload = $4::jsonb,
			updated_at = $5
		 WHERE payment_id = $1::uuid`,
		paymentID,
		receiptStatus,
		provider.ReceiptRegistration,
		payload,
		now,
	); err != nil {
		return Payment{}, fmt.Errorf("update fiscal receipt after payment creation: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Payment{}, fmt.Errorf("commit provider payment: %w", err)
	}
	return payment, nil
}

func (repository *PostgresRepository) MarkCreationForReview(
	ctx context.Context,
	paymentID string,
	message string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE payments
		 SET
			status = 'requires_review',
			provider_payload = jsonb_build_object('validation_error', $2::text),
			updated_at = $3
		 WHERE id = $1::uuid AND status = 'creating'`,
		paymentID,
		message,
		now,
	)
	if err != nil {
		return fmt.Errorf("mark payment for review: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) StoreEvent(
	ctx context.Context,
	eventKey string,
	providerPaymentID string,
	eventType string,
	raw json.RawMessage,
	now time.Time,
) (string, bool, error) {
	var id string
	err := repository.database.QueryRow(
		ctx,
		`INSERT INTO payment_events (
			event_key,
			provider_payment_id,
			event_type,
			raw_payload,
			status,
			next_attempt_at,
			created_at
		)
		VALUES ($1, $2, $3, $4::jsonb, 'pending', $5, $5)
		ON CONFLICT (event_key) DO NOTHING
		RETURNING id::text`,
		eventKey,
		providerPaymentID,
		eventType,
		raw,
		now,
	).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("store payment event: %w", err)
	}
	if err := repository.database.QueryRow(
		ctx,
		`SELECT id::text FROM payment_events WHERE event_key = $1`,
		eventKey,
	).Scan(&id); err != nil {
		return "", false, fmt.Errorf("get duplicate payment event: %w", err)
	}
	return id, false, nil
}

func (repository *PostgresRepository) ClaimEventByID(
	ctx context.Context,
	eventID string,
	now time.Time,
) (PaymentEvent, bool, error) {
	event, err := scanEvent(repository.database.QueryRow(
		ctx,
		`UPDATE payment_events
		 SET status = 'processing', attempts = attempts + 1, locked_at = $2
		 WHERE id = $1::uuid AND status IN ('pending', 'failed')
		 RETURNING
			id::text,
			event_key,
			provider_payment_id,
			event_type,
			raw_payload,
			attempts`,
		eventID,
		now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentEvent{}, false, nil
	}
	if err != nil {
		return PaymentEvent{}, false, fmt.Errorf("claim payment event: %w", err)
	}
	return event, true, nil
}

func (repository *PostgresRepository) ClaimNextEvent(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	maxAttempts int,
) (PaymentEvent, bool, error) {
	event, err := scanEvent(repository.database.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT id
			FROM payment_events
			WHERE
				attempts < $3
				AND (
					(status IN ('pending', 'failed') AND next_attempt_at <= $1)
					OR (status = 'processing' AND locked_at <= $2)
				)
			ORDER BY created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE payment_events AS pe
		SET status = 'processing', attempts = pe.attempts + 1, locked_at = $1
		FROM candidate
		WHERE pe.id = candidate.id
		RETURNING
			pe.id::text,
			pe.event_key,
			pe.provider_payment_id,
			pe.event_type,
			pe.raw_payload,
			pe.attempts`,
		now,
		staleBefore,
		maxAttempts,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentEvent{}, false, nil
	}
	if err != nil {
		return PaymentEvent{}, false, fmt.Errorf("claim next payment event: %w", err)
	}
	return event, true, nil
}

func (repository *PostgresRepository) GetByProviderID(
	ctx context.Context,
	providerPaymentID string,
) (Payment, error) {
	payment, err := scanPayment(repository.database.QueryRow(
		ctx,
		paymentSelect+" WHERE provider_payment_id = $1",
		providerPaymentID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrWebhookMismatch
	}
	if err != nil {
		return Payment{}, fmt.Errorf("get payment by provider id: %w", err)
	}
	return payment, nil
}

func (repository *PostgresRepository) CompleteSucceeded(
	ctx context.Context,
	event PaymentEvent,
	provider yookassa.Payment,
	receiptStatus string,
	now time.Time,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin successful payment: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var paymentID string
	var rentalID string
	var paymentStatus string
	var rentalStatus string
	var holdStatus string
	var paymentExpiresAt time.Time
	err = transaction.QueryRow(
		ctx,
		`SELECT
			p.id::text,
			p.rental_request_id::text,
			p.status,
			rr.status,
			h.status,
			rr.expires_at
		 FROM payments AS p
		 JOIN rental_requests AS rr ON rr.id = p.rental_request_id
		 JOIN tool_holds AS h ON h.rental_request_id = rr.id
		 WHERE p.provider_payment_id = $1
		 FOR UPDATE OF p, rr, h`,
		provider.ID,
	).Scan(
		&paymentID,
		&rentalID,
		&paymentStatus,
		&rentalStatus,
		&holdStatus,
		&paymentExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWebhookMismatch
	}
	if err != nil {
		return fmt.Errorf("lock successful payment: %w", err)
	}

	if paymentStatus != StatusSucceeded &&
		(rentalStatus != "awaiting_payment" ||
			holdStatus != "confirmed" ||
			!paymentExpiresAt.After(now)) {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE payments
			 SET
				status = 'requires_review',
				confirmation_url = NULL,
				provider_payload = $2::jsonb,
				updated_at = $3,
				succeeded_at = $3
			 WHERE id = $1::uuid`,
			paymentID,
			provider.Raw,
			now,
		); err != nil {
			return fmt.Errorf("mark late payment for review: %w", err)
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
			VALUES (
				$1::uuid,
				$2::uuid,
				'payment.succeeded_after_deadline',
				'provider',
				$3,
				jsonb_build_object(
					'event_id', $4::text,
					'rental_status', $5::text,
					'payment_expires_at', $6::timestamptz
				),
				$7
			)`,
			paymentID,
			rentalID,
			provider.ID,
			event.ID,
			rentalStatus,
			paymentExpiresAt,
			now,
		); err != nil {
			return fmt.Errorf("audit late successful payment: %w", err)
		}
	}

	if paymentStatus != StatusSucceeded &&
		rentalStatus == "awaiting_payment" &&
		holdStatus == "confirmed" &&
		paymentExpiresAt.After(now) {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE payments
			 SET
				status = 'succeeded',
				provider_payload = $2::jsonb,
				updated_at = $3,
				succeeded_at = $3
			 WHERE id = $1::uuid`,
			paymentID,
			provider.Raw,
			now,
		); err != nil {
			return fmt.Errorf("mark payment succeeded: %w", err)
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE rental_requests
			 SET status = 'paid', updated_at = $2
			 WHERE id = $1::uuid`,
			rentalID,
			now,
		); err != nil {
			return fmt.Errorf("mark rental paid: %w", err)
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
			VALUES ($1::uuid, 'awaiting_payment', 'paid', 'integration', $2, $3)`,
			rentalID,
			provider.ID,
			now,
		); err != nil {
			return fmt.Errorf("record paid rental status: %w", err)
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE deposits
			 SET status = 'paid', updated_at = $2
			 WHERE payment_id = $1::uuid`,
			paymentID,
			now,
		); err != nil {
			return fmt.Errorf("mark deposit paid: %w", err)
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
			VALUES (
				$1::uuid,
				$2::uuid,
				'payment.succeeded',
				'provider',
				$3,
				jsonb_build_object('event_id', $4::text),
				$5
			)`,
			paymentID,
			rentalID,
			provider.ID,
			event.ID,
			now,
		); err != nil {
			return fmt.Errorf("audit successful payment: %w", err)
		}
		payload, err := json.Marshal(map[string]string{
			"rental_id": rentalID,
			"status":    "paid",
		})
		if err != nil {
			return fmt.Errorf("encode paid Bitrix outbox: %w", err)
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
			"bitrix.deal.update:"+rentalID+":paid",
			payload,
			now,
		); err != nil {
			return fmt.Errorf("insert paid Bitrix outbox: %w", err)
		}
	}

	if err := updateReceiptTx(
		ctx,
		transaction,
		paymentID,
		provider,
		receiptStatus,
		now,
	); err != nil {
		return err
	}
	if err := completeEventTx(ctx, transaction, event.ID, paymentID, now); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit successful payment: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) CompleteCanceled(
	ctx context.Context,
	event PaymentEvent,
	provider yookassa.Payment,
	expired bool,
	receiptStatus string,
	now time.Time,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin canceled payment: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var paymentID string
	var rentalID string
	var currentPaymentStatus string
	var rentalStatus string
	err = transaction.QueryRow(
		ctx,
		`SELECT
			p.id::text,
			p.rental_request_id::text,
			p.status,
			rr.status
		 FROM payments AS p
		 JOIN rental_requests AS rr ON rr.id = p.rental_request_id
		 JOIN tool_holds AS h ON h.rental_request_id = rr.id
		 WHERE p.provider_payment_id = $1
		 FOR UPDATE OF p, rr, h`,
		provider.ID,
	).Scan(&paymentID, &rentalID, &currentPaymentStatus, &rentalStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWebhookMismatch
	}
	if err != nil {
		return fmt.Errorf("lock canceled payment: %w", err)
	}

	targetPaymentStatus := StatusCanceled
	if expired {
		targetPaymentStatus = StatusExpired
	}
	if currentPaymentStatus != StatusCanceled && currentPaymentStatus != StatusExpired {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE payments
			 SET
				status = $2,
				provider_payload = $3::jsonb,
				updated_at = $4,
				cancelled_at = $4
			 WHERE id = $1::uuid AND status <> 'succeeded'`,
			paymentID,
			targetPaymentStatus,
			provider.Raw,
			now,
		); err != nil {
			return fmt.Errorf("mark payment canceled: %w", err)
		}
		if expired && rentalStatus == "awaiting_payment" {
			if _, err := transaction.Exec(
				ctx,
				`UPDATE rental_requests
				 SET status = 'payment_expired', updated_at = $2
				 WHERE id = $1::uuid`,
				rentalID,
				now,
			); err != nil {
				return fmt.Errorf("expire rental after payment: %w", err)
			}
			if _, err := transaction.Exec(
				ctx,
				`UPDATE tool_holds
				 SET status = 'expired', updated_at = $2
				 WHERE rental_request_id = $1::uuid AND status = 'confirmed'`,
				rentalID,
				now,
			); err != nil {
				return fmt.Errorf("expire hold after payment: %w", err)
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
				VALUES (
					$1::uuid,
					'awaiting_payment',
					'payment_expired',
					'integration',
					$2,
					$3
				)`,
				rentalID,
				provider.ID,
				now,
			); err != nil {
				return fmt.Errorf("record expired rental status: %w", err)
			}
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
			VALUES (
				$1::uuid,
				$2::uuid,
				$3,
				'provider',
				$4,
				jsonb_build_object('event_id', $5::text),
				$6
			)`,
			paymentID,
			rentalID,
			"payment."+targetPaymentStatus,
			provider.ID,
			event.ID,
			now,
		); err != nil {
			return fmt.Errorf("audit canceled payment: %w", err)
		}
	}

	if err := updateReceiptTx(
		ctx,
		transaction,
		paymentID,
		provider,
		receiptStatus,
		now,
	); err != nil {
		return err
	}
	if err := completeEventTx(ctx, transaction, event.ID, paymentID, now); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit canceled payment: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RetryEvent(
	ctx context.Context,
	eventID string,
	nextAttemptAt time.Time,
	message string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE payment_events
		 SET
			status = 'failed',
			next_attempt_at = $2,
			locked_at = NULL,
			last_error = $3
		 WHERE id = $1::uuid`,
		eventID,
		nextAttemptAt,
		message,
	)
	if err != nil {
		return fmt.Errorf("schedule payment event retry: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RejectEvent(
	ctx context.Context,
	eventID string,
	message string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE payment_events
		 SET
			status = 'rejected',
			locked_at = NULL,
			last_error = $2,
			processed_at = $3
		 WHERE id = $1::uuid`,
		eventID,
		message,
		now,
	)
	if err != nil {
		return fmt.Errorf("reject payment event: %w", err)
	}
	return nil
}

func receiptItems(
	toolName string,
	rentalAmount int64,
	depositAmount int64,
	deliveryAmount int64,
) []yookassa.ReceiptItem {
	items := make([]yookassa.ReceiptItem, 0, 3)
	add := func(description string, amount int64, subject string) {
		if amount == 0 {
			return
		}
		items = append(items, yookassa.ReceiptItem{
			Description: description,
			Quantity:    "1.00",
			Amount: yookassa.Money{
				Value:    formatKopecks(amount),
				Currency: "RUB",
			},
			VATCode:        1,
			PaymentMode:    "full_payment",
			PaymentSubject: subject,
		})
	}
	add("Аренда: "+toolName, rentalAmount, "service")
	add("Обеспечительный платеж (залог)", depositAmount, "lien")
	add("Доставка", deliveryAmount, "service")
	return items
}

func updateReceiptTx(
	ctx context.Context,
	transaction pgx.Tx,
	paymentID string,
	provider yookassa.Payment,
	receiptStatus string,
	now time.Time,
) error {
	payload := provider.Raw
	if len(payload) == 0 {
		var err error
		payload, err = json.Marshal(provider)
		if err != nil {
			return fmt.Errorf("encode fiscal provider payload: %w", err)
		}
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE fiscal_receipts
		 SET
			status = CASE WHEN status = 'disabled' THEN 'disabled' ELSE $2 END,
			provider_registration_status = NULLIF($3, ''),
			provider_payload = $4::jsonb,
			updated_at = $5
		 WHERE payment_id = $1::uuid`,
		paymentID,
		receiptStatus,
		provider.ReceiptRegistration,
		payload,
		now,
	); err != nil {
		return fmt.Errorf("update fiscal receipt: %w", err)
	}
	return nil
}

func completeEventTx(
	ctx context.Context,
	transaction pgx.Tx,
	eventID string,
	paymentID string,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE payment_events
		 SET
			status = 'completed',
			payment_id = $2::uuid,
			locked_at = NULL,
			last_error = NULL,
			processed_at = $3
		 WHERE id = $1::uuid`,
		eventID,
		paymentID,
		now,
	); err != nil {
		return fmt.Errorf("complete payment event: %w", err)
	}
	return nil
}

const paymentSelect = `SELECT
	id::text,
	rental_request_id::text,
	provider_payment_id,
	idempotency_key,
	rental_amount,
	deposit_amount,
	delivery_amount,
	total_amount,
	currency,
	status,
	confirmation_url,
	provider_payload,
	created_at,
	updated_at,
	succeeded_at,
	cancelled_at
 FROM payments`

type rowScanner interface {
	Scan(...any) error
}

func scanPayment(row rowScanner) (Payment, error) {
	var payment Payment
	var providerID pgtype.Text
	var confirmationURL pgtype.Text
	var succeededAt pgtype.Timestamptz
	var cancelledAt pgtype.Timestamptz
	if err := row.Scan(
		&payment.ID,
		&payment.RentalRequestID,
		&providerID,
		&payment.IdempotencyKey,
		&payment.RentalAmount,
		&payment.DepositAmount,
		&payment.DeliveryAmount,
		&payment.TotalAmount,
		&payment.Currency,
		&payment.Status,
		&confirmationURL,
		&payment.ProviderPayload,
		&payment.CreatedAt,
		&payment.UpdatedAt,
		&succeededAt,
		&cancelledAt,
	); err != nil {
		return Payment{}, err
	}
	if providerID.Valid {
		payment.ProviderPaymentID = &providerID.String
	}
	if confirmationURL.Valid {
		payment.ConfirmationURL = &confirmationURL.String
	}
	if succeededAt.Valid {
		payment.SucceededAt = &succeededAt.Time
	}
	if cancelledAt.Valid {
		payment.CancelledAt = &cancelledAt.Time
	}
	return payment, nil
}

func scanEvent(row rowScanner) (PaymentEvent, error) {
	var event PaymentEvent
	if err := row.Scan(
		&event.ID,
		&event.EventKey,
		&event.ProviderPaymentID,
		&event.EventType,
		&event.RawPayload,
		&event.Attempts,
	); err != nil {
		return PaymentEvent{}, err
	}
	return event, nil
}

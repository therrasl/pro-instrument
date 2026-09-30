package push

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) RegisterToken(
	ctx context.Context,
	clientID string,
	token string,
	platform string,
	now time.Time,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin push token registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var tokenID string
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO client_push_tokens (
			client_id, token, platform, enabled, created_at, updated_at, disabled_at
		)
		VALUES ($1::uuid, $2, $3, TRUE, $4, $4, NULL)
		ON CONFLICT (token) DO UPDATE SET
			client_id = EXCLUDED.client_id,
			platform = EXCLUDED.platform,
			enabled = TRUE,
			updated_at = EXCLUDED.updated_at,
			disabled_at = NULL
		RETURNING id::text`,
		clientID,
		token,
		platform,
		now,
	).Scan(&tokenID)
	if err != nil {
		return fmt.Errorf("upsert push token: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE push_notification_deliveries AS delivery
		 SET status = 'failed', last_error = 'token reassigned', locked_at = NULL
		 FROM push_notification_events AS event
		 WHERE delivery.event_id = event.id
		   AND delivery.push_token_id = $1::uuid
		   AND event.client_id <> $2::uuid
		   AND delivery.status IN ('pending', 'processing', 'failed')`,
		tokenID,
		clientID,
	); err != nil {
		return fmt.Errorf("discard reassigned token deliveries: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit push token registration: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) DeleteToken(
	ctx context.Context,
	clientID string,
	token string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE client_push_tokens
		 SET enabled = FALSE, updated_at = $3, disabled_at = $3
		 WHERE client_id = $1::uuid AND token = $2`,
		clientID,
		token,
		now,
	)
	if err != nil {
		return fmt.Errorf("disable client push token: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) GetActiveTokensForClient(
	ctx context.Context,
	clientID string,
) ([]string, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT token
		 FROM client_push_tokens
		 WHERE client_id = $1::uuid AND enabled = TRUE`,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active push tokens: %w", err)
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, fmt.Errorf("scan active push token: %w", err)
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (repository *PostgresRepository) ClaimDelivery(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	maxAttempts int,
) (Delivery, bool, error) {
	var delivery Delivery
	err := repository.database.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT delivery.id
			FROM push_notification_deliveries AS delivery
			JOIN client_push_tokens AS token ON token.id = delivery.push_token_id
			JOIN push_notification_events AS event ON event.id = delivery.event_id
			WHERE token.enabled = TRUE
			  AND token.client_id = event.client_id
			  AND delivery.attempts < $3
			  AND (
				(delivery.status IN ('pending', 'failed') AND delivery.next_attempt_at <= $1)
				OR (delivery.status = 'processing' AND delivery.locked_at <= $2)
			  )
			ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
			FOR UPDATE OF delivery SKIP LOCKED
			LIMIT 1
		), claimed AS (
			UPDATE push_notification_deliveries AS delivery
			SET status = 'processing', attempts = attempts + 1, locked_at = $1
			FROM candidate
			WHERE delivery.id = candidate.id
			RETURNING delivery.*
		)
		SELECT
			claimed.id::text,
			token.id::text,
			token.token,
			event.rental_request_id::text,
			event.status,
			claimed.attempts,
			claimed.created_at
		FROM claimed
		JOIN client_push_tokens AS token ON token.id = claimed.push_token_id
		JOIN push_notification_events AS event ON event.id = claimed.event_id`,
		now,
		staleBefore,
		maxAttempts,
	).Scan(
		&delivery.ID,
		&delivery.TokenID,
		&delivery.Token,
		&delivery.RentalID,
		&delivery.Status,
		&delivery.Attempts,
		&delivery.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, fmt.Errorf("claim push delivery: %w", err)
	}
	return delivery, true, nil
}

func (repository *PostgresRepository) SaveTicket(
	ctx context.Context,
	deliveryID string,
	ticketID string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE push_notification_deliveries
		 SET status = 'ticketed', ticket_id = $2, next_attempt_at = $3,
		     locked_at = NULL, last_error = NULL
		 WHERE id = $1::uuid AND status = 'processing'`,
		deliveryID,
		ticketID,
		now,
	)
	if err != nil {
		return fmt.Errorf("save Expo push ticket: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) ClaimReceipt(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	maxAttempts int,
) (Receipt, bool, error) {
	var receipt Receipt
	err := repository.database.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT delivery.id
			FROM push_notification_deliveries AS delivery
			JOIN client_push_tokens AS token ON token.id = delivery.push_token_id
			WHERE token.enabled = TRUE
			  AND delivery.receipt_attempts < $3
			  AND delivery.ticket_id IS NOT NULL
			  AND (
				(delivery.status = 'ticketed' AND delivery.next_attempt_at <= $1)
				OR (delivery.status = 'checking' AND delivery.locked_at <= $2)
			  )
			ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
			FOR UPDATE OF delivery SKIP LOCKED
			LIMIT 1
		), claimed AS (
			UPDATE push_notification_deliveries AS delivery
			SET status = 'checking', receipt_attempts = receipt_attempts + 1, locked_at = $1
			FROM candidate
			WHERE delivery.id = candidate.id
			RETURNING delivery.*
		)
		SELECT claimed.id::text, claimed.push_token_id::text, claimed.ticket_id,
		       claimed.receipt_attempts
		FROM claimed`,
		now,
		staleBefore,
		maxAttempts,
	).Scan(&receipt.DeliveryID, &receipt.TokenID, &receipt.TicketID, &receipt.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, fmt.Errorf("claim Expo push receipt: %w", err)
	}
	return receipt, true, nil
}

func (repository *PostgresRepository) CompleteReceipt(
	ctx context.Context,
	deliveryID string,
	now time.Time,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE push_notification_deliveries
		 SET status = 'sent', sent_at = $2, locked_at = NULL, last_error = NULL
		 WHERE id = $1::uuid AND status = 'checking'`,
		deliveryID,
		now,
	)
	if err != nil {
		return fmt.Errorf("complete Expo push receipt: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RetryReceipt(
	ctx context.Context,
	deliveryID string,
	nextAttemptAt time.Time,
	message string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE push_notification_deliveries
		 SET status = 'ticketed', next_attempt_at = $2, last_error = $3, locked_at = NULL
		 WHERE id = $1::uuid AND status = 'checking'`,
		deliveryID,
		nextAttemptAt,
		message,
	)
	if err != nil {
		return fmt.Errorf("retry Expo push receipt: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) RetryDelivery(
	ctx context.Context,
	deliveryID string,
	nextAttemptAt time.Time,
	message string,
) error {
	_, err := repository.database.Exec(
		ctx,
		`UPDATE push_notification_deliveries
		 SET status = 'failed', next_attempt_at = $2, last_error = $3, locked_at = NULL
		 WHERE id = $1::uuid AND status = 'processing'`,
		deliveryID,
		nextAttemptAt,
		message,
	)
	if err != nil {
		return fmt.Errorf("retry push delivery: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) DisableInvalidToken(
	ctx context.Context,
	tokenID string,
	deliveryID string,
	message string,
	now time.Time,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin invalid push token disable: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(
		ctx,
		`UPDATE client_push_tokens
		 SET enabled = FALSE, disabled_at = $2, updated_at = $2
		 WHERE id = $1::uuid`,
		tokenID,
		now,
	); err != nil {
		return fmt.Errorf("disable invalid push token: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE push_notification_deliveries
		 SET status = 'failed', last_error = $2, locked_at = NULL
		 WHERE id = $1::uuid`,
		deliveryID,
		message,
	); err != nil {
		return fmt.Errorf("fail invalid push delivery: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit invalid push token disable: %w", err)
	}
	return nil
}

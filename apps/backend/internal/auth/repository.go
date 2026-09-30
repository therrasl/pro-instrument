package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
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

func (repository *PostgresRepository) CreateVerificationCode(
	ctx context.Context,
	phone string,
	codeHash []byte,
	expiresAt time.Time,
	createdAt time.Time,
	cooldown time.Duration,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create verification code: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	if _, err := transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		phone,
	); err != nil {
		return fmt.Errorf("lock verification phone: %w", err)
	}

	var latestCreatedAt time.Time
	err = transaction.QueryRow(
		ctx,
		`SELECT created_at
		 FROM phone_verification_codes
		 WHERE phone = $1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		phone,
	).Scan(&latestCreatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("query latest verification code: %w", err)
	}
	if err == nil && createdAt.Sub(latestCreatedAt) < cooldown {
		return ErrRateLimited
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE phone_verification_codes
		 SET consumed_at = $2
		 WHERE phone = $1 AND consumed_at IS NULL`,
		phone,
		createdAt,
	); err != nil {
		return fmt.Errorf("invalidate verification codes: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO phone_verification_codes (phone, code_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4)`,
		phone,
		codeHash,
		expiresAt,
		createdAt,
	); err != nil {
		return fmt.Errorf("insert verification code: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit verification code: %w", err)
	}

	return nil
}

func (repository *PostgresRepository) VerifyCodeAndCreateSession(
	ctx context.Context,
	phone string,
	codeHash []byte,
	tokenHash []byte,
	now time.Time,
	sessionExpiresAt time.Time,
	maxAttempts int,
) (Client, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Client{}, fmt.Errorf("begin verify code: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var codeID string
	var storedHash []byte
	var codeExpiresAt time.Time
	var attempts int
	err = transaction.QueryRow(
		ctx,
		`SELECT id::text, code_hash, expires_at, attempts
		 FROM phone_verification_codes
		 WHERE phone = $1 AND consumed_at IS NULL
		 ORDER BY created_at DESC
		 LIMIT 1
		 FOR UPDATE`,
		phone,
	).Scan(&codeID, &storedHash, &codeExpiresAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrInvalidCode
	}
	if err != nil {
		return Client{}, fmt.Errorf("query verification code: %w", err)
	}

	if !now.Before(codeExpiresAt) || attempts >= maxAttempts {
		return Client{}, repository.consumeInvalidCode(ctx, transaction, codeID, attempts, maxAttempts, now)
	}

	if subtle.ConstantTimeCompare(storedHash, codeHash) != 1 {
		return Client{}, repository.consumeInvalidCode(ctx, transaction, codeID, attempts+1, maxAttempts, now)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE phone_verification_codes
		 SET consumed_at = $2
		 WHERE id = $1::uuid`,
		codeID,
		now,
	); err != nil {
		return Client{}, fmt.Errorf("consume verification code: %w", err)
	}

	client, err := scanClient(transaction.QueryRow(
		ctx,
		`INSERT INTO clients (phone, phone_verified_at, status)
		 VALUES ($1, $2, 'phone_verified')
		 ON CONFLICT (phone) DO UPDATE SET
		 	phone_verified_at = COALESCE(clients.phone_verified_at, EXCLUDED.phone_verified_at),
		 	status = CASE
		 		WHEN clients.status = 'registered' THEN 'phone_verified'
		 		ELSE clients.status
		 	END,
		 	updated_at = $2
		 RETURNING
		 	id::text,
		 	phone,
		 	phone_verified_at,
            (
                SELECT MAX(accepted_at)
                FROM consent_acceptances
                WHERE
                    client_id = clients.id
                    AND offer_accepted
                    AND privacy_accepted
                    AND data_accuracy_confirmed
                    AND rental_rules_accepted
            ),
		 	full_name,
		 	birth_date::text,
		 	email,
			client_type, company_name, inn, kpp, ogrn, legal_address, company_contact,
		 	status,
            (
                SELECT reason
                FROM verification_reviews
                WHERE client_id = clients.id AND decision = 'rejected'
                ORDER BY created_at DESC
                LIMIT 1
            ),
		 	created_at,
		 	updated_at`,
		phone,
		now,
	))
	if err != nil {
		return Client{}, err
	}

	if client.Status == "blocked" {
		if err := transaction.Commit(ctx); err != nil {
			return Client{}, fmt.Errorf("commit blocked verification: %w", err)
		}
		return Client{}, ErrInvalidCode
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO auth_sessions (client_id, token_hash, expires_at, created_at)
		 VALUES ($1::uuid, $2, $3, $4)`,
		client.ID,
		tokenHash,
		sessionExpiresAt,
		now,
	); err != nil {
		return Client{}, fmt.Errorf("insert auth session: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return Client{}, fmt.Errorf("commit verified session: %w", err)
	}

	return client, nil
}

func (repository *PostgresRepository) consumeInvalidCode(
	ctx context.Context,
	transaction pgx.Tx,
	codeID string,
	attempts int,
	maxAttempts int,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE phone_verification_codes
		 SET
			attempts = $2::integer,
			consumed_at = CASE
				WHEN $2::integer >= $3::integer OR expires_at <= $4::timestamptz
				THEN $4::timestamptz
				ELSE consumed_at
			END
		 WHERE id = $1::uuid`,
		codeID,
		attempts,
		maxAttempts,
		now,
	); err != nil {
		return fmt.Errorf("record invalid verification attempt: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit invalid verification attempt: %w", err)
	}

	return ErrInvalidCode
}

func (repository *PostgresRepository) AuthenticateSession(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (Client, error) {
	var sessionID string
	client, err := scanSessionClient(repository.database.QueryRow(
		ctx,
		`SELECT
			s.id::text,
			c.id::text,
			c.phone,
			c.phone_verified_at,
			(
				SELECT MAX(accepted_at)
				FROM consent_acceptances
				WHERE
					client_id = c.id
					AND offer_accepted
					AND privacy_accepted
					AND data_accuracy_confirmed
					AND rental_rules_accepted
			),
			c.full_name,
			c.birth_date::text,
			c.email,
			c.client_type, c.company_name, c.inn, c.kpp, c.ogrn,
			c.legal_address, c.company_contact,
			c.status,
			(
				SELECT reason
				FROM verification_reviews
				WHERE client_id = c.id AND decision = 'rejected'
				ORDER BY created_at DESC
				LIMIT 1
			),
			c.created_at,
			c.updated_at
		 FROM auth_sessions AS s
		 JOIN clients AS c ON c.id = s.client_id
		 WHERE
		 	s.token_hash = $1
		 	AND s.revoked_at IS NULL
		 	AND s.expires_at > $2
		 	AND c.status <> 'blocked'`,
		tokenHash,
		now,
	), &sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrUnauthorized
	}
	if err != nil {
		return Client{}, err
	}

	if _, err := repository.database.Exec(
		ctx,
		`UPDATE auth_sessions SET last_used_at = $2 WHERE id = $1::uuid`,
		sessionID,
		now,
	); err != nil {
		return Client{}, fmt.Errorf("update session use: %w", err)
	}

	return client, nil
}

func (repository *PostgresRepository) UpdateProfile(
	ctx context.Context,
	clientID string,
	patch ProfilePatch,
) (Client, error) {
	client, err := scanClient(repository.database.QueryRow(
		ctx,
		`UPDATE clients
		 SET
		 	full_name = COALESCE($2::text, full_name),
		 	birth_date = COALESCE($3::date, birth_date),
            email = CASE
                WHEN $4::text IS NULL THEN email
                ELSE NULLIF(BTRIM($4::text), '')
            END,
			client_type = COALESCE($5::text, client_type),
			company_name = COALESCE($6::text, company_name),
			inn = COALESCE($7::text, inn),
			kpp = COALESCE($8::text, kpp),
			ogrn = COALESCE($9::text, ogrn),
			legal_address = COALESCE($10::text, legal_address),
			company_contact = COALESCE($11::text, company_contact),
			status = CASE
			WHEN status IN ('registered', 'phone_verified')
				AND NULLIF(BTRIM(COALESCE($2::text, full_name)), '') IS NOT NULL
				AND (
					(COALESCE($5::text, client_type) = 'individual' AND COALESCE($3::date, birth_date) IS NOT NULL)
					OR (COALESCE($5::text, client_type) = 'legal_entity'
						AND NULLIF(BTRIM(COALESCE($6::text, company_name)), '') IS NOT NULL
						AND NULLIF(BTRIM(COALESCE($7::text, inn)), '') IS NOT NULL
						AND NULLIF(BTRIM(COALESCE($9::text, ogrn)), '') IS NOT NULL
						AND NULLIF(BTRIM(COALESCE($10::text, legal_address)), '') IS NOT NULL)
				)
		 		THEN 'profile_completed'
		 		ELSE status
		 	END,
		 	updated_at = NOW()
		 WHERE
            id = $1::uuid
            AND status <> 'blocked'
            AND EXISTS (
                SELECT 1
                FROM consent_acceptances
                WHERE
                    client_id = clients.id
                    AND offer_accepted
                    AND privacy_accepted
                    AND data_accuracy_confirmed
                    AND rental_rules_accepted
            )
		 RETURNING
		 	id::text,
		 	phone,
		 	phone_verified_at,
            (
                SELECT MAX(accepted_at)
                FROM consent_acceptances
                WHERE
                    client_id = clients.id
                    AND offer_accepted
                    AND privacy_accepted
                    AND data_accuracy_confirmed
                    AND rental_rules_accepted
            ),
		 	full_name,
		 	birth_date::text,
		 	email,
			client_type, company_name, inn, kpp, ogrn, legal_address, company_contact,
		 	status,
            (
                SELECT reason
                FROM verification_reviews
                WHERE client_id = clients.id AND decision = 'rejected'
                ORDER BY created_at DESC
                LIMIT 1
            ),
		 	created_at,
		 	updated_at`,
		clientID,
		nullableString(patch.FullName),
		nullableString(patch.BirthDate),
		nullableString(patch.Email),
		nullableString(patch.ClientType),
		nullableString(patch.CompanyName),
		nullableString(patch.INN),
		nullableString(patch.KPP),
		nullableString(patch.OGRN),
		nullableString(patch.LegalAddress),
		nullableString(patch.CompanyContact),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrUnauthorized
	}
	if err != nil {
		return Client{}, err
	}

	return client, nil
}

func (repository *PostgresRepository) CreateConsent(
	ctx context.Context,
	clientID string,
	input ConsentInput,
) (ConsentAcceptance, error) {
	var acceptance ConsentAcceptance
	err := repository.database.QueryRow(
		ctx,
		`INSERT INTO consent_acceptances (
			client_id,
			offer_version,
			privacy_version,
			offer_accepted,
			privacy_accepted,
			data_accuracy_confirmed,
			rental_rules_accepted,
			accepted_at,
			ip_address,
			user_agent
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::inet, $10)
		ON CONFLICT (client_id, offer_version, privacy_version) DO UPDATE SET
			offer_version = EXCLUDED.offer_version
		RETURNING
			id::text,
			offer_version,
			privacy_version,
			offer_accepted,
			privacy_accepted,
			data_accuracy_confirmed,
			rental_rules_accepted,
			accepted_at`,
		clientID,
		input.OfferVersion,
		input.PrivacyVersion,
		input.OfferAccepted,
		input.PrivacyAccepted,
		input.DataAccuracyConfirmed,
		input.RentalRulesAccepted,
		input.AcceptedAt,
		input.IPAddress,
		input.UserAgent,
	).Scan(
		&acceptance.ID,
		&acceptance.OfferVersion,
		&acceptance.PrivacyVersion,
		&acceptance.OfferAccepted,
		&acceptance.PrivacyAccepted,
		&acceptance.DataAccuracyConfirmed,
		&acceptance.RentalRulesAccepted,
		&acceptance.AcceptedAt,
	)
	if err != nil {
		return ConsentAcceptance{}, fmt.Errorf("create consent acceptance: %w", err)
	}

	return acceptance, nil
}

type clientScanner interface {
	Scan(...any) error
}

func scanClient(row clientScanner) (Client, error) {
	var client Client
	if err := row.Scan(
		&client.ID,
		&client.Phone,
		&client.PhoneVerifiedAt,
		&client.OfferAcceptedAt,
		&client.FullName,
		&client.BirthDate,
		&client.Email,
		&client.ClientType,
		&client.CompanyName,
		&client.INN,
		&client.KPP,
		&client.OGRN,
		&client.LegalAddress,
		&client.CompanyContact,
		&client.Status,
		&client.VerificationRejectionReason,
		&client.CreatedAt,
		&client.UpdatedAt,
	); err != nil {
		return Client{}, fmt.Errorf("scan client: %w", err)
	}
	client.setProgress()
	return client, nil
}

func scanSessionClient(row clientScanner, sessionID *string) (Client, error) {
	var client Client
	if err := row.Scan(
		sessionID,
		&client.ID,
		&client.Phone,
		&client.PhoneVerifiedAt,
		&client.OfferAcceptedAt,
		&client.FullName,
		&client.BirthDate,
		&client.Email,
		&client.ClientType,
		&client.CompanyName,
		&client.INN,
		&client.KPP,
		&client.OGRN,
		&client.LegalAddress,
		&client.CompanyContact,
		&client.Status,
		&client.VerificationRejectionReason,
		&client.CreatedAt,
		&client.UpdatedAt,
	); err != nil {
		return Client{}, fmt.Errorf("scan authenticated client: %w", err)
	}
	client.setProgress()
	return client, nil
}

func (client *Client) setProgress() {
	client.PhoneVerified = client.PhoneVerifiedAt != nil
	client.OfferAccepted = client.OfferAcceptedAt != nil
	client.ProfileCompleted = client.FullName != nil && strings.TrimSpace(*client.FullName) != ""
	if client.ClientType == "legal_entity" {
		client.ProfileCompleted = client.ProfileCompleted && client.CompanyName != nil &&
			client.INN != nil && client.OGRN != nil && client.LegalAddress != nil
	} else {
		client.ProfileCompleted = client.ProfileCompleted && client.BirthDate != nil
	}
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

package verification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	database *pgxpool.Pool
}

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) CreateDocument(
	ctx context.Context,
	input Document,
) (Document, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Document{}, fmt.Errorf("begin create document: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var document Document
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO client_documents (
			client_id,
			document_type,
			storage_key,
			mime_type,
			size_bytes
		)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING
			id::text,
			client_id::text,
			document_type,
			storage_key,
			mime_type,
			size_bytes,
			created_at,
			updated_at`,
		input.ClientID,
		input.DocumentType,
		input.StorageKey,
		input.MIMEType,
		input.SizeBytes,
	).Scan(
		&document.ID,
		&document.ClientID,
		&document.DocumentType,
		&document.StorageKey,
		&document.MIMEType,
		&document.SizeBytes,
		&document.CreatedAt,
		&document.UpdatedAt,
	)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return Document{}, ErrDocumentExists
	}
	if err != nil {
		return Document{}, fmt.Errorf("insert client document: %w", err)
	}

	if err := insertAuditLog(
		ctx,
		transaction,
		document.ClientID,
		&document.ID,
		nil,
		document.ClientID,
		"document_uploaded",
		map[string]any{
			"document_type": document.DocumentType,
			"mime_type":     document.MIMEType,
			"size_bytes":    document.SizeBytes,
		},
	); err != nil {
		return Document{}, err
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE clients
		 SET status = 'documents_uploaded', updated_at = NOW()
		 WHERE
		 	id = $1::uuid
		 	AND status IN (
		 		'phone_verified',
		 		'profile_completed',
		 		'verification_rejected'
		 	)
		 	AND (
		 		SELECT COUNT(DISTINCT document_type)
		 		FROM client_documents
		 		WHERE client_id = $1::uuid
		 	) = 3`,
		document.ClientID,
	); err != nil {
		return Document{}, fmt.Errorf("update document completion status: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return Document{}, fmt.Errorf("commit document upload: %w", err)
	}

	return document, nil
}

func (repository *PostgresRepository) ListDocuments(
	ctx context.Context,
	clientID string,
) ([]Document, error) {
	rows, err := repository.database.Query(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			document_type,
			storage_key,
			mime_type,
			size_bytes,
			created_at,
			updated_at
		 FROM client_documents
		 WHERE client_id = $1::uuid
		 ORDER BY created_at, id`,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("query client documents: %w", err)
	}
	defer rows.Close()

	documents := make([]Document, 0)
	for rows.Next() {
		var document Document
		if err := rows.Scan(
			&document.ID,
			&document.ClientID,
			&document.DocumentType,
			&document.StorageKey,
			&document.MIMEType,
			&document.SizeBytes,
			&document.CreatedAt,
			&document.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan client document: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate client documents: %w", err)
	}

	return documents, nil
}

func (repository *PostgresRepository) GetDocument(
	ctx context.Context,
	clientID string,
	documentID string,
) (Document, error) {
	var document Document
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			document_type,
			storage_key,
			mime_type,
			size_bytes,
			created_at,
			updated_at
		 FROM client_documents
		 WHERE id = $1::uuid AND client_id = $2::uuid`,
		documentID,
		clientID,
	).Scan(
		&document.ID,
		&document.ClientID,
		&document.DocumentType,
		&document.StorageKey,
		&document.MIMEType,
		&document.SizeBytes,
		&document.CreatedAt,
		&document.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("get client document: %w", err)
	}
	return document, nil
}

func (repository *PostgresRepository) GetDocumentByID(
	ctx context.Context,
	documentID string,
) (Document, error) {
	var document Document
	err := repository.database.QueryRow(
		ctx,
		`SELECT
			id::text,
			client_id::text,
			document_type,
			storage_key,
			mime_type,
			size_bytes,
			created_at,
			updated_at
		 FROM client_documents
		 WHERE id = $1::uuid`,
		documentID,
	).Scan(
		&document.ID,
		&document.ClientID,
		&document.DocumentType,
		&document.StorageKey,
		&document.MIMEType,
		&document.SizeBytes,
		&document.CreatedAt,
		&document.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("get document by id: %w", err)
	}
	return document, nil
}

func (repository *PostgresRepository) DeleteDocument(
	ctx context.Context,
	clientID string,
	documentID string,
) (Document, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Document{}, fmt.Errorf("begin delete document: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var document Document
	err = transaction.QueryRow(
		ctx,
		`DELETE FROM client_documents
		 WHERE id = $1::uuid AND client_id = $2::uuid
		 RETURNING
		 	id::text,
		 	client_id::text,
		 	document_type,
		 	storage_key,
		 	mime_type,
		 	size_bytes,
		 	created_at,
		 	updated_at`,
		documentID,
		clientID,
	).Scan(
		&document.ID,
		&document.ClientID,
		&document.DocumentType,
		&document.StorageKey,
		&document.MIMEType,
		&document.SizeBytes,
		&document.CreatedAt,
		&document.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("delete client document: %w", err)
	}

	if err := insertAuditLog(
		ctx,
		transaction,
		document.ClientID,
		&document.ID,
		nil,
		document.ClientID,
		"document_deleted",
		map[string]any{"document_type": document.DocumentType},
	); err != nil {
		return Document{}, err
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE clients
		 SET
		 	status = CASE
		 		WHEN NULLIF(BTRIM(full_name), '') IS NOT NULL AND birth_date IS NOT NULL
		 			THEN 'profile_completed'
		 		ELSE 'phone_verified'
		 	END,
		 	updated_at = NOW()
		 WHERE id = $1::uuid
		    AND status IN (
		        'documents_uploaded',
		        'pending_verification',
		        'verification_rejected',
		        'verified'
		    )`,
		clientID,
	); err != nil {
		return Document{}, fmt.Errorf("invalidate verification after deletion: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return Document{}, fmt.Errorf("commit document deletion: %w", err)
	}

	return document, nil
}

func (repository *PostgresRepository) SubmitDocuments(
	ctx context.Context,
	clientID string,
) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin document submission: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var status string
	err = transaction.QueryRow(
		ctx,
		`SELECT status
		 FROM clients
		 WHERE id = $1::uuid
		 FOR UPDATE`,
		clientID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSubmissionNotAllowed
	}
	if err != nil {
		return fmt.Errorf("get document submission status: %w", err)
	}

	var documentCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT COUNT(DISTINCT document_type)
		 FROM client_documents
		 WHERE client_id = $1::uuid`,
		clientID,
	).Scan(&documentCount); err != nil {
		return fmt.Errorf("count documents for submission: %w", err)
	}
	if documentCount != len(requiredDocumentTypes) {
		return ErrDocumentsIncomplete
	}

	if status == "pending_verification" {
		return transaction.Commit(ctx)
	}
	switch status {
	case "phone_verified", "profile_completed", "documents_uploaded", "verification_rejected":
	default:
		return ErrSubmissionNotAllowed
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE clients
		 SET
		    status = 'pending_verification',
		    updated_at = NOW()
		 WHERE id = $1::uuid`,
		clientID,
	); err != nil {
		return fmt.Errorf("mark documents pending verification: %w", err)
	}

	if err := insertAuditLog(
		ctx,
		transaction,
		clientID,
		nil,
		nil,
		clientID,
		"verification_submitted",
		map[string]any{"document_count": documentCount},
	); err != nil {
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit document submission: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) CreateReview(
	ctx context.Context,
	clientID string,
	reviewerID string,
	decision string,
	reason *string,
	createdAt time.Time,
) (Review, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Review{}, fmt.Errorf("begin verification review: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var status string
	err = transaction.QueryRow(
		ctx,
		`SELECT status FROM clients WHERE id = $1::uuid FOR UPDATE`,
		clientID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Review{}, ErrReviewNotAllowed
	}
	if err != nil {
		return Review{}, fmt.Errorf("lock client verification: %w", err)
	}
	if status == "verified" && decision == "approved" {
		return Review{ClientID: clientID, ReviewerID: reviewerID, Decision: decision, CreatedAt: createdAt}, nil
	}
	if status == "verification_rejected" && decision == "rejected" {
		return Review{ClientID: clientID, ReviewerID: reviewerID, Decision: decision, Reason: reason, CreatedAt: createdAt}, nil
	}
	if status != "pending_verification" && status != "documents_uploaded" && status != "phone_verified" && status != "profile_completed" && status != "verification_rejected" {
		return Review{}, ErrReviewNotAllowed
	}

	if decision == "approved" && reviewerID != "bitrix_manager" {
		var documentCount int
		if err := transaction.QueryRow(
			ctx,
			`SELECT COUNT(DISTINCT document_type)
			 FROM client_documents
			 WHERE client_id = $1::uuid`,
			clientID,
		).Scan(&documentCount); err != nil {
			return Review{}, fmt.Errorf("count verification documents: %w", err)
		}
		if documentCount != len(requiredDocumentTypes) {
			return Review{}, ErrReviewNotAllowed
		}
	}

	var review Review
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO verification_reviews (
			client_id,
			reviewer_id,
			decision,
			reason,
			created_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text, client_id::text, reviewer_id, decision, reason, created_at`,
		clientID,
		reviewerID,
		decision,
		reason,
		createdAt,
	).Scan(
		&review.ID,
		&review.ClientID,
		&review.ReviewerID,
		&review.Decision,
		&review.Reason,
		&review.CreatedAt,
	)
	if err != nil {
		return Review{}, fmt.Errorf("insert verification review: %w", err)
	}

	clientStatus := "verified"
	action := "verification_approved"
	if decision == "rejected" {
		clientStatus = "verification_rejected"
		action = "verification_rejected"
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE clients SET status = $2, updated_at = $3 WHERE id = $1::uuid`,
		clientID,
		clientStatus,
		createdAt,
	); err != nil {
		return Review{}, fmt.Errorf("update verification status: %w", err)
	}

	details := map[string]any{"decision": decision}
	if reason != nil {
		details["reason"] = *reason
	}
	if err := insertAuditLog(
		ctx,
		transaction,
		clientID,
		nil,
		&review.ID,
		reviewerID,
		action,
		details,
	); err != nil {
		return Review{}, err
	}

	if err := transaction.Commit(ctx); err != nil {
		return Review{}, fmt.Errorf("commit verification review: %w", err)
	}

	return review, nil
}

func insertAuditLog(
	ctx context.Context,
	transaction pgx.Tx,
	clientID string,
	documentID *string,
	reviewID *string,
	actorID string,
	action string,
	details map[string]any,
) error {
	encodedDetails, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO verification_audit_logs (
			client_id,
			document_id,
			review_id,
			actor_id,
			action,
			details
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::jsonb)`,
		clientID,
		nullableUUID(documentID),
		nullableUUID(reviewID),
		actorID,
		action,
		encodedDetails,
	); err != nil {
		return fmt.Errorf("insert verification audit log: %w", err)
	}

	return nil
}

func nullableUUID(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

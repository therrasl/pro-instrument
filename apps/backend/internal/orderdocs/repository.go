package orderdocs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRentalNotFound = errors.New("legal rental not found")

type Repository interface {
	GetRental(context.Context, string) (RentalData, error)
	ListRentalIDs(context.Context, int) ([]string, error)
	ListDocumentTypes(context.Context, string) (map[string]bool, error)
	SaveDocument(context.Context, RentalData, GeneratedDocument) error
}

func (repository *PostgresRepository) ListDocumentTypes(ctx context.Context, rentalID string) (map[string]bool, error) {
	rows, err := repository.database.Query(ctx, `SELECT document_type FROM order_documents WHERE rental_request_id=$1::uuid`, rentalID)
	if err != nil {
		return nil, fmt.Errorf("list existing order documents: %w", err)
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		result[kind] = true
	}
	return result, rows.Err()
}

type PostgresRepository struct{ database *pgxpool.Pool }

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (repository *PostgresRepository) GetRental(ctx context.Context, rentalID string) (RentalData, error) {
	var data RentalData
	err := repository.database.QueryRow(ctx, `SELECT
		rr.id::text,
		rr.order_number,
		rr.status,
		t.name,
		t.daily_price,
		rr.rental_days,
		rr.start_date::text,
		rr.end_date::text,
		rr.rental_price,
		rr.deposit_amount,
		rr.delivery_cost,
		rr.total_amount,
		rr.delivery_method,
		COALESCE(rr.delivery_address, ''),
		COALESCE(s.client_type, c.client_type, 'individual'),
		COALESCE(c.full_name, ''),
		COALESCE(c.phone, ''),
		COALESCE(s.company_name, c.company_name, ''),
		COALESCE(s.inn, c.inn, ''),
		COALESCE(s.kpp, c.kpp, ''),
		COALESCE(s.ogrn, c.ogrn, ''),
		COALESCE(s.legal_address, c.legal_address, ''),
		COALESCE(s.actual_address, ''),
		COALESCE(s.settlement_account, ''),
		COALESCE(s.bik, ''),
		COALESCE(s.correspondent_account, ''),
		COALESCE(s.bank_name, ''),
		COALESCE(s.email, c.email, ''),
		COALESCE(s.organization_phone, c.phone, ''),
		COALESCE(s.company_contact, c.company_contact, ''),
		COALESCE(s.contact_position, ''),
		EXISTS(
			SELECT 1 FROM payments p WHERE p.rental_request_id = rr.id AND p.status = 'succeeded'
		),
		rr.created_at
	FROM rental_requests rr
	JOIN clients c ON c.id = rr.client_id
	LEFT JOIN rental_customer_snapshots s ON s.rental_request_id = rr.id
	JOIN tools t ON t.id = rr.tool_id
	WHERE rr.id = $1::uuid`, rentalID).Scan(
		&data.RentalID, &data.OrderNumber, &data.Status, &data.ToolName,
		&data.DailyPrice, &data.RentalDays,
		&data.StartDate, &data.EndDate,
		&data.RentalPrice, &data.DepositAmount, &data.DeliveryCost, &data.TotalAmount,
		&data.DeliveryMethod, &data.DeliveryAddress,
		&data.ClientType, &data.ClientFullName, &data.ClientPhone,
		&data.CompanyName, &data.INN, &data.KPP,
		&data.OGRN, &data.LegalAddress, &data.ActualAddress, &data.SettlementAccount,
		&data.BIK, &data.CorrespondentAccount, &data.BankName, &data.Email, &data.Phone,
		&data.ContactFullName, &data.ContactPosition, &data.HasSuccessfulPayment, &data.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RentalData{}, ErrRentalNotFound
	}
	if err != nil {
		return RentalData{}, fmt.Errorf("get document rental: %w", err)
	}
	return data, nil
}

func (repository *PostgresRepository) ListRentalIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := repository.database.Query(ctx, `SELECT rr.id::text FROM rental_requests rr
		WHERE rr.status NOT IN ('rejected','cancelled','payment_expired')
		ORDER BY rr.updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list document rentals: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) SaveDocument(ctx context.Context, rental RentalData, document GeneratedDocument) error {
	_, err := repository.database.Exec(ctx, `INSERT INTO order_documents (
		rental_request_id, order_number, document_type, title, storage_key, mime_type, created_at
	) VALUES ($1::uuid,$2,$3,$4,$5,'application/pdf',NOW())
	ON CONFLICT (rental_request_id, document_type) DO UPDATE SET
		title=EXCLUDED.title, storage_key=EXCLUDED.storage_key, mime_type=EXCLUDED.mime_type`,
		rental.RentalID, rental.OrderNumber, document.Type, document.Title, document.StorageKey)
	if err != nil {
		return fmt.Errorf("save order document: %w", err)
	}
	return nil
}

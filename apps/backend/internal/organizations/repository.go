package organizations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ database *pgxpool.Pool }

func NewPostgresRepository(database *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: database}
}

const organizationColumns = `client_id::text, company_name, inn, kpp, ogrn, legal_address,
actual_address, settlement_account, bik, correspondent_account, bank_name,
email, phone, contact_full_name, contact_position, created_at, updated_at`

func scanOrganization(row pgx.Row) (Organization, error) {
	var result Organization
	err := row.Scan(&result.ClientID, &result.CompanyName, &result.INN, &result.KPP, &result.OGRN,
		&result.LegalAddress, &result.ActualAddress, &result.SettlementAccount, &result.BIK,
		&result.CorrespondentAccount, &result.BankName, &result.Email, &result.Phone,
		&result.ContactFullName, &result.ContactPosition, &result.CreatedAt, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("scan organization: %w", err)
	}
	result.CreatedAt = result.CreatedAt.UTC()
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}

func (repository *PostgresRepository) GetByClient(ctx context.Context, clientID string) (Organization, error) {
	return scanOrganization(repository.database.QueryRow(ctx, `SELECT `+organizationColumns+` FROM client_organizations WHERE client_id = $1::uuid`, clientID))
}

func (repository *PostgresRepository) UpsertByClient(ctx context.Context, clientID string, input Input) (Organization, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Organization{}, fmt.Errorf("begin organization update: %w", err)
	}
	defer transaction.Rollback(ctx)
	row := transaction.QueryRow(ctx, `INSERT INTO client_organizations (
		client_id, company_name, inn, kpp, ogrn, legal_address, actual_address,
		settlement_account, bik, correspondent_account, bank_name, email, phone,
		contact_full_name, contact_position
	) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	ON CONFLICT (client_id) DO UPDATE SET
		company_name=EXCLUDED.company_name, inn=EXCLUDED.inn, kpp=EXCLUDED.kpp,
		ogrn=EXCLUDED.ogrn, legal_address=EXCLUDED.legal_address,
		actual_address=EXCLUDED.actual_address, settlement_account=EXCLUDED.settlement_account,
		bik=EXCLUDED.bik, correspondent_account=EXCLUDED.correspondent_account,
		bank_name=EXCLUDED.bank_name, email=EXCLUDED.email, phone=EXCLUDED.phone,
		contact_full_name=EXCLUDED.contact_full_name, contact_position=EXCLUDED.contact_position,
		updated_at=NOW()
	RETURNING `+organizationColumns,
		clientID, input.CompanyName, input.INN, input.KPP, input.OGRN, input.LegalAddress,
		input.ActualAddress, input.SettlementAccount, input.BIK, input.CorrespondentAccount,
		input.BankName, input.Email, input.Phone, input.ContactFullName, input.ContactPosition)
	organization, err := scanOrganization(row)
	if err != nil {
		return Organization{}, err
	}
	command, err := transaction.Exec(ctx, `UPDATE clients SET client_type='legal_entity', company_name=$2, inn=$3, kpp=$4, ogrn=$5, legal_address=$6, company_contact=$7, updated_at=NOW() WHERE id=$1::uuid`, clientID, input.CompanyName, input.INN, input.KPP, input.OGRN, input.LegalAddress, input.ContactFullName)
	if err != nil {
		return Organization{}, fmt.Errorf("sync organization profile: %w", err)
	}
	if command.RowsAffected() != 1 {
		return Organization{}, ErrNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit organization update: %w", err)
	}
	return organization, nil
}

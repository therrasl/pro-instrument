package rentals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	integrationCategoryID = "20000000-0000-4000-8000-000000000001"
	integrationToolID     = "20000000-0000-4000-8000-000000000002"
	integrationUnitID     = "20000000-0000-4000-8000-000000000003"
	integrationClientOne  = "20000000-0000-4000-8000-000000000004"
	integrationClientTwo  = "20000000-0000-4000-8000-000000000005"
)

func TestRentalMigrationHasDatabaseOverlapConstraints(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000009_rentals.up.sql"))
	if err != nil {
		t.Fatalf("read rental migration: %v", err)
	}
	migration := string(data)
	if strings.Count(migration, "EXCLUDE USING gist") < 2 {
		t.Fatal("rental request and tool hold must both have PostgreSQL exclusion constraints")
	}
	for _, fragment := range []string{
		"tool_unit_id WITH =",
		"rental_period WITH &&",
		"'active', 'confirmed'",
		"'pending_manager'",
		"CREATE TABLE integration_outbox",
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("rental migration does not contain %q", fragment)
		}
	}
}

func TestPostgresRentalHoldLifecycleAndIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	pool := openRentalTestDatabase(t, databaseURL)
	repository := NewPostgresRepository(pool)
	seedRentalTestData(t, pool)

	now := time.Now().UTC().Truncate(time.Microsecond)
	startDate := dateOnly(now.AddDate(0, 0, 2))
	endDate := dateOnly(now.AddDate(0, 0, 4))

	type result struct {
		rental RentalRequest
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for _, clientID := range []string{integrationClientOne, integrationClientTwo} {
		waitGroup.Add(1)
		go func(clientID string) {
			defer waitGroup.Done()
			<-start
			rental, err := repository.Create(context.Background(), integrationCommand(
				clientID,
				startDate,
				endDate,
				now,
				now.Add(30*time.Minute),
			))
			results <- result{rental: rental, err: err}
		}(clientID)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var created RentalRequest
	var unavailable int
	for operation := range results {
		switch {
		case operation.err == nil:
			if created.ID != "" {
				t.Fatalf("parallel requests both succeeded: %#v and %#v", created, operation.rental)
			}
			created = operation.rental
		case errors.Is(operation.err, ErrNoAvailableUnit):
			unavailable++
		default:
			t.Fatalf("unexpected parallel create error: %v", operation.err)
		}
	}
	if created.ID == "" || unavailable != 1 {
		t.Fatalf("expected one created rental and one conflict, created=%#v conflicts=%d", created, unavailable)
	}
	if created.ToolUnitID != integrationUnitID ||
		created.RentalPrice != 150_000 ||
		created.DepositAmount != 200_000 ||
		created.TotalAmount != 350_000 {
		t.Fatalf("unexpected persisted rental: %#v", created)
	}

	assertSingleRentalSideEffects(t, pool, created.ID)

	otherClientID := integrationClientOne
	if created.ClientID == integrationClientOne {
		otherClientID = integrationClientTwo
	}
	if _, err := repository.GetByClient(
		context.Background(),
		created.ClientID,
		created.ID,
	); err != nil {
		t.Fatalf("owner cannot read rental: %v", err)
	}
	if _, err := repository.GetByClient(
		context.Background(),
		otherClientID,
		created.ID,
	); !errors.Is(err, ErrRentalNotFound) {
		t.Fatalf("foreign client must receive not found, got %v", err)
	}

	if _, err := repository.Create(
		context.Background(),
		integrationCommand(otherClientID, startDate, endDate, now, now.Add(30*time.Minute)),
	); !errors.Is(err, ErrNoAvailableUnit) {
		t.Fatalf("repeated request bypassed active period constraint: %v", err)
	}

	expiredAt := now.Add(-time.Minute)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE tool_holds SET expires_at = $2 WHERE rental_request_id = $1::uuid`,
		created.ID,
		expiredAt,
	); err != nil {
		t.Fatalf("age tool hold: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests SET expires_at = $2 WHERE id = $1::uuid`,
		created.ID,
		expiredAt,
	); err != nil {
		t.Fatalf("age rental request: %v", err)
	}

	replacement, err := repository.Create(
		context.Background(),
		integrationCommand(otherClientID, startDate, endDate, now, now.Add(30*time.Minute)),
	)
	if err != nil {
		t.Fatalf("expired hold did not release availability: %v", err)
	}
	if replacement.ToolUnitID != created.ToolUnitID {
		t.Fatalf("expected released unit %q, got %q", created.ToolUnitID, replacement.ToolUnitID)
	}

	var requestStatus string
	var holdStatus string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT rr.status, h.status
		 FROM rental_requests AS rr
		 JOIN tool_holds AS h ON h.rental_request_id = rr.id
		 WHERE rr.id = $1::uuid`,
		created.ID,
	).Scan(&requestStatus, &holdStatus); err != nil {
		t.Fatalf("query expired rental state: %v", err)
	}
	if requestStatus != "payment_expired" || holdStatus != "expired" {
		t.Fatalf("unexpected expired state: request=%q hold=%q", requestStatus, holdStatus)
	}

	cancelled, err := repository.CancelByClient(
		context.Background(),
		replacement.ClientID,
		replacement.ID,
		now,
	)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel replacement rental: rental=%#v err=%v", cancelled, err)
	}
	if _, err := repository.CancelByClient(
		context.Background(),
		replacement.ClientID,
		replacement.ID,
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("repeat cancellation must be idempotent: %v", err)
	}
	var cancellationHistory int
	var cancellationOutbox int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT status FROM tool_holds WHERE rental_request_id = $1::uuid),
			(
				SELECT COUNT(*)
				FROM rental_status_history
				WHERE rental_request_id = $1::uuid AND to_status = 'cancelled'
			),
			(
				SELECT COUNT(*)
				FROM integration_outbox
				WHERE dedupe_key = $2
			)`,
		replacement.ID,
		"bitrix.deal.update:"+replacement.ID+":cancelled",
	).Scan(&holdStatus, &cancellationHistory, &cancellationOutbox); err != nil {
		t.Fatalf("query cancellation side effects: %v", err)
	}
	if holdStatus != "cancelled" || cancellationHistory != 1 || cancellationOutbox != 1 {
		t.Fatalf(
			"unexpected cancellation side effects: hold=%q history=%d outbox=%d",
			holdStatus,
			cancellationHistory,
			cancellationOutbox,
		)
	}

	paid, err := repository.Create(
		context.Background(),
		integrationCommand(
			otherClientID,
			startDate,
			endDate,
			now,
			now.Add(30*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("create paid cancellation test rental: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests SET status = 'paid' WHERE id = $1::uuid`,
		paid.ID,
	); err != nil {
		t.Fatalf("mark cancellation test rental paid: %v", err)
	}
	if _, err := repository.CancelByClient(
		context.Background(),
		paid.ClientID,
		paid.ID,
		now,
	); !errors.Is(err, ErrRentalNotCancellable) {
		t.Fatalf("paid rental cancellation error=%v", err)
	}
}

func TestLegalRentalKeepsOrganizationSnapshot(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool := openRentalTestDatabase(t, databaseURL)
	repository := NewPostgresRepository(pool)
	seedRentalTestData(t, pool)
	_, err := pool.Exec(context.Background(), `UPDATE clients SET client_type='legal_entity', company_name='ООО Старое', inn='7705432109', kpp='770501001', ogrn='1157746123456', legal_address='Старый адрес', company_contact='Анна Соколова' WHERE id=$1::uuid`, integrationClientOne)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO client_organizations (client_id,company_name,inn,kpp,ogrn,legal_address,email,phone,contact_full_name) VALUES ($1::uuid,'ООО Старое','7705432109','770501001','1157746123456','Старый адрес','legal@example.test','+79990000001','Анна Соколова')`, integrationClientOne)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rental, err := repository.Create(context.Background(), integrationCommand(integrationClientOne, dateOnly(now.AddDate(0, 0, 10)), dateOnly(now.AddDate(0, 0, 12)), now, now.Add(30*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `UPDATE client_organizations SET company_name='ООО Новое',inn='7812345678',updated_at=NOW() WHERE client_id=$1::uuid`, integrationClientOne)
	if err != nil {
		t.Fatal(err)
	}
	var company, inn string
	if err := pool.QueryRow(context.Background(), `SELECT company_name,inn FROM rental_customer_snapshots WHERE rental_request_id=$1::uuid`, rental.ID).Scan(&company, &inn); err != nil {
		t.Fatal(err)
	}
	if company != "ООО Старое" || inn != "7705432109" {
		t.Fatalf("snapshot changed: %s %s", company, inn)
	}
	var documentID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO order_documents (rental_request_id,order_number,document_type,title,storage_key,mime_type) VALUES ($1::uuid,$2,'rental_contract','Договор','order-documents/test.pdf','application/pdf') RETURNING id::text`, rental.ID, rental.OrderNumber).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	foreign, err := repository.ListDocumentsByClient(context.Background(), integrationClientTwo, rental.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != 0 {
		t.Fatalf("foreign client sees documents: %#v", foreign)
	}
	if _, err := repository.GetDocumentByClient(context.Background(), integrationClientTwo, rental.ID, documentID); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("foreign document access: %v", err)
	}
}

func openRentalTestDatabase(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}

	randomValue := make([]byte, 8)
	if _, err := rand.Read(randomValue); err != nil {
		adminPool.Close()
		t.Fatalf("generate test schema: %v", err)
	}
	schema := "rentals_test_" + hex.EncodeToString(randomValue)
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatalf("create test schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("parse integration database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("open schema pool: %v", err)
	}

	for _, migrationName := range []string{
		"000002_catalog.up.sql",
		"000004_client_auth.up.sql",
		"000009_rentals.up.sql",
		"000010_bitrix_integration.up.sql",
		"000011_yookassa_payments.up.sql",
		"000013_order_numbers_b2b_documents.up.sql",
		"000014_rental_documents_organizations.up.sql",
		"000015_order_documents_storage_constraint.up.sql",
		"000016_backfill_missing_rental_snapshots.up.sql",
		"000017_rental_extensions_and_inspection_photos.up.sql",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", migrationName))
		if err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
			adminPool.Close()
			t.Fatalf("read %s: %v", migrationName, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
			adminPool.Close()
			t.Fatalf("apply %s: %v", migrationName, err)
		}
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
	})
	return pool
}

func seedRentalTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	batch := &pgx.Batch{}
	batch.Queue(
		`INSERT INTO categories (id, name, slug)
		 VALUES ($1::uuid, 'Test', 'test')`,
		integrationCategoryID,
	)
	batch.Queue(
		`INSERT INTO tools (
			id,
			category_id,
			name,
			slug,
			daily_price,
			deposit_amount
		 )
		 VALUES ($1::uuid, $2::uuid, 'Test tool', 'test-tool', 50000, 200000)`,
		integrationToolID,
		integrationCategoryID,
	)
	batch.Queue(
		`INSERT INTO tool_units (id, tool_id, inventory_number, status)
		 VALUES ($1::uuid, $2::uuid, 'TEST-UNIT-1', 'available')`,
		integrationUnitID,
		integrationToolID,
	)
	batch.Queue(
		`INSERT INTO clients (
			id,
			phone,
			phone_verified_at,
			full_name,
			birth_date,
			status
		 )
		 VALUES
			($1::uuid, '+79990000001', NOW(), 'Client One', '1990-01-01', 'verified'),
			($2::uuid, '+79990000002', NOW(), 'Client Two', '1991-01-01', 'verified')`,
		integrationClientOne,
		integrationClientTwo,
	)
	results := pool.SendBatch(context.Background(), batch)
	defer func() {
		_ = results.Close()
	}()
	for index := 0; index < batch.Len(); index++ {
		if _, err := results.Exec(); err != nil {
			t.Fatalf("seed rental test data statement %d: %v", index+1, err)
		}
	}
	if err := results.Close(); err != nil {
		t.Fatalf("close rental seed batch: %v", err)
	}
}

func integrationCommand(
	clientID string,
	startDate time.Time,
	endDate time.Time,
	now time.Time,
	expiresAt time.Time,
) CreateCommand {
	return CreateCommand{
		ClientID:       clientID,
		ToolID:         integrationToolID,
		StartDate:      startDate,
		EndDate:        endDate,
		RentalDays:     3,
		DeliveryMethod: DeliverySelfPickup,
		CourierFee:     100_000,
		ExpiresAt:      expiresAt,
		Now:            now,
	}
}

func assertSingleRentalSideEffects(
	t *testing.T,
	pool *pgxpool.Pool,
	rentalID string,
) {
	t.Helper()
	var holds int
	var history int
	var outbox int
	var eventType string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT COUNT(*) FROM tool_holds WHERE rental_request_id = $1::uuid),
			(SELECT COUNT(*) FROM rental_status_history WHERE rental_request_id = $1::uuid),
			(
				SELECT COUNT(*)
				FROM integration_outbox
				WHERE aggregate_id IN (
					$1::uuid,
					(SELECT client_id FROM rental_requests WHERE id = $1::uuid)
				)
			),
			(SELECT event_type FROM integration_outbox WHERE aggregate_id = $1::uuid)`,
		rentalID,
	).Scan(&holds, &history, &outbox, &eventType); err != nil {
		t.Fatalf("query rental side effects: %v", err)
	}
	if holds != 1 || history != 1 || outbox != 2 || eventType != "bitrix.deal.create" {
		t.Fatalf(
			"unexpected rental side effects: holds=%d history=%d outbox=%d event=%q",
			holds,
			history,
			outbox,
			eventType,
		)
	}
}

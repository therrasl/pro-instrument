package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

const (
	paymentCategoryID = "40000000-0000-4000-8000-000000000001"
	paymentToolID     = "40000000-0000-4000-8000-000000000002"
	paymentUnitID     = "40000000-0000-4000-8000-000000000003"
	paymentClientID   = "40000000-0000-4000-8000-000000000004"
)

func TestPaymentMigrationContainsIdempotencyAndAudit(t *testing.T) {
	data, err := os.ReadFile(
		filepath.Join("..", "..", "migrations", "000011_yookassa_payments.up.sql"),
	)
	if err != nil {
		t.Fatalf("read payment migration: %v", err)
	}
	migration := string(data)
	for _, fragment := range []string{
		"CREATE TABLE payments",
		"idempotency_key TEXT NOT NULL UNIQUE",
		"payments_one_active_per_rental_idx",
		"CREATE TABLE payment_events",
		"event_key TEXT NOT NULL UNIQUE",
		"CREATE TABLE fiscal_receipts",
		"CREATE TABLE deposits",
		"CREATE TABLE payment_audit_logs",
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("payment migration does not contain %q", fragment)
		}
	}
}

func TestSuccessfulPaymentTransactionCreatesBitrixOutbox(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool := openPaymentTestDatabase(t, databaseURL)
	seedPaymentTestData(t, pool)

	now := time.Now().UTC().Truncate(time.Microsecond)
	rentalRepository := rentals.NewPostgresRepository(pool)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, 2)
	rental, err := rentalRepository.Create(context.Background(), rentals.CreateCommand{
		ClientID:       paymentClientID,
		ToolID:         paymentToolID,
		StartDate:      start,
		EndDate:        start.AddDate(0, 0, 1),
		RentalDays:     2,
		DeliveryMethod: rentals.DeliveryCourier,
		DeliveryAddress: func() *string {
			value := "Москва, Тестовая улица, 1"
			return &value
		}(),
		CourierFee: 50_000,
		ExpiresAt:  now.Add(30 * time.Minute),
		Now:        now,
	})
	if err != nil {
		t.Fatalf("create payment test rental: %v", err)
	}
	confirmBatch := &pgx.Batch{}
	confirmBatch.Queue(
		`UPDATE rental_requests
		 SET status = 'awaiting_payment', expires_at = $2, updated_at = $3
		 WHERE id = $1::uuid`,
		rental.ID,
		now.Add(30*time.Minute),
		now,
	)
	confirmBatch.Queue(
		`UPDATE tool_holds
		 SET status = 'confirmed', expires_at = $2, updated_at = $3
		 WHERE rental_request_id = $1::uuid`,
		rental.ID,
		now.Add(30*time.Minute),
		now,
	)
	confirmBatch.Queue(
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
			'pending_manager',
			'awaiting_payment',
			'manager',
			'test-manager',
			$2
		 )`,
		rental.ID,
		now,
	)
	confirmResults := pool.SendBatch(context.Background(), confirmBatch)
	for index := 0; index < confirmBatch.Len(); index++ {
		if _, err := confirmResults.Exec(); err != nil {
			_ = confirmResults.Close()
			t.Fatalf("confirm payment test rental statement %d: %v", index+1, err)
		}
	}
	if err := confirmResults.Close(); err != nil {
		t.Fatalf("close payment confirmation batch: %v", err)
	}

	provider := &providerStub{
		createResult: providerPayment("pending", false, "3500.00"),
		getResult:    providerPayment("succeeded", true, "3500.00"),
	}
	provider.createResult.Metadata["rental_id"] = rental.ID
	provider.getResult.Metadata["rental_id"] = rental.ID
	repository := NewPostgresRepository(pool)
	service := NewService(
		repository,
		provider,
		true,
		"https://app.example.test/return",
		false,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	)

	first, err := service.Create(context.Background(), paymentClientID, rental.ID)
	if err != nil {
		t.Fatalf("create persisted payment: %v", err)
	}
	second, err := service.Create(context.Background(), paymentClientID, rental.ID)
	if err != nil {
		t.Fatalf("repeat persisted payment: %v", err)
	}
	if first.Payment.ID != second.Payment.ID || provider.createCalls != 1 {
		t.Fatalf(
			"payment creation was not idempotent: first=%q second=%q calls=%d",
			first.Payment.ID,
			second.Payment.ID,
			provider.createCalls,
		)
	}

	raw := json.RawMessage(`{
		"type":"notification",
		"event":"payment.succeeded",
		"object":{"id":"provider-1"}
	}`)
	eventID, _, err := service.StoreWebhook(context.Background(), raw)
	if err != nil {
		t.Fatalf("store successful webhook: %v", err)
	}
	if err := service.ProcessEvent(context.Background(), eventID); err != nil {
		t.Fatalf("process successful webhook: %v", err)
	}

	var paymentStatus string
	var rentalStatus string
	var depositStatus string
	var historyCount int
	var auditCount int
	var paidOutboxCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT status FROM payments WHERE id = $1::uuid),
			(SELECT status FROM rental_requests WHERE id = $2::uuid),
			(SELECT status FROM deposits WHERE payment_id = $1::uuid),
			(
				SELECT COUNT(*)
				FROM rental_status_history
				WHERE rental_request_id = $2::uuid AND to_status = 'paid'
			),
			(
				SELECT COUNT(*)
				FROM payment_audit_logs
				WHERE payment_id = $1::uuid AND action = 'payment.succeeded'
			),
			(
				SELECT COUNT(*)
				FROM integration_outbox
				WHERE dedupe_key = $3
			)`,
		first.Payment.ID,
		rental.ID,
		"bitrix.deal.update:"+rental.ID+":paid",
	).Scan(
		&paymentStatus,
		&rentalStatus,
		&depositStatus,
		&historyCount,
		&auditCount,
		&paidOutboxCount,
	); err != nil {
		t.Fatalf("query successful payment side effects: %v", err)
	}
	if paymentStatus != StatusSucceeded ||
		rentalStatus != rentals.StatusPaid ||
		depositStatus != "paid" ||
		historyCount != 1 ||
		auditCount != 1 ||
		paidOutboxCount != 1 {
		t.Fatalf(
			"unexpected successful payment side effects: payment=%q rental=%q deposit=%q history=%d audit=%d outbox=%d",
			paymentStatus,
			rentalStatus,
			depositStatus,
			historyCount,
			auditCount,
			paidOutboxCount,
		)
	}

	duplicateID, created, err := service.StoreWebhook(context.Background(), raw)
	if err != nil || created || duplicateID != eventID {
		t.Fatalf("duplicate successful webhook: id=%q created=%t err=%v", duplicateID, created, err)
	}
	if err := service.ProcessEvent(context.Background(), duplicateID); err != nil {
		t.Fatalf("repeat successful webhook: %v", err)
	}

	expiredStart := start.AddDate(0, 0, 10)
	expiredRental, err := rentalRepository.Create(
		context.Background(),
		rentals.CreateCommand{
			ClientID:       paymentClientID,
			ToolID:         paymentToolID,
			StartDate:      expiredStart,
			EndDate:        expiredStart,
			RentalDays:     1,
			DeliveryMethod: rentals.DeliverySelfPickup,
			CourierFee:     50_000,
			ExpiresAt:      now.Add(30 * time.Minute),
			Now:            now,
		},
	)
	if err != nil {
		t.Fatalf("create expired payment test rental: %v", err)
	}
	expiredAt := now.Add(-time.Second)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests
		 SET status = 'awaiting_payment', expires_at = $2, updated_at = $3
		 WHERE id = $1::uuid`,
		expiredRental.ID,
		expiredAt,
		now,
	); err != nil {
		t.Fatalf("expire rental payment deadline: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE tool_holds
		 SET status = 'confirmed', expires_at = $2, updated_at = $3
		 WHERE rental_request_id = $1::uuid`,
		expiredRental.ID,
		now.Add(30*time.Minute),
		now,
	); err != nil {
		t.Fatalf("confirm expired payment test hold: %v", err)
	}
	if _, _, _, err := repository.PreparePayment(
		context.Background(),
		paymentClientID,
		expiredRental.ID,
		false,
		now,
	); !errors.Is(err, ErrPaymentExpired) {
		t.Fatalf("expired direct payment request error=%v", err)
	}

	lateStart := start.AddDate(0, 0, 20)
	lateRental, err := rentalRepository.Create(
		context.Background(),
		rentals.CreateCommand{
			ClientID:       paymentClientID,
			ToolID:         paymentToolID,
			StartDate:      lateStart,
			EndDate:        lateStart,
			RentalDays:     1,
			DeliveryMethod: rentals.DeliverySelfPickup,
			CourierFee:     50_000,
			ExpiresAt:      now.Add(30 * time.Minute),
			Now:            now,
		},
	)
	if err != nil {
		t.Fatalf("create late webhook test rental: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests
		 SET status = 'awaiting_payment', expires_at = $2, updated_at = $3
		 WHERE id = $1::uuid`,
		lateRental.ID,
		now.Add(30*time.Minute),
		now,
	); err != nil {
		t.Fatalf("open late webhook payment deadline: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE tool_holds
		 SET status = 'confirmed', expires_at = $2, updated_at = $3
		 WHERE rental_request_id = $1::uuid`,
		lateRental.ID,
		now.Add(30*time.Minute),
		now,
	); err != nil {
		t.Fatalf("confirm late webhook hold: %v", err)
	}

	latePending := providerPayment("pending", false, "2500.00")
	latePending.ID = "provider-late"
	latePending.Metadata["rental_id"] = lateRental.ID
	lateSucceeded := providerPayment("succeeded", true, "2500.00")
	lateSucceeded.ID = "provider-late"
	lateSucceeded.Metadata["rental_id"] = lateRental.ID
	lateProvider := &providerStub{
		createResult: latePending,
		getResult:    lateSucceeded,
	}
	clockNow := now
	lateService := NewService(
		repository,
		lateProvider,
		true,
		"https://app.example.test/return",
		false,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	)
	lateService.now = func() time.Time { return clockNow }
	latePayment, err := lateService.Create(
		context.Background(),
		paymentClientID,
		lateRental.ID,
	)
	if err != nil {
		t.Fatalf("create late webhook payment: %v", err)
	}

	clockNow = now.Add(31 * time.Minute)
	if _, err := rentalRepository.ExpireHolds(context.Background(), clockNow); err != nil {
		t.Fatalf("expire late webhook rental: %v", err)
	}
	lateEventID, _, err := lateService.StoreWebhook(
		context.Background(),
		json.RawMessage(`{
			"type":"notification",
			"event":"payment.succeeded",
			"object":{"id":"provider-late"}
		}`),
	)
	if err != nil {
		t.Fatalf("store late successful webhook: %v", err)
	}
	if err := lateService.ProcessEvent(context.Background(), lateEventID); err != nil {
		t.Fatalf("process late successful webhook: %v", err)
	}

	var latePaymentStatus string
	var lateRentalStatus string
	var lateAuditCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT status FROM payments WHERE id = $1::uuid),
			(SELECT status FROM rental_requests WHERE id = $2::uuid),
			(
				SELECT COUNT(*)
				FROM payment_audit_logs
				WHERE payment_id = $1::uuid
				  AND action = 'payment.succeeded_after_deadline'
			)`,
		latePayment.Payment.ID,
		lateRental.ID,
	).Scan(&latePaymentStatus, &lateRentalStatus, &lateAuditCount); err != nil {
		t.Fatalf("query late successful webhook state: %v", err)
	}
	if latePaymentStatus != StatusRequiresReview ||
		lateRentalStatus != rentals.StatusPaymentExpired ||
		lateAuditCount != 1 {
		t.Fatalf(
			"late webhook changed rental incorrectly: payment=%q rental=%q audit=%d",
			latePaymentStatus,
			lateRentalStatus,
			lateAuditCount,
		)
	}
}

func openPaymentTestDatabase(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open payment integration database: %v", err)
	}

	randomValue := make([]byte, 8)
	if _, err := rand.Read(randomValue); err != nil {
		adminPool.Close()
		t.Fatalf("generate payment test schema: %v", err)
	}
	schema := "payments_test_" + hex.EncodeToString(randomValue)
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatalf("create payment test schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("parse payment integration database: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("open payment test schema: %v", err)
	}
	for _, migrationName := range []string{
		"000002_catalog.up.sql",
		"000004_client_auth.up.sql",
		"000009_rentals.up.sql",
		"000010_bitrix_integration.up.sql",
		"000011_yookassa_payments.up.sql",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", migrationName))
		if err != nil {
			t.Fatalf("read %s: %v", migrationName, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
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

func seedPaymentTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	batch := &pgx.Batch{}
	batch.Queue(
		`INSERT INTO categories (id, name, slug)
		 VALUES ($1::uuid, 'Test', 'payment-test')`,
		paymentCategoryID,
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
		 VALUES (
			$1::uuid,
			$2::uuid,
			'Тестовый перфоратор',
			'payment-test-tool',
			50000,
			200000
		 )`,
		paymentToolID,
		paymentCategoryID,
	)
	batch.Queue(
		`INSERT INTO tool_units (id, tool_id, inventory_number, status)
		 VALUES ($1::uuid, $2::uuid, 'PAYMENT-UNIT-1', 'available')`,
		paymentUnitID,
		paymentToolID,
	)
	batch.Queue(
		`INSERT INTO clients (
			id,
			phone,
			phone_verified_at,
			full_name,
			birth_date,
			email,
			status
		 )
		 VALUES (
			$1::uuid,
			'+79990000004',
			NOW(),
			'Тестовый Клиент',
			'1990-01-01',
			'client@example.test',
			'verified'
		 )`,
		paymentClientID,
	)
	batch.Queue(
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
		 VALUES (
			$1::uuid,
			'payment-test-offer',
			'payment-test-privacy',
			TRUE,
			TRUE,
			TRUE,
			TRUE,
			NOW(),
			'127.0.0.1',
			'test'
		 )`,
		paymentClientID,
	)
	results := pool.SendBatch(context.Background(), batch)
	for index := 0; index < batch.Len(); index++ {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			t.Fatalf("seed payment test statement %d: %v", index+1, err)
		}
	}
	if err := results.Close(); err != nil {
		t.Fatalf("close payment seed batch: %v", err)
	}
}

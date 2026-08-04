package bitrix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

const (
	bitrixTestCategoryID = "30000000-0000-4000-8000-000000000001"
	bitrixTestToolID     = "30000000-0000-4000-8000-000000000002"
	bitrixTestClientID   = "30000000-0000-4000-8000-000000000003"
)

func TestBitrixInboundTransitionsUseActualDealAndRentalRules(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool := openBitrixTestDatabase(t, databaseURL)
	seedBitrixTestData(t, pool)

	stagesByDeal := map[string]string{
		"101": "C12:UC_RRNJ1L",
		"102": "C12:LOSE",
		"103": "C12:UC_RRNJ1L",
		"104": "C12:UC_RRNJ1L",
		"105": "C12:UC_RRNJ1L",
		"106": "C12:UC_KQXWS6",
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/crm.deal.get.json" {
			http.NotFound(response, request)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode get deal request: %v", err)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"result": map[string]any{
				"ID":          body.ID,
				"CATEGORY_ID": "12",
				"STAGE_ID":    stagesByDeal[body.ID],
			},
		})
	}))
	defer server.Close()

	rentalRepository := rentals.NewPostgresRepository(pool)
	bitrixRepository := NewPostgresRepository(pool)
	worker := NewWorker(
		bitrixRepository,
		NewHTTPClient(server.URL, time.Second),
		rentals.NewStatusService(rentalRepository, 30*time.Minute),
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)

	confirmed := createBitrixTestRental(t, rentalRepository, pool, "101")
	storeBitrixTestEvent(t, bitrixRepository, "evt-confirm", "101")
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("process manager confirmation: processed=%t err=%v", processed, err)
	}
	assertRentalAndHoldStatus(
		t,
		pool,
		confirmed.ID,
		rentals.StatusAwaitingPayment,
		"confirmed",
	)
	confirmedView, err := rentalRepository.GetByClient(
		context.Background(),
		bitrixTestClientID,
		confirmed.ID,
	)
	if err != nil || !confirmedView.PaymentAvailable {
		t.Fatalf("awaiting payment did not open payment: rental=%#v err=%v", confirmedView, err)
	}
	assertNoInboundDealUpdate(t, pool, confirmed.ID)

	_, created, err := bitrixRepository.StoreInboundEvent(
		context.Background(),
		"evt-confirm",
		"101",
		json.RawMessage(`{"deal_id":"101","stage":"ignored"}`),
		time.Now().UTC(),
	)
	if err != nil || created {
		t.Fatalf("duplicate event was not idempotent: created=%t err=%v", created, err)
	}
	processed, err = worker.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("duplicate event changed state again: processed=%t err=%v", processed, err)
	}
	assertStatusHistoryCount(t, pool, confirmed.ID, 2)
	storeBitrixTestEvent(t, bitrixRepository, "evt-confirm-repeat", "101")
	processed, err = worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("process repeated stage event: processed=%t err=%v", processed, err)
	}
	assertStatusHistoryCount(t, pool, confirmed.ID, 2)

	fastForwarded := createBitrixTestRental(t, rentalRepository, pool, "106")
	storeBitrixTestEvent(t, bitrixRepository, "evt-fast-forward", "106")
	processed, err = worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("process fast-forward event: processed=%t err=%v", processed, err)
	}
	assertRentalAndHoldStatus(
		t,
		pool,
		fastForwarded.ID,
		rentals.StatusPreparing,
		"confirmed",
	)
	assertStatusHistoryCount(t, pool, fastForwarded.ID, 4)
	assertNoInboundDealUpdate(t, pool, fastForwarded.ID)

	storeBitrixTestEvent(t, bitrixRepository, "evt-fast-forward-repeat", "106")
	processed, err = worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("process repeated fast-forward event: processed=%t err=%v", processed, err)
	}
	assertStatusHistoryCount(t, pool, fastForwarded.ID, 4)

	rejected := createBitrixTestRental(t, rentalRepository, pool, "102")
	storeBitrixTestEvent(t, bitrixRepository, "evt-reject", "102")
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("process manager rejection: %v", err)
	}
	assertRentalAndHoldStatus(t, pool, rejected.ID, rentals.StatusRejected, "released")

	invalid := createBitrixTestRental(t, rentalRepository, pool, "103")
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests
		 SET status = 'preparing', updated_at = NOW()
		 WHERE id = $1::uuid`,
		invalid.ID,
	); err != nil {
		t.Fatalf("prepare invalid rental status: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE tool_holds
		 SET status = 'confirmed', updated_at = NOW()
		 WHERE rental_request_id = $1::uuid`,
		invalid.ID,
	); err != nil {
		t.Fatalf("prepare invalid hold status: %v", err)
	}
	storeBitrixTestEvent(t, bitrixRepository, "evt-invalid", "103")
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("process invalid transition event: %v", err)
	}
	assertRentalAndHoldStatus(t, pool, invalid.ID, rentals.StatusPreparing, "confirmed")
	assertStatusHistoryCount(t, pool, invalid.ID, 1)
	var inboundStatus string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT status FROM bitrix_inbound_events WHERE event_key = 'evt-invalid'`,
	).Scan(&inboundStatus); err != nil {
		t.Fatalf("query invalid inbound status: %v", err)
	}
	if inboundStatus != "rejected" {
		t.Fatalf("invalid transition event status=%q", inboundStatus)
	}

	expired := createBitrixTestRental(t, rentalRepository, pool, "104")
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE tool_holds
		 SET expires_at = NOW() - INTERVAL '1 minute'
		 WHERE rental_request_id = $1::uuid`,
		expired.ID,
	); err != nil {
		t.Fatalf("expire Bitrix test hold: %v", err)
	}
	storeBitrixTestEvent(t, bitrixRepository, "evt-expired-hold", "104")
	processed, err = worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("process expired hold event: processed=%t err=%v", processed, err)
	}
	assertRentalAndHoldStatus(
		t,
		pool,
		expired.ID,
		rentals.StatusPendingManager,
		"active",
	)
	var expiredEventStatus string
	var expiredEventError string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT status, last_error
		 FROM bitrix_inbound_events
		 WHERE event_key = 'evt-expired-hold'`,
	).Scan(&expiredEventStatus, &expiredEventError); err != nil {
		t.Fatalf("query expired hold event: %v", err)
	}
	if expiredEventStatus != "rejected" ||
		expiredEventError != rentals.ErrRentalHoldExpired.Error() {
		t.Fatalf(
			"unexpected expired hold event: status=%q error=%q",
			expiredEventStatus,
			expiredEventError,
		)
	}
	processed, err = worker.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("expired hold event was retried: processed=%t err=%v", processed, err)
	}

	missing := createBitrixTestRental(t, rentalRepository, pool, "105")
	if _, err := pool.Exec(
		context.Background(),
		`DELETE FROM tool_holds WHERE rental_request_id = $1::uuid`,
		missing.ID,
	); err != nil {
		t.Fatalf("delete Bitrix test hold: %v", err)
	}
	_, _, err = rentalRepository.ApplyBitrixStatus(
		context.Background(),
		"105",
		rentals.StatusAwaitingPayment,
		time.Now().UTC(),
		time.Now().UTC().Add(30*time.Minute),
	)
	if !errors.Is(err, rentals.ErrRentalHoldNotFound) {
		t.Fatalf("expected missing hold error, got %v", err)
	}

	reconciled, err := bitrixRepository.Reconcile(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("reconcile Bitrix rentals: %v", err)
	}
	if reconciled != 6 {
		t.Fatalf("expected six deal update events, got %d", reconciled)
	}
	reconciled, err = bitrixRepository.Reconcile(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("repeat Bitrix reconciliation: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciliation duplicated events: %d", reconciled)
	}
}

func TestClaimInboundSerializesEventsForOneDeal(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool := openBitrixTestDatabase(t, databaseURL)
	repository := NewPostgresRepository(pool)
	now := time.Now().UTC()

	for index, eventKey := range []string{"evt-sequential-1", "evt-sequential-2"} {
		_, created, err := repository.StoreInboundEvent(
			context.Background(),
			eventKey,
			"201",
			json.RawMessage(`{"deal_id":"201"}`),
			now.Add(time.Duration(index)*time.Millisecond),
		)
		if err != nil || !created {
			t.Fatalf("store sequential event %d: created=%t err=%v", index+1, created, err)
		}
	}

	first, claimed, err := repository.ClaimInbound(
		context.Background(),
		now.Add(time.Second),
		now.Add(-time.Minute),
		3,
	)
	if err != nil || !claimed || first.EventKey != "evt-sequential-1" {
		t.Fatalf("claim first event: event=%#v claimed=%t err=%v", first, claimed, err)
	}
	_, claimed, err = repository.ClaimInbound(
		context.Background(),
		now.Add(time.Second),
		now.Add(-time.Minute),
		3,
	)
	if err != nil || claimed {
		t.Fatalf("second event bypassed processing predecessor: claimed=%t err=%v", claimed, err)
	}
	if err := repository.RejectInbound(
		context.Background(),
		first.ID,
		3,
		"test completion",
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("resolve first event: %v", err)
	}
	second, claimed, err := repository.ClaimInbound(
		context.Background(),
		now.Add(2*time.Second),
		now.Add(-time.Minute),
		3,
	)
	if err != nil || !claimed || second.EventKey != "evt-sequential-2" {
		t.Fatalf("claim second event: event=%#v claimed=%t err=%v", second, claimed, err)
	}
}

func openBitrixTestDatabase(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open Bitrix integration database: %v", err)
	}

	randomValue := make([]byte, 8)
	if _, err := rand.Read(randomValue); err != nil {
		adminPool.Close()
		t.Fatalf("generate Bitrix test schema: %v", err)
	}
	schema := "bitrix_test_" + hex.EncodeToString(randomValue)
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatalf("create Bitrix test schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("parse Bitrix integration database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("open Bitrix test schema pool: %v", err)
	}
	for _, migrationName := range []string{
		"000002_catalog.up.sql",
		"000004_client_auth.up.sql",
		"000009_rentals.up.sql",
		"000010_bitrix_integration.up.sql",
		"000011_yookassa_payments.up.sql",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migrationName))
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

func seedBitrixTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	batch := &pgx.Batch{}
	batch.Queue(
		`INSERT INTO categories (id, name, slug) VALUES ($1::uuid, 'Test', 'test')`,
		bitrixTestCategoryID,
	)
	batch.Queue(
		`INSERT INTO tools (
			id, category_id, name, slug, daily_price, deposit_amount
		 )
		 VALUES ($1::uuid, $2::uuid, 'Перфоратор', 'test-tool', 50000, 200000)`,
		bitrixTestToolID,
		bitrixTestCategoryID,
	)
	for index, unitID := range []string{
		"30000000-0000-4000-8000-000000000011",
		"30000000-0000-4000-8000-000000000012",
		"30000000-0000-4000-8000-000000000013",
		"30000000-0000-4000-8000-000000000014",
		"30000000-0000-4000-8000-000000000015",
		"30000000-0000-4000-8000-000000000016",
	} {
		batch.Queue(
			`INSERT INTO tool_units (id, tool_id, inventory_number, status)
			 VALUES ($1::uuid, $2::uuid, $3, 'available')`,
			unitID,
			bitrixTestToolID,
			"BITRIX-UNIT-"+string(rune('1'+index)),
		)
	}
	batch.Queue(
		`INSERT INTO clients (
			id, phone, phone_verified_at, full_name, birth_date, status, bitrix_contact_id
		 )
		 VALUES (
			$1::uuid, '+79990000003', NOW(), 'Иван Иванов', '1990-01-01', 'verified', '42'
		 )`,
		bitrixTestClientID,
	)
	results := pool.SendBatch(context.Background(), batch)
	for index := 0; index < batch.Len(); index++ {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			t.Fatalf("seed Bitrix test statement %d: %v", index+1, err)
		}
	}
	if err := results.Close(); err != nil {
		t.Fatalf("close Bitrix seed batch: %v", err)
	}
}

func createBitrixTestRental(
	t *testing.T,
	repository *rentals.PostgresRepository,
	pool *pgxpool.Pool,
	dealID string,
) rentals.RentalRequest {
	t.Helper()
	now := time.Now().UTC()
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, 2)
	rental, err := repository.Create(context.Background(), rentals.CreateCommand{
		ClientID:       bitrixTestClientID,
		ToolID:         bitrixTestToolID,
		StartDate:      startDate,
		EndDate:        startDate.AddDate(0, 0, 2),
		RentalDays:     3,
		DeliveryMethod: rentals.DeliverySelfPickup,
		CourierFee:     100_000,
		ExpiresAt:      now.Add(30 * time.Minute),
		Now:            now,
	})
	if err != nil {
		t.Fatalf("create Bitrix test rental: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE rental_requests SET bitrix_deal_id = $2 WHERE id = $1::uuid`,
		rental.ID,
		dealID,
	); err != nil {
		t.Fatalf("assign Bitrix test deal: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE integration_outbox
		 SET status = 'completed', processed_at = NOW()
		 WHERE status <> 'completed'`,
	); err != nil {
		t.Fatalf("complete Bitrix test outbox: %v", err)
	}
	return rental
}

func storeBitrixTestEvent(
	t *testing.T,
	repository *PostgresRepository,
	eventKey string,
	dealID string,
) {
	t.Helper()
	_, created, err := repository.StoreInboundEvent(
		context.Background(),
		eventKey,
		dealID,
		json.RawMessage(`{"deal_id":"`+dealID+`","stage":"untrusted"}`),
		time.Now().UTC(),
	)
	if err != nil || !created {
		t.Fatalf("store Bitrix test event: created=%t err=%v", created, err)
	}
}

func assertRentalAndHoldStatus(
	t *testing.T,
	pool *pgxpool.Pool,
	rentalID string,
	expectedRental string,
	expectedHold string,
) {
	t.Helper()
	var rentalStatus string
	var holdStatus string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT rr.status, h.status
		 FROM rental_requests AS rr
		 JOIN tool_holds AS h ON h.rental_request_id = rr.id
		 WHERE rr.id = $1::uuid`,
		rentalID,
	).Scan(&rentalStatus, &holdStatus); err != nil {
		t.Fatalf("query rental/hold status: %v", err)
	}
	if rentalStatus != expectedRental || holdStatus != expectedHold {
		t.Fatalf(
			"unexpected status: rental=%q hold=%q expected rental=%q hold=%q",
			rentalStatus,
			holdStatus,
			expectedRental,
			expectedHold,
		)
	}
}

func assertStatusHistoryCount(
	t *testing.T,
	pool *pgxpool.Pool,
	rentalID string,
	expected int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM rental_status_history WHERE rental_request_id = $1::uuid`,
		rentalID,
	).Scan(&count); err != nil {
		t.Fatalf("count rental status history: %v", err)
	}
	if count != expected {
		t.Fatalf("status history count=%d, expected %d", count, expected)
	}
}

func assertNoInboundDealUpdate(
	t *testing.T,
	pool *pgxpool.Pool,
	rentalID string,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*)
		 FROM integration_outbox
		 WHERE aggregate_id = $1::uuid
		   AND event_type = 'bitrix.deal.update'`,
		rentalID,
	).Scan(&count); err != nil {
		t.Fatalf("query inbound deal updates: %v", err)
	}
	if count != 0 {
		t.Fatalf("inbound Bitrix transition created %d reverse deal updates", count)
	}
}

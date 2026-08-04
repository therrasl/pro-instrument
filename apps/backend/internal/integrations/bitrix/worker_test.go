package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

type fakeWorkerRepository struct {
	clientData      ClientSyncData
	rentalData      RentalSyncData
	savedContactID  string
	savedDealID     string
	retryOutbox     bool
	completedOutbox bool
	failedOutbox    bool
}

func (repository *fakeWorkerRepository) ClaimOutbox(
	context.Context,
	time.Time,
	time.Time,
	int,
) (OutboxEvent, bool, error) {
	return OutboxEvent{}, false, nil
}

func (repository *fakeWorkerRepository) CompleteOutbox(
	context.Context,
	string,
	time.Time,
) error {
	repository.completedOutbox = true
	return nil
}

func (repository *fakeWorkerRepository) RetryOutbox(
	context.Context,
	string,
	time.Time,
	string,
) error {
	repository.retryOutbox = true
	return nil
}

func (repository *fakeWorkerRepository) FailOutbox(
	context.Context,
	string,
	int,
	string,
) error {
	repository.failedOutbox = true
	return nil
}

func (repository *fakeWorkerRepository) GetClientSyncData(
	context.Context,
	string,
) (ClientSyncData, error) {
	data := repository.clientData
	if repository.savedContactID != "" {
		data.ContactID = repository.savedContactID
	}
	return data, nil
}

func (repository *fakeWorkerRepository) SaveContactID(
	_ context.Context,
	_ string,
	contactID string,
) error {
	repository.savedContactID = contactID
	return nil
}

func (repository *fakeWorkerRepository) GetRentalSyncData(
	context.Context,
	string,
) (RentalSyncData, error) {
	data := repository.rentalData
	if repository.savedContactID != "" {
		data.ContactID = repository.savedContactID
	}
	if repository.savedDealID != "" {
		data.DealID = repository.savedDealID
	}
	return data, nil
}

func (repository *fakeWorkerRepository) SaveDealID(
	_ context.Context,
	_ string,
	dealID string,
) error {
	repository.savedDealID = dealID
	return nil
}

func (repository *fakeWorkerRepository) ClaimInbound(
	context.Context,
	time.Time,
	time.Time,
	int,
) (InboundEvent, bool, error) {
	return InboundEvent{}, false, nil
}

func (repository *fakeWorkerRepository) CompleteInbound(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (repository *fakeWorkerRepository) RetryInbound(
	context.Context,
	string,
	time.Time,
	string,
) error {
	return nil
}

func (repository *fakeWorkerRepository) RejectInbound(
	context.Context,
	string,
	int,
	string,
	time.Time,
) error {
	return nil
}

type unusedStatusUpdater struct{}

func (unusedStatusUpdater) ApplyBitrixStatus(
	context.Context,
	string,
	string,
) (string, bool, error) {
	return "", false, nil
}

type trackingStatusUpdater struct {
	rentalID string
	status   string
	changed  []string
}

func (updater *trackingStatusUpdater) ApplyBitrixStatus(
	_ context.Context,
	_ string,
	targetStatus string,
) (string, bool, error) {
	if updater.status == targetStatus {
		return updater.rentalID, false, nil
	}
	if !testValidStatusTransition(updater.status, targetStatus) {
		return "", false, rentals.ErrInvalidStatusTransition
	}
	updater.status = targetStatus
	updater.changed = append(updater.changed, targetStatus)
	return updater.rentalID, true, nil
}

func testValidStatusTransition(currentStatus string, targetStatus string) bool {
	switch currentStatus {
	case rentals.StatusPendingManager:
		return targetStatus == rentals.StatusAwaitingPayment ||
			targetStatus == rentals.StatusRejected
	case rentals.StatusAwaitingPayment:
		return targetStatus == rentals.StatusPaid ||
			targetStatus == rentals.StatusRejected
	case rentals.StatusPaid:
		return targetStatus == rentals.StatusPreparing
	case rentals.StatusPreparing:
		return targetStatus == rentals.StatusReady
	case rentals.StatusReady:
		return targetStatus == rentals.StatusHandedToCourier ||
			targetStatus == rentals.StatusRented
	case rentals.StatusHandedToCourier:
		return targetStatus == rentals.StatusRented
	case rentals.StatusRented:
		return targetStatus == rentals.StatusAwaitingReturn
	case rentals.StatusAwaitingReturn:
		return targetStatus == rentals.StatusInspection
	case rentals.StatusInspection:
		return targetStatus == rentals.StatusCompleted
	default:
		return false
	}
}

func TestInboundFastForwardAppliesEveryDomainTransitionAndDuplicatesAreSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/crm.deal.get.json" {
			http.NotFound(response, request)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"result": map[string]any{
				"ID":          "100",
				"CATEGORY_ID": "12",
				"STAGE_ID":    "C12:UC_KQXWS6",
			},
		})
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{}
	updater := &trackingStatusUpdater{
		rentalID: "50000000-0000-4000-8000-000000000001",
		status:   rentals.StatusPendingManager,
	}
	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		updater,
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)

	for _, eventID := range []string{
		"60000000-0000-4000-8000-000000000001",
		"60000000-0000-4000-8000-000000000002",
	} {
		err := worker.processInbound(context.Background(), InboundEvent{
			ID:           eventID,
			BitrixDealID: "100",
			Attempts:     1,
		}, now)
		if err != nil {
			t.Fatalf("process inbound event %s: %v", eventID, err)
		}
	}

	expected := []string{
		rentals.StatusAwaitingPayment,
		rentals.StatusPaid,
		rentals.StatusPreparing,
	}
	if updater.status != rentals.StatusPreparing ||
		len(updater.changed) != len(expected) {
		t.Fatalf("unexpected fast-forward result: status=%q changes=%v", updater.status, updater.changed)
	}
	for index := range expected {
		if updater.changed[index] != expected[index] {
			t.Fatalf("unexpected transition chain: %v", updater.changed)
		}
	}
}

func TestInboundFastForwardStillRejectsInvalidBackwardTransition(t *testing.T) {
	updater := &trackingStatusUpdater{
		rentalID: "50000000-0000-4000-8000-000000000001",
		status:   rentals.StatusPreparing,
	}
	worker := NewWorker(
		&fakeWorkerRepository{},
		nil,
		updater,
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)

	_, err := worker.applyInboundStatus(
		context.Background(),
		"100",
		rentals.StatusAwaitingPayment,
	)
	if !errors.Is(err, rentals.ErrInvalidStatusTransition) {
		t.Fatalf("expected invalid backward transition, got %v", err)
	}
	if updater.status != rentals.StatusPreparing || len(updater.changed) != 0 {
		t.Fatalf("invalid transition changed rental: %#v", updater)
	}
}

func TestDealCreatedOnlyOnce(t *testing.T) {
	var createCalls atomic.Int32
	var comments string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/crm.deal.list.json":
			_ = json.NewEncoder(response).Encode(map[string]any{"result": []any{}})
		case "/crm.deal.add.json":
			createCalls.Add(1)
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode deal request: %v", err)
			}
			comments, _ = body.Fields["COMMENTS"].(string)
			if body.Fields["CATEGORY_ID"] != float64(12) ||
				body.Fields["STAGE_ID"] != "C12:NEW" ||
				body.Fields["CONTACT_ID"] != "42" {
				t.Fatalf("unexpected deal fields: %#v", body.Fields)
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"result": "100"})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{rentalData: testRentalSyncData()}
	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		unusedStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)

	if err := worker.createDeal(context.Background(), repository.rentalData.RentalID); err != nil {
		t.Fatalf("create deal: %v", err)
	}
	if err := worker.createDeal(context.Background(), repository.rentalData.RentalID); err != nil {
		t.Fatalf("repeat deal creation: %v", err)
	}
	if createCalls.Load() != 1 || repository.savedDealID != "100" {
		t.Fatalf(
			"deal was not idempotent: calls=%d saved=%q",
			createCalls.Load(),
			repository.savedDealID,
		)
	}
	for _, fragment := range []string{
		repository.rentalData.RentalID,
		repository.rentalData.ClientPhone,
		repository.rentalData.ToolName,
		"1000.00 RUB",
		"2000.00 RUB",
	} {
		if !strings.Contains(comments, fragment) {
			t.Fatalf("deal comments do not contain %q: %s", fragment, comments)
		}
	}
}

func TestEmptyDuplicateResultCreatesContactThenDeal(t *testing.T) {
	var contactCreateCalls atomic.Int32
	var dealCreateCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/crm.duplicate.findbycomm.json":
			_, _ = response.Write([]byte(`{"result":[]}`))
		case "/crm.contact.add.json":
			contactCreateCalls.Add(1)
			_ = json.NewEncoder(response).Encode(map[string]any{"result": 42})
		case "/crm.deal.list.json":
			_ = json.NewEncoder(response).Encode(map[string]any{"result": []any{}})
		case "/crm.deal.add.json":
			dealCreateCalls.Add(1)
			_ = json.NewEncoder(response).Encode(map[string]any{"result": 100})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	rentalData := testRentalSyncData()
	rentalData.ContactID = ""
	repository := &fakeWorkerRepository{
		clientData: ClientSyncData{
			ClientID: rentalData.ClientID,
			FullName: rentalData.ClientFullName,
			Phone:    rentalData.ClientPhone,
		},
		rentalData: rentalData,
	}
	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		unusedStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)

	if err := worker.upsertContact(context.Background(), rentalData.ClientID); err != nil {
		t.Fatalf("upsert contact: %v", err)
	}
	if repository.savedContactID != "42" {
		t.Fatalf("created contact ID was not saved: %q", repository.savedContactID)
	}
	if err := worker.createDeal(context.Background(), rentalData.RentalID); err != nil {
		t.Fatalf("create deal after contact upsert: %v", err)
	}
	if repository.savedDealID != "100" ||
		contactCreateCalls.Load() != 1 ||
		dealCreateCalls.Load() != 1 {
		t.Fatalf(
			"unexpected upsert chain: contact=%q deal=%q contact_calls=%d deal_calls=%d",
			repository.savedContactID,
			repository.savedDealID,
			contactCreateCalls.Load(),
			dealCreateCalls.Load(),
		)
	}
}

func TestOutboxRetriesAfterTemporaryBitrixError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"error":             "INTERNAL_ERROR",
				"error_description": "temporary",
			})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"result": map[string]any{"CONTACT": []string{"42"}},
		})
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{clientData: ClientSyncData{
		ClientID: "40000000-0000-4000-8000-000000000001",
		FullName: "Иван Иванов",
		Phone:    "+79990000001",
	}}
	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		unusedStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)

	event := OutboxEvent{
		ID:          "60000000-0000-4000-8000-000000000001",
		EventType:   EventContactUpsert,
		AggregateID: repository.clientData.ClientID,
		Attempts:    1,
	}
	if err := worker.processOutbox(context.Background(), event, now); err != nil {
		t.Fatalf("process temporary error: %v", err)
	}
	if !repository.retryOutbox || repository.failedOutbox {
		t.Fatalf("temporary failure was not scheduled for retry: %#v", repository)
	}

	repository.retryOutbox = false
	event.Attempts = 2
	if err := worker.processOutbox(context.Background(), event, now.Add(time.Minute)); err != nil {
		t.Fatalf("process retry: %v", err)
	}
	if !repository.completedOutbox || repository.savedContactID != "42" {
		t.Fatalf("retry did not complete contact upsert: %#v", repository)
	}
}

func TestDealUpdateUsesMappedStage(t *testing.T) {
	var receivedStage string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/crm.deal.update.json" {
			http.NotFound(response, request)
			return
		}
		var body struct {
			ID     string         `json:"id"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode deal update: %v", err)
		}
		if body.ID != "100" {
			t.Fatalf("unexpected deal id: %q", body.ID)
		}
		receivedStage, _ = body.Fields["STAGE_ID"].(string)
		_ = json.NewEncoder(response).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	data := testRentalSyncData()
	data.DealID = "100"
	data.Status = "ready"
	repository := &fakeWorkerRepository{rentalData: data}
	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		unusedStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	)
	if err := worker.updateDeal(context.Background(), data.RentalID); err != nil {
		t.Fatalf("update deal: %v", err)
	}
	if receivedStage != "C12:UC_3C6VH5" {
		t.Fatalf("unexpected mapped stage: %q", receivedStage)
	}
}

func testRentalSyncData() RentalSyncData {
	return RentalSyncData{
		RentalID:        "50000000-0000-4000-8000-000000000001",
		ClientID:        "40000000-0000-4000-8000-000000000001",
		ClientFullName:  "Иван Иванов",
		ClientPhone:     "+79990000001",
		ContactID:       "42",
		ToolName:        "Перфоратор",
		StartDate:       "2026-08-01",
		EndDate:         "2026-08-03",
		RentalPrice:     100_000,
		DepositAmount:   200_000,
		DeliveryCost:    50_000,
		TotalAmount:     350_000,
		DeliveryMethod:  "courier",
		DeliveryAddress: "Москва",
		Status:          "pending_manager",
	}
}

func testBitrixSettings() config.BitrixConfig {
	return config.BitrixConfig{
		Enabled:      true,
		HTTPTimeout:  time.Second,
		PollInterval: time.Millisecond,
		RetryBase:    time.Second,
		MaxAttempts:  3,
		CategoryID:   12,
		Stages: config.BitrixStages{
			Application:     "C12:NEW",
			AwaitingPayment: "C12:UC_RRNJ1L",
			Paid:            "C12:UC_48E30A",
			Preparing:       "C12:UC_KQXWS6",
			Ready:           "C12:UC_3C6VH5",
			Courier:         "C12:UC_NC65FK",
			Rented:          "C12:UC_PL3Q5Q",
			AwaitingReturn:  "C12:UC_DHV3I2",
			Inspection:      "C12:UC_6WE1PX",
			Completed:       "C12:WON",
			Failed:          "C12:LOSE",
		},
	}
}

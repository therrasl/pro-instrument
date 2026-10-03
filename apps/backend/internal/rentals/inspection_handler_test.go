package rentals

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
)

type fakeInspectionRepo struct {
	data          InspectionViewData
	updatedStatus string
	actorType     string
	actorID       string
}

func (f *fakeInspectionRepo) GetInspectionViewData(_ context.Context, rentalID string) (InspectionViewData, error) {
	if f.data.RentalID != "" {
		return f.data, nil
	}
	return InspectionViewData{
		RentalID:         rentalID,
		OrderNumber:      "PI-2026-TEST",
		Status:           StatusAwaitingReturn,
		ToolName:         "Перфоратор Bosch",
		InventoryNumber:  "DRL-001",
		ClientName:       "Иван Тестов",
		ClientPhone:      "+79991112233",
		StartDate:        "01.10.2026",
		EndDate:          "05.10.2026",
		RentalDays:       5,
		RentalPrice:      500000,
		DepositAmount:    1000000,
		DepositStatus:    "paid",
		RefundableAmount: 1000000,
		RefundedAmount:   0,
		WithheldAmount:   0,
		BitrixDealID:     "12345",
		HandoverPhotos: map[string]InspectionPhoto{
			PhotoTypeBody: {
				ID:        "photo-1",
				Phase:     PhotoPhaseHandover,
				PhotoType: PhotoTypeBody,
				URL:       "/api/v1/rentals/" + rentalID + "/photos/photo-1",
				CreatedAt: time.Now(),
			},
		},
		ReturnPhotos: map[string]InspectionPhoto{
			PhotoTypeBody: {
				ID:        "photo-2",
				Phase:     PhotoPhaseReturn,
				PhotoType: PhotoTypeBody,
				URL:       "/api/v1/rentals/" + rentalID + "/photos/photo-2",
				CreatedAt: time.Now(),
			},
		},
	}, nil
}

func (f *fakeInspectionRepo) UpdateRentalStatus(_ context.Context, rentalID string, targetStatus string, actorType string, actorID string, _ time.Time) error {
	f.updatedStatus = targetStatus
	f.actorType = actorType
	f.actorID = actorID
	return nil
}

type fakeBitrixInspectionClient struct {
	updatedStage string
	lastComment  string
}

func (b *fakeBitrixInspectionClient) UpdateDeal(_ context.Context, dealID string, fields map[string]any) error {
	if stage, ok := fields["STAGE_ID"].(string); ok {
		b.updatedStage = stage
	}
	return nil
}

func (b *fakeBitrixInspectionClient) AddDealComment(_ context.Context, dealID string, comment string) error {
	b.lastComment = comment
	return nil
}

func setupInspectionTest() (*InspectionHandler, *fakeInspectionRepo, *fakeBitrixInspectionClient, *bool, *int64, *int64) {
	repo := &fakeInspectionRepo{}
	bitrixClient := &fakeBitrixInspectionClient{}
	settled := false
	var settledRefund int64
	var settledWithhold int64

	settler := DepositSettlerFunc(func(ctx context.Context, rentalID string, refundAmount int64, withholdAmount int64, reason string) error {
		settled = true
		settledRefund = refundAmount
		settledWithhold = withholdAmount
		return nil
	})

	stages := config.BitrixStages{
		Completed:  "C12:WON",
		Inspection: "C12:INSPECTION",
	}

	handler := NewInspectionHandler(
		repo,
		settler,
		bitrixClient,
		stages,
		"https://portal.bitrix24.ru/rest/1/secret/",
		"test-secret",
		log.New(io.Discard, "", 0),
	)

	return handler, repo, bitrixClient, &settled, &settledRefund, &settledWithhold
}

func TestInspectionViewInvalidToken(t *testing.T) {
	handler, _, _, _, _, _ := setupInspectionTest()
	mux := http.NewServeMux()
	handler.Register(mux)

	rentalID := "d1000000-0000-4000-8000-000000000001"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rentals/"+rentalID+"/inspection-view?token=invalid-token", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 Forbidden, got %d", rec.Code)
	}
}

func TestInspectionViewSuccess(t *testing.T) {
	handler, _, _, _, _, _ := setupInspectionTest()
	mux := http.NewServeMux()
	handler.Register(mux)

	rentalID := "d1000000-0000-4000-8000-000000000001"
	token := GenerateInspectionToken("test-secret", rentalID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rentals/"+rentalID+"/inspection-view?token="+token, nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "PI-2026-TEST") {
		t.Fatalf("expected body to contain order number PI-2026-TEST")
	}
	if !strings.Contains(body, "Перфоратор Bosch") {
		t.Fatalf("expected body to contain tool name")
	}
	if !strings.Contains(body, "Возврат 100% залога") {
		t.Fatal("expected body to contain action button for 100% refund")
	}
}

func TestInspectionActionApproveFull(t *testing.T) {
	handler, repo, bitrixClient, settled, settledRefund, settledWithhold := setupInspectionTest()
	mux := http.NewServeMux()
	handler.Register(mux)

	rentalID := "d1000000-0000-4000-8000-000000000001"
	token := GenerateInspectionToken("test-secret", rentalID)

	form := url.Values{}
	form.Set("action", "approve_full")
	form.Set("token", token)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rentals/"+rentalID+"/inspection-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d, body: %s", rec.Code, rec.Body.String())
	}

	if !*settled {
		t.Fatal("expected deposit to be settled")
	}
	if *settledRefund != 1000000 || *settledWithhold != 0 {
		t.Fatalf("expected 1000000 refund and 0 withhold, got refund=%d withhold=%d", *settledRefund, *settledWithhold)
	}
	if repo.updatedStatus != StatusCompleted {
		t.Fatalf("expected status completed, got %s", repo.updatedStatus)
	}
	if bitrixClient.updatedStage != "C12:WON" {
		t.Fatalf("expected bitrix deal stage C12:WON, got %s", bitrixClient.updatedStage)
	}
	if !strings.Contains(bitrixClient.lastComment, "100%") {
		t.Fatalf("expected bitrix comment to mention 100 percent refund, got: %s", bitrixClient.lastComment)
	}
}

func TestInspectionActionPartialRefund(t *testing.T) {
	handler, repo, bitrixClient, settled, settledRefund, settledWithhold := setupInspectionTest()
	mux := http.NewServeMux()
	handler.Register(mux)

	rentalID := "d1000000-0000-4000-8000-000000000001"
	token := GenerateInspectionToken("test-secret", rentalID)

	// Deposit is 10,000 RUB (1,000,000 kopecks).
	// Withhold 2,500 RUB (250,000 kopecks), refund 7,500 RUB (750,000 kopecks).
	form := url.Values{}
	form.Set("action", "partial_refund")
	form.Set("token", token)
	form.Set("deduction_amount", "2500")
	form.Set("reason", "Грязный инструмент, удержание за мойку")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rentals/"+rentalID+"/inspection-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d, body: %s", rec.Code, rec.Body.String())
	}

	if !*settled {
		t.Fatal("expected deposit to be settled")
	}
	if *settledRefund != 750000 || *settledWithhold != 250000 {
		t.Fatalf("expected refund=750000 withhold=250000, got refund=%d withhold=%d", *settledRefund, *settledWithhold)
	}
	if repo.updatedStatus != StatusCompleted {
		t.Fatalf("expected status completed, got %s", repo.updatedStatus)
	}
	if bitrixClient.updatedStage != "C12:WON" {
		t.Fatalf("expected bitrix deal stage C12:WON, got %s", bitrixClient.updatedStage)
	}
	if !strings.Contains(bitrixClient.lastComment, "Грязный инструмент") {
		t.Fatalf("expected bitrix comment to mention reason, got: %s", bitrixClient.lastComment)
	}
}

func TestInspectionActionDispute(t *testing.T) {
	handler, repo, bitrixClient, settled, _, _ := setupInspectionTest()
	mux := http.NewServeMux()
	handler.Register(mux)

	rentalID := "d1000000-0000-4000-8000-000000000001"
	token := GenerateInspectionToken("test-secret", rentalID)

	form := url.Values{}
	form.Set("action", "dispute")
	form.Set("token", token)
	form.Set("reason", "Сломан редуктор, требуется экспертиза")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rentals/"+rentalID+"/inspection-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	// For dispute, deposit must NOT be settled yet!
	if *settled {
		t.Fatal("expected deposit not to be settled during dispute")
	}
	if repo.updatedStatus != StatusInspection {
		t.Fatalf("expected status inspection, got %s", repo.updatedStatus)
	}
	if bitrixClient.updatedStage != "C12:INSPECTION" {
		t.Fatalf("expected bitrix deal stage C12:INSPECTION, got %s", bitrixClient.updatedStage)
	}
	if !strings.Contains(bitrixClient.lastComment, "Сломан редуктор") {
		t.Fatalf("expected bitrix comment to mention reason, got: %s", bitrixClient.lastComment)
	}
}

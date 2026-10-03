package bitrix

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

type fakeVerificationReviewer struct {
	approvedClientID string
	rejectedClientID string
	rejectedReason   string
}

func (f *fakeVerificationReviewer) Approve(_ context.Context, clientID string, _ string) (verification.Review, error) {
	f.approvedClientID = clientID
	return verification.Review{ClientID: clientID, Decision: "approved"}, nil
}

func (f *fakeVerificationReviewer) Reject(_ context.Context, clientID string, _ string, reason string) (verification.Review, error) {
	f.rejectedClientID = clientID
	f.rejectedReason = reason
	return verification.Review{ClientID: clientID, Decision: "rejected", Reason: &reason}, nil
}

type fakePushNotifier struct {
	approvedClientID string
	rejectedClientID string
	rejectedReason   string
}

func (f *fakePushNotifier) NotifyVerificationApproved(_ context.Context, clientID string) error {
	f.approvedClientID = clientID
	return nil
}

func (f *fakePushNotifier) NotifyVerificationRejected(_ context.Context, clientID string, reason string) error {
	f.rejectedClientID = clientID
	f.rejectedReason = reason
	return nil
}

func TestVerificationNotifierSendsTimelineCommentAndActivity(t *testing.T) {
	commentAdded := false
	activityAdded := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm.timeline.comment.add.json":
			commentAdded = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			fields := body["fields"].(map[string]any)
			comment := fields["COMMENT"].(string)
			if !strings.Contains(comment, "Главный разворот паспорта") || !strings.Contains(comment, "https://example.com/api/v1/verification/documents/doc-1/view") {
				t.Errorf("unexpected comment content: %s", comment)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": 123})
		case "/crm.activity.todo.add.json":
			activityAdded = true
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": 456}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{
		clientData: ClientSyncData{
			ClientID:  "client-1",
			FullName:  "Иван Иванов",
			Phone:     "+79991234567",
			ContactID: "contact-100",
		},
	}

	client := NewHTTPClient(server.URL, time.Second)
	notifier := NewVerificationNotifier(client, repository, "https://example.com", "test-secret")

	docs := []verification.Document{
		{ID: "doc-1", DocumentType: verification.DocumentPassportMain},
		{ID: "doc-2", DocumentType: verification.DocumentPassportRegistration},
		{ID: "doc-3", DocumentType: verification.DocumentSelfieWithPassport},
	}

	err := notifier.NotifyDocumentsSubmitted(context.Background(), "client-1", docs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !commentAdded {
		t.Error("expected timeline comment to be added")
	}
	if !activityAdded {
		t.Error("expected activity to be added")
	}
}

func TestWorkerProcessesInboundContactApproval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crm.contact.get.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"ID":                  "100",
					"NAME":                "Иван",
					"LAST_NAME":           "Иванов",
					"PHONE":               []map[string]string{{"VALUE": "+79991234567"}},
					"UF_CRM_1786619870585": "300",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{}
	reviewer := &fakeVerificationReviewer{}
	pushNotifier := &fakePushNotifier{}

	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		&trackingStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	).SetVerificationReviewer(reviewer).SetVerificationPushNotifier(pushNotifier)

	event := InboundEvent{
		ID:           "event-contact-1",
		BitrixDealID: "100",
		RawPayload:   []byte(`{"event":"ONCRMCONTACTUPDATE","data":{"FIELDS":{"ID":"100"}}}`),
	}

	err := worker.processInbound(context.Background(), event, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reviewer.approvedClientID != "test-client-id" {
		t.Errorf("expected client to be approved, got %q", reviewer.approvedClientID)
	}
	if pushNotifier.approvedClientID != "test-client-id" {
		t.Errorf("expected push to be sent for approved client, got %q", pushNotifier.approvedClientID)
	}
}

func TestWorkerProcessesInboundContactRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crm.contact.get.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"ID":                  "100",
					"NAME":                "Иван",
					"LAST_NAME":           "Иванов",
					"COMMENTS":            "Паспорт размыт",
					"PHONE":               []map[string]string{{"VALUE": "+79991234567"}},
					"UF_CRM_1786619870585": "302",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	repository := &fakeWorkerRepository{}
	reviewer := &fakeVerificationReviewer{}
	pushNotifier := &fakePushNotifier{}

	worker := NewWorker(
		repository,
		NewHTTPClient(server.URL, time.Second),
		&trackingStatusUpdater{},
		testBitrixSettings(),
		log.New(io.Discard, "", 0),
	).SetVerificationReviewer(reviewer).SetVerificationPushNotifier(pushNotifier)

	event := InboundEvent{
		ID:           "event-contact-2",
		BitrixDealID: "100",
		RawPayload:   []byte(`{"event":"ONCRMCONTACTUPDATE","data":{"FIELDS":{"ID":"100"}}}`),
	}

	err := worker.processInbound(context.Background(), event, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reviewer.rejectedClientID != "test-client-id" {
		t.Errorf("expected client to be rejected, got %q", reviewer.rejectedClientID)
	}
	if reviewer.rejectedReason != "Паспорт размыт" {
		t.Errorf("expected rejection reason 'Паспорт размыт', got %q", reviewer.rejectedReason)
	}
	if pushNotifier.rejectedClientID != "test-client-id" {
		t.Errorf("expected push to be sent for rejected client, got %q", pushNotifier.rejectedClientID)
	}
}

func TestDirectVerificationWebhookEndpoint(t *testing.T) {
	repository := &fakeWorkerRepository{}
	reviewer := &fakeVerificationReviewer{}
	pushNotifier := &fakePushNotifier{}

	handler := NewVerificationWebhookHandler(
		repository,
		reviewer,
		pushNotifier,
		true,
		"test-secret",
		log.New(io.Discard, "", 0),
	)

	mux := http.NewServeMux()
	handler.Register(mux)

	// 1. Unauthorized request
	req := httptest.NewRequest("POST", "/api/v1/integrations/bitrix/verification", strings.NewReader(`{"contact_id":"100","decision":"approved"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// 2. Authorized approval
	req = httptest.NewRequest("POST", "/api/v1/integrations/bitrix/verification", strings.NewReader(`{"contact_id":"100","decision":"approved","auth_token":"test-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if reviewer.approvedClientID != "test-client-id" {
		t.Errorf("expected client approved, got %q", reviewer.approvedClientID)
	}

	// 3. Authorized rejection
	req = httptest.NewRequest("POST", "/api/v1/integrations/bitrix/verification", strings.NewReader(`{"contact_id":"100","decision":"rejected","reason":"Нечеткое фото","auth_token":"test-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if reviewer.rejectedClientID != "test-client-id" || reviewer.rejectedReason != "Нечеткое фото" {
		t.Errorf("expected client rejected with reason, got %q / %q", reviewer.rejectedClientID, reviewer.rejectedReason)
	}

	// 4. Quick review: 1-click approval
	approveToken := GenerateQuickReviewToken("test-secret", "test-client-id", "approved")
	req = httptest.NewRequest("GET", "/api/v1/integrations/bitrix/quick-review?client_id=test-client-id&decision=approved&token="+approveToken, nil)
	rec = httptest.NewRecorder()
	reviewer.approvedClientID = ""
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for quick review approve, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Документы одобрены") {
		t.Errorf("expected success HTML, got %s", rec.Body.String())
	}
	if reviewer.approvedClientID != "test-client-id" {
		t.Errorf("expected client approved via quick review, got %q", reviewer.approvedClientID)
	}
}

func TestSplitFullName(t *testing.T) {
	testCases := []struct {
		input      string
		phone      string
		wantLast   string
		wantFirst  string
		wantSecond string
	}{
		{"", "+79991112233", "", "Клиент +79991112233", ""},
		{"Иван", "+79991112233", "", "Иван", ""},
		{"Иванов Иван", "+79991112233", "Иванов", "Иван", ""},
		{"Смирнов Дмитрий Александрович", "+79991112233", "Смирнов", "Дмитрий", "Александрович"},
		{"ван де Зандсхюлп Ботик", "+79991112233", "ван", "де", "Зандсхюлп Ботик"},
	}

	for _, tc := range testCases {
		last, first, second := splitFullName(tc.input, tc.phone)
		if last != tc.wantLast || first != tc.wantFirst || second != tc.wantSecond {
			t.Errorf("splitFullName(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.input, last, first, second, tc.wantLast, tc.wantFirst, tc.wantSecond)
		}
	}
}

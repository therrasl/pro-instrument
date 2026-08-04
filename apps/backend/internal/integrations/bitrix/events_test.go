package bitrix

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEventRepository struct {
	mu      sync.Mutex
	events  map[string]string
	calls   int
	dealID  string
	payload json.RawMessage
}

func (repository *fakeEventRepository) StoreInboundEvent(
	_ context.Context,
	eventKey string,
	dealID string,
	payload json.RawMessage,
	_ time.Time,
) (string, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls++
	repository.dealID = dealID
	repository.payload = append(json.RawMessage(nil), payload...)
	if eventID, ok := repository.events[eventKey]; ok {
		return eventID, false, nil
	}
	eventID := "70000000-0000-4000-8000-000000000001"
	repository.events[eventKey] = eventID
	return eventID, true, nil
}

func TestBitrixFormEventUsesApplicationTokenAndIsIdempotent(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	repository := &fakeEventRepository{events: make(map[string]string)}
	handler := NewEventsHandler(
		repository,
		true,
		secret,
		log.New(io.Discard, "", 0),
	)
	mux := http.NewServeMux()
	handler.Register(mux)

	form := url.Values{
		"event":                   {"ONCRMDEALUPDATE"},
		"data[FIELDS][ID]":        {"321"},
		"auth[application_token]": {secret},
		"ts":                      {"1785400000"},
	}
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/integrations/bitrix/events",
			strings.NewReader(form.Encode()),
		)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)

		if response.Code != http.StatusAccepted {
			t.Fatalf(
				"attempt %d: expected status %d, got %d: %s",
				attempt+1,
				http.StatusAccepted,
				response.Code,
				response.Body.String(),
			)
		}
		var result struct {
			Duplicate bool `json:"duplicate"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatalf("decode form event response: %v", err)
		}
		if result.Duplicate != (attempt == 1) {
			t.Fatalf("attempt %d: unexpected duplicate=%t", attempt+1, result.Duplicate)
		}
	}

	if repository.dealID != "321" {
		t.Fatalf("unexpected extracted deal ID: %q", repository.dealID)
	}
	if repository.calls != 2 || len(repository.events) != 1 {
		t.Fatalf(
			"form event was not idempotent: calls=%d events=%d",
			repository.calls,
			len(repository.events),
		)
	}
	if strings.Contains(string(repository.payload), secret) ||
		strings.Contains(string(repository.payload), "application_token") {
		t.Fatalf("stored payload contains webhook token: %s", repository.payload)
	}
}

func TestBitrixFormEventRejectsInvalidApplicationToken(t *testing.T) {
	repository := &fakeEventRepository{events: make(map[string]string)}
	handler := NewEventsHandler(
		repository,
		true,
		"0123456789abcdef0123456789abcdef",
		log.New(io.Discard, "", 0),
	)
	mux := http.NewServeMux()
	handler.Register(mux)

	for _, token := range []string{"", "wrong-token"} {
		form := url.Values{
			"event":                   {"ONCRMDEALUPDATE"},
			"data[FIELDS][ID]":        {"321"},
			"auth[application_token]": {token},
		}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/integrations/bitrix/events",
			strings.NewReader(form.Encode()),
		)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: expected unauthorized, got %d", token, response.Code)
		}
	}
	if repository.calls != 0 {
		t.Fatalf("unauthorized events reached repository %d times", repository.calls)
	}
}

func TestBitrixEventsEndpointRequiresSeparateSecretAndIsIdempotent(t *testing.T) {
	repository := &fakeEventRepository{events: make(map[string]string)}
	handler := NewEventsHandler(
		repository,
		true,
		"0123456789abcdef0123456789abcdef",
		log.New(io.Discard, "", 0),
	)
	mux := http.NewServeMux()
	handler.Register(mux)

	unauthorized := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/bitrix/events",
		strings.NewReader(`{"event_id":"evt-1","deal_id":"100"}`),
	)
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	mux.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", unauthorizedResponse.Code)
	}

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/integrations/bitrix/events",
			strings.NewReader(`{"event_id":"evt-1","deal_id":"100","stage":"untrusted"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(bitrixWebhookSecretHeader, "0123456789abcdef0123456789abcdef")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("attempt %d: expected accepted, got %d", attempt+1, response.Code)
		}
		var result struct {
			Duplicate bool `json:"duplicate"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatalf("decode event response: %v", err)
		}
		if result.Duplicate != (attempt == 1) {
			t.Fatalf("attempt %d: unexpected duplicate=%t", attempt+1, result.Duplicate)
		}
	}
	if repository.calls != 2 || len(repository.events) != 1 {
		t.Fatalf("unexpected stored events: calls=%d events=%d", repository.calls, len(repository.events))
	}
}

func TestBitrixEventsEndpointDisabled(t *testing.T) {
	handler := NewEventsHandler(
		&fakeEventRepository{events: make(map[string]string)},
		false,
		"",
		log.New(io.Discard, "", 0),
	)
	mux := http.NewServeMux()
	handler.Register(mux)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/bitrix/events",
		strings.NewReader(`{"event_id":"evt-1","deal_id":"100"}`),
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected disabled status, got %d", response.Code)
	}
}

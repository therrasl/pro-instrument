package appconfig_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/appconfig"
)

func TestPublicAppConfigEnabled(t *testing.T) {
	response := serveAppConfig(true, "654321")
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["demo_mode"] != true || body["demo_otp_code"] != "654321" || body["message"] == "" {
		t.Fatalf("unexpected enabled config: %#v", body)
	}
	if len(body) != 3 {
		t.Fatalf("public config exposes unexpected fields: %#v", body)
	}
}

func TestPublicAppConfigDisabled(t *testing.T) {
	response := serveAppConfig(false, "should-not-leak")
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["demo_mode"] != false || len(body) != 1 {
		t.Fatalf("disabled config must only expose demo_mode=false: %#v", body)
	}
}

func serveAppConfig(enabled bool, code string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	appconfig.NewHandler(enabled, code).Register(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/app-config", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

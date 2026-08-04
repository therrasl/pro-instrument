package auth_test

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

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const clientID = "40000000-0000-4000-8000-000000000001"

type fakeAuthService struct {
	requestCode    func(context.Context, string) error
	verifyCode     func(context.Context, string, string) (auth.Session, error)
	authenticate   func(context.Context, string) (auth.Client, error)
	updateProfile  func(context.Context, string, auth.ProfilePatch) (auth.Client, error)
	acceptConsents func(context.Context, string, auth.ConsentInput) (auth.ConsentAcceptance, error)
}

func (service fakeAuthService) RequestCode(ctx context.Context, phone string) error {
	return service.requestCode(ctx, phone)
}

func (service fakeAuthService) VerifyCode(
	ctx context.Context,
	phone string,
	code string,
) (auth.Session, error) {
	return service.verifyCode(ctx, phone, code)
}

func (service fakeAuthService) Authenticate(ctx context.Context, token string) (auth.Client, error) {
	return service.authenticate(ctx, token)
}

func (service fakeAuthService) UpdateProfile(
	ctx context.Context,
	id string,
	patch auth.ProfilePatch,
) (auth.Client, error) {
	return service.updateProfile(ctx, id, patch)
}

func (service fakeAuthService) AcceptConsents(
	ctx context.Context,
	id string,
	input auth.ConsentInput,
) (auth.ConsentAcceptance, error) {
	return service.acceptConsents(ctx, id, input)
}

func TestRequestCodeDoesNotReturnCode(t *testing.T) {
	service := defaultAuthService()
	service.requestCode = func(_ context.Context, phone string) error {
		if phone != "+79991234567" {
			t.Fatalf("unexpected phone: %q", phone)
		}
		return nil
	}

	response := serveAuthRequest(
		service,
		http.MethodPost,
		"/api/v1/auth/request-code",
		`{"phone":"+79991234567"}`,
		"",
	)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}
	if strings.Contains(response.Body.String(), "123456") || strings.Contains(response.Body.String(), "code") {
		t.Fatalf("response exposes verification code: %s", response.Body.String())
	}
}

func TestVerifyCodeUsesGenericFailure(t *testing.T) {
	service := defaultAuthService()
	service.verifyCode = func(context.Context, string, string) (auth.Session, error) {
		return auth.Session{}, auth.ErrInvalidCode
	}

	response := serveAuthRequest(
		service,
		http.MethodPost,
		"/api/v1/auth/verify-code",
		`{"phone":"+79991234567","code":"000000"}`,
		"",
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
	if strings.Contains(response.Body.String(), "+79991234567") {
		t.Fatalf("response exposes phone state: %s", response.Body.String())
	}
}

func TestRentalEligibilityUsesServerVerificationStatus(t *testing.T) {
	service := defaultAuthService()
	service.authenticate = func(context.Context, string) (auth.Client, error) {
		return auth.Client{ID: clientID, Status: "pending_verification"}, nil
	}

	response := serveAuthRequest(
		service,
		http.MethodGet,
		"/api/v1/me/rental-eligibility",
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	var result struct {
		Eligible bool   `json:"eligible"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode eligibility: %v", err)
	}
	if result.Eligible || result.Reason != "documents_pending" {
		t.Fatalf("unexpected eligibility: %#v", result)
	}
}

func TestUploadedDocumentsRequireExplicitSubmission(t *testing.T) {
	service := defaultAuthService()
	service.authenticate = func(context.Context, string) (auth.Client, error) {
		return auth.Client{ID: clientID, Status: "documents_uploaded"}, nil
	}

	response := serveAuthRequest(
		service,
		http.MethodGet,
		"/api/v1/me/rental-eligibility",
		"",
		"Bearer session-token",
	)

	var result struct {
		Eligible bool   `json:"eligible"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode eligibility: %v", err)
	}
	if result.Eligible || result.Reason != "documents_required" {
		t.Fatalf("unexpected eligibility: %#v", result)
	}
}

func TestRequestCodeRateLimit(t *testing.T) {
	service := defaultAuthService()
	service.requestCode = func(context.Context, string) error {
		return auth.ErrRateLimited
	}

	response := serveAuthRequest(
		service,
		http.MethodPost,
		"/api/v1/auth/request-code",
		`{"phone":"+79991234567"}`,
		"",
	)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, response.Code)
	}
}

func TestGetMeWithBearerToken(t *testing.T) {
	service := defaultAuthService()
	service.authenticate = func(_ context.Context, token string) (auth.Client, error) {
		if token != "session-token" {
			t.Fatalf("unexpected token: %q", token)
		}
		return auth.Client{ID: clientID, Phone: "+79991234567", Status: "phone_verified"}, nil
	}

	response := serveAuthRequest(service, http.MethodGet, "/api/v1/me", "", "Bearer session-token")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var client auth.Client
	if err := json.NewDecoder(response.Body).Decode(&client); err != nil {
		t.Fatalf("decode client: %v", err)
	}
	if client.ID != clientID {
		t.Fatalf("unexpected client: %#v", client)
	}
}

func TestGetMeRequiresBearerToken(t *testing.T) {
	response := serveAuthRequest(defaultAuthService(), http.MethodGet, "/api/v1/me", "", "")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestPatchMeCompletesProfile(t *testing.T) {
	service := defaultAuthService()
	service.updateProfile = func(_ context.Context, id string, patch auth.ProfilePatch) (auth.Client, error) {
		if id != clientID || patch.FullName == nil || patch.BirthDate == nil {
			t.Fatalf("unexpected profile patch: id=%q patch=%#v", id, patch)
		}
		return auth.Client{ID: id, Status: "profile_completed"}, nil
	}

	response := serveAuthRequest(
		service,
		http.MethodPatch,
		"/api/v1/me",
		`{"full_name":"Иван Иванов","birth_date":"1990-01-01"}`,
		"Bearer session-token",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestAcceptConsentsCapturesRequestMetadata(t *testing.T) {
	service := defaultAuthService()
	service.acceptConsents = func(
		_ context.Context,
		id string,
		input auth.ConsentInput,
	) (auth.ConsentAcceptance, error) {
		if id != clientID || input.IPAddress != "192.0.2.1" || input.UserAgent != "backend-test" {
			t.Fatalf("unexpected consent metadata: id=%q input=%#v", id, input)
		}
		return auth.ConsentAcceptance{
			ID:             "consent-id",
			OfferVersion:   input.OfferVersion,
			PrivacyVersion: input.PrivacyVersion,
			AcceptedAt:     time.Now(),
		}, nil
	}

	response := serveAuthRequest(
		service,
		http.MethodPost,
		"/api/v1/me/consents",
		`{
			"offer_version":"offer-v1",
			"privacy_version":"privacy-v1",
			"offer_accepted":true,
			"privacy_accepted":true,
			"data_accuracy_confirmed":true,
			"rental_rules_accepted":true
		}`,
		"Bearer session-token",
	)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
}

func TestAcceptConsentsRejectsOfferText(t *testing.T) {
	service := defaultAuthService()
	response := serveAuthRequest(
		service,
		http.MethodPost,
		"/api/v1/me/consents",
		`{
			"offer_version":"offer-v1",
			"privacy_version":"privacy-v1",
			"offer_accepted":true,
			"privacy_accepted":true,
			"data_accuracy_confirmed":true,
			"rental_rules_accepted":true,
			"offer_text":"must not be accepted"
		}`,
		"Bearer session-token",
	)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func defaultAuthService() fakeAuthService {
	return fakeAuthService{
		requestCode: func(context.Context, string) error { return nil },
		verifyCode: func(context.Context, string, string) (auth.Session, error) {
			return auth.Session{AccessToken: "session-token", TokenType: "Bearer"}, nil
		},
		authenticate: func(context.Context, string) (auth.Client, error) {
			return auth.Client{ID: clientID, Phone: "+79991234567", Status: "phone_verified"}, nil
		},
		updateProfile: func(context.Context, string, auth.ProfilePatch) (auth.Client, error) {
			return auth.Client{ID: clientID, Status: "profile_completed"}, nil
		},
		acceptConsents: func(context.Context, string, auth.ConsentInput) (auth.ConsentAcceptance, error) {
			return auth.ConsentAcceptance{ID: "consent-id"}, nil
		},
	}
}

func serveAuthRequest(
	service auth.AuthService,
	method string,
	target string,
	body string,
	authorization string,
) *httptest.ResponseRecorder {
	handler := auth.NewHandler(service, log.New(io.Discard, "", 0))
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("User-Agent", "backend-test")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

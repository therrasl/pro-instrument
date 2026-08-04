package push_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/push"
)

const testClientID = "40000000-0000-4000-8000-000000000001"

type fakePushService struct {
	register func(context.Context, string, string, string) error
	delete   func(context.Context, string, string) error
}

func (service fakePushService) Register(
	ctx context.Context,
	clientID string,
	token string,
	platform string,
) error {
	return service.register(ctx, clientID, token, platform)
}

func (service fakePushService) Delete(ctx context.Context, clientID string, token string) error {
	return service.delete(ctx, clientID, token)
}

type fakeAuthService struct{}

func (fakeAuthService) RequestCode(context.Context, string) error { return nil }
func (fakeAuthService) VerifyCode(context.Context, string, string) (auth.Session, error) {
	return auth.Session{}, nil
}
func (fakeAuthService) Authenticate(_ context.Context, token string) (auth.Client, error) {
	if token != "session-token" {
		return auth.Client{}, auth.ErrUnauthorized
	}
	return auth.Client{ID: testClientID}, nil
}
func (fakeAuthService) UpdateProfile(context.Context, string, auth.ProfilePatch) (auth.Client, error) {
	return auth.Client{}, nil
}
func (fakeAuthService) AcceptConsents(
	context.Context,
	string,
	auth.ConsentInput,
) (auth.ConsentAcceptance, error) {
	return auth.ConsentAcceptance{}, nil
}

func TestRegisterAndDeletePushTokenEndpoints(t *testing.T) {
	const token = "ExpoPushToken[test-device-token]"
	registered := false
	deleted := false
	service := fakePushService{
		register: func(_ context.Context, clientID string, actualToken string, platform string) error {
			registered = clientID == testClientID && actualToken == token && platform == "android"
			return nil
		},
		delete: func(_ context.Context, clientID string, actualToken string) error {
			deleted = clientID == testClientID && actualToken == token
			return nil
		},
	}

	registerResponse := servePushRequest(
		service,
		http.MethodPost,
		`{"token":"`+token+`","platform":"android"}`,
		"Bearer session-token",
	)
	if registerResponse.Code != http.StatusNoContent || !registered {
		t.Fatalf("register: status=%d body=%s", registerResponse.Code, registerResponse.Body.String())
	}
	if strings.Contains(registerResponse.Body.String(), token) {
		t.Fatal("registration response exposes push token")
	}

	deleteResponse := servePushRequest(
		service,
		http.MethodDelete,
		`{"token":"`+token+`"}`,
		"Bearer session-token",
	)
	if deleteResponse.Code != http.StatusNoContent || !deleted {
		t.Fatalf("delete: status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestPushTokenEndpointRequiresAuthentication(t *testing.T) {
	response := servePushRequest(defaultPushService(), http.MethodPost, `{}`, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func servePushRequest(
	service push.Service,
	method string,
	body string,
	authorization string,
) *httptest.ResponseRecorder {
	logger := log.New(io.Discard, "", 0)
	authHandler := auth.NewHandler(fakeAuthService{}, logger)
	pushHandler := push.NewHandler(service, logger)
	mux := http.NewServeMux()
	pushHandler.Register(mux, authHandler.BearerAuth)
	request := httptest.NewRequest(method, "/api/v1/me/push-tokens", strings.NewReader(body))
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func defaultPushService() fakePushService {
	return fakePushService{
		register: func(context.Context, string, string, string) error { return nil },
		delete:   func(context.Context, string, string) error { return nil },
	}
}

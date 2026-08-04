package payments

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

func TestPaymentHandlerIgnoresMobileAmount(t *testing.T) {
	paymentService := &handlerPaymentService{
		result: CreateResult{
			Payment: Payment{
				ID:              "payment-1",
				RentalRequestID: "40000000-0000-4000-8000-000000000001",
				RentalAmount:    100_000,
				DepositAmount:   200_000,
				DeliveryAmount:  50_000,
				TotalAmount:     350_000,
				Currency:        "RUB",
				Status:          StatusPending,
			},
			Created: true,
		},
	}
	authHandler := auth.NewHandler(
		authServiceStub{client: auth.Client{ID: "client-1"}},
		log.New(io.Discard, "", 0),
	)
	handler := NewHandler(paymentService, log.New(io.Discard, "", 0))
	mux := http.NewServeMux()
	handler.Register(mux, authHandler.BearerAuth)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/rentals/40000000-0000-4000-8000-000000000001/payment",
		bytes.NewBufferString(`{"total_amount":1,"currency":"USD"}`),
	)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if paymentService.clientID != "client-1" ||
		paymentService.rentalID != "40000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected payment command: %#v", paymentService)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"total_amount":350000`)) {
		t.Fatalf("response used mobile amount: %s", response.Body.String())
	}
}

func TestPaymentHandlerRejectsExpiredDeadline(t *testing.T) {
	paymentService := &handlerPaymentService{err: ErrPaymentExpired}
	authHandler := auth.NewHandler(
		authServiceStub{client: auth.Client{ID: "client-1"}},
		log.New(io.Discard, "", 0),
	)
	handler := NewHandler(paymentService, log.New(io.Discard, "", 0))
	mux := http.NewServeMux()
	handler.Register(mux, authHandler.BearerAuth)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/rentals/40000000-0000-4000-8000-000000000001/payment",
		nil,
	)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
}

type handlerPaymentService struct {
	result   CreateResult
	err      error
	clientID string
	rentalID string
}

func (service *handlerPaymentService) Create(
	_ context.Context,
	clientID string,
	rentalID string,
) (CreateResult, error) {
	service.clientID = clientID
	service.rentalID = rentalID
	return service.result, service.err
}

type authServiceStub struct {
	client auth.Client
}

func (service authServiceStub) RequestCode(context.Context, string) error {
	return nil
}

func (service authServiceStub) VerifyCode(context.Context, string, string) (auth.Session, error) {
	return auth.Session{}, nil
}

func (service authServiceStub) Authenticate(context.Context, string) (auth.Client, error) {
	return service.client, nil
}

func (service authServiceStub) UpdateProfile(
	context.Context,
	string,
	auth.ProfilePatch,
) (auth.Client, error) {
	return auth.Client{}, nil
}

func (service authServiceStub) AcceptConsents(
	context.Context,
	string,
	auth.ConsentInput,
) (auth.ConsentAcceptance, error) {
	return auth.ConsentAcceptance{AcceptedAt: time.Now()}, nil
}

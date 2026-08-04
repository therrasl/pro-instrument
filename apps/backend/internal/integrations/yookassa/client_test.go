package yookassa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreatePaymentUsesBasicAuthAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodPost || request.URL.Path != "/payments" {
			http.NotFound(response, request)
			return
		}
		shopID, secret, ok := request.BasicAuth()
		if !ok || shopID != "shop-1" || secret != "secret-1" {
			t.Fatalf("unexpected Basic Auth: shop=%q secret=%q ok=%t", shopID, secret, ok)
		}
		if request.Header.Get("Idempotence-Key") != "payment-key" {
			t.Fatalf("unexpected idempotency key: %q", request.Header.Get("Idempotence-Key"))
		}
		var body CreatePaymentRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Amount.Value != "3500.00" ||
			body.Amount.Currency != "RUB" ||
			body.Confirmation.Type != "redirect" ||
			body.Confirmation.ReturnURL != "https://app.example.test/return" ||
			body.Metadata["rental_id"] != "rental-1" {
			t.Fatalf("unexpected payment request: %#v", body)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id":     "provider-1",
			"status": "pending",
			"paid":   false,
			"amount": map[string]string{"value": "3500.00", "currency": "RUB"},
			"confirmation": map[string]string{
				"type":             "redirect",
				"confirmation_url": "https://yookassa.test/pay/provider-1",
			},
			"metadata": map[string]string{"rental_id": "rental-1"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "shop-1", "secret-1", time.Second)
	payment, err := client.CreatePayment(
		context.Background(),
		"payment-key",
		CreatePaymentRequest{
			Amount:  Money{Value: "3500.00", Currency: "RUB"},
			Capture: true,
			Confirmation: ConfirmationRequest{
				Type:      "redirect",
				ReturnURL: "https://app.example.test/return",
			},
			Metadata: map[string]string{"rental_id": "rental-1"},
		},
	)
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if payment.ID != "provider-1" ||
		payment.Confirmation == nil ||
		payment.Confirmation.ConfirmationURL == "" {
		t.Fatalf("unexpected provider payment: %#v", payment)
	}
}

func TestGetPaymentAndTemporaryError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		calls++
		if calls == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(response).Encode(map[string]string{
				"type":        "error",
				"code":        "internal_server_error",
				"description": "try later",
			})
			return
		}
		if request.URL.Path != "/payments/provider-1" {
			http.NotFound(response, request)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id":       "provider-1",
			"status":   "succeeded",
			"paid":     true,
			"amount":   map[string]string{"value": "3500.00", "currency": "RUB"},
			"metadata": map[string]string{"rental_id": "rental-1"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "shop", "secret", time.Second)
	_, err := client.GetPayment(context.Background(), "provider-1")
	var apiError *APIError
	if !errors.As(err, &apiError) || !apiError.IsTemporary() {
		t.Fatalf("expected temporary API error, got %v", err)
	}
	payment, err := client.GetPayment(context.Background(), "provider-1")
	if err != nil || !payment.Paid || payment.Status != "succeeded" {
		t.Fatalf("unexpected fetched payment: %#v err=%v", payment, err)
	}
}

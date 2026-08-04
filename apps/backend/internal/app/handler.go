package app

import (
	"encoding/json"
	"net/http"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/catalog"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/bitrix"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/payments"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/push"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

type healthResponse struct {
	Status string `json:"status"`
}

func NewHandler(
	catalogHandler *catalog.Handler,
	authHandler *auth.Handler,
	verificationHandler *verification.Handler,
	rentalsHandler *rentals.Handler,
	bitrixEventsHandler *bitrix.EventsHandler,
	paymentsHandler *payments.Handler,
	yooKassaWebhookHandler *payments.WebhookHandler,
	pushHandler *push.Handler,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(healthResponse{Status: "ok"})
	})
	if catalogHandler != nil {
		catalogHandler.Register(mux)
	}
	if authHandler != nil {
		authHandler.Register(mux)
	}
	if authHandler != nil && verificationHandler != nil {
		verificationHandler.Register(mux, authHandler.BearerAuth)
	}
	if authHandler != nil && rentalsHandler != nil {
		rentalsHandler.Register(mux, authHandler.BearerAuth)
	}
	if bitrixEventsHandler != nil {
		bitrixEventsHandler.Register(mux)
	}
	if authHandler != nil && paymentsHandler != nil {
		paymentsHandler.Register(mux, authHandler.BearerAuth)
	}
	if yooKassaWebhookHandler != nil {
		yooKassaWebhookHandler.Register(mux)
	}
	if authHandler != nil && pushHandler != nil {
		pushHandler.Register(mux, authHandler.BearerAuth)
	}
	mux.HandleFunc("/", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "not found"})
	})

	return mux
}

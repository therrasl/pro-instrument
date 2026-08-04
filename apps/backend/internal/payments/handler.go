package payments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const maximumWebhookSize = 1 << 20

type PaymentService interface {
	Create(context.Context, string, string) (CreateResult, error)
}

type Handler struct {
	service PaymentService
	logger  *log.Logger
}

func NewHandler(service PaymentService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(
	mux *http.ServeMux,
	bearerAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/rentals/{id}/payment",
		bearerAuth(http.HandlerFunc(handler.create)),
	)
}

func (handler *Handler) create(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}

	result, err := handler.service.Create(request.Context(), client.ID, rentalID)
	switch {
	case err == nil:
		status := http.StatusOK
		if result.Created {
			status = http.StatusCreated
		}
		writeJSON(response, status, result.Payment)
	case errors.Is(err, ErrDisabled):
		writeError(response, http.StatusServiceUnavailable, "payments are disabled")
	case errors.Is(err, ErrRentalNotFound):
		writeError(response, http.StatusNotFound, "rental request not found")
	case errors.Is(err, ErrRentalNotPayable):
		writeError(response, http.StatusConflict, "rental request is not awaiting payment")
	case errors.Is(err, ErrPaymentExpired):
		writeError(response, http.StatusConflict, "payment deadline has expired")
	case errors.Is(err, ErrClientNotVerified):
		writeError(response, http.StatusForbidden, "client verification is incomplete")
	case errors.Is(err, ErrHoldNotConfirmed):
		writeError(response, http.StatusConflict, "rental hold is not confirmed or has expired")
	case errors.Is(err, ErrReceiptEmailRequired):
		writeError(response, http.StatusConflict, "email is required for fiscal receipt")
	case errors.Is(err, ErrProviderUnavailable):
		handler.logger.Printf("create YooKassa payment failed: %v", err)
		writeError(response, http.StatusBadGateway, "payment provider is unavailable")
	case errors.Is(err, ErrPaymentRequiresReview):
		writeError(response, http.StatusConflict, "payment requires reconciliation")
	default:
		handler.logger.Printf("payment request failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
}

type WebhookService interface {
	StoreWebhook(context.Context, json.RawMessage) (string, bool, error)
	ProcessEvent(context.Context, string) error
}

type WebhookHandler struct {
	service WebhookService
	logger  *log.Logger
}

func NewWebhookHandler(service WebhookService, logger *log.Logger) *WebhookHandler {
	return &WebhookHandler{service: service, logger: logger}
}

func (handler *WebhookHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST /api/v1/integrations/yookassa/webhook",
		handler.receive,
	)
}

func (handler *WebhookHandler) receive(
	response http.ResponseWriter,
	request *http.Request,
) {
	raw, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maximumWebhookSize))
	if err != nil || len(raw) == 0 || !json.Valid(raw) {
		writeError(response, http.StatusBadRequest, "invalid notification")
		return
	}
	eventID, created, err := handler.service.StoreWebhook(
		request.Context(),
		json.RawMessage(raw),
	)
	switch {
	case errors.Is(err, ErrDisabled):
		writeError(response, http.StatusServiceUnavailable, "payments are disabled")
		return
	case errors.Is(err, ErrUnsupportedEvent):
		writeError(response, http.StatusBadRequest, "unsupported notification")
		return
	case errors.Is(err, ErrWebhookMismatch):
		writeError(response, http.StatusBadRequest, "invalid notification")
		return
	case err != nil:
		handler.logger.Printf("store YooKassa webhook failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
		return
	}

	err = handler.service.ProcessEvent(request.Context(), eventID)
	switch {
	case err == nil:
		writeJSON(response, http.StatusOK, map[string]any{
			"id":        eventID,
			"duplicate": !created,
		})
	case errors.Is(err, ErrProviderUnavailable):
		handler.logger.Printf("YooKassa webhook scheduled for retry: %v", err)
		writeJSON(response, http.StatusAccepted, map[string]any{
			"id":           eventID,
			"duplicate":    !created,
			"retry_queued": true,
		})
	case errors.Is(err, ErrWebhookMismatch),
		errors.Is(err, ErrProviderMismatch),
		errors.Is(err, ErrUnsupportedEvent),
		errors.Is(err, ErrRentalNotPayable):
		writeError(response, http.StatusBadRequest, "notification verification failed")
	default:
		handler.logger.Printf("process YooKassa webhook failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(value) == nil && id.Valid
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

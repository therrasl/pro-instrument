package rentals

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const (
	maximumRequestBody = 1 << 20
	defaultListLimit   = 20
	maximumListLimit   = 100
)

type RentalService interface {
	Quote(context.Context, QuoteRequest) (Quote, error)
	Create(context.Context, string, QuoteRequest) (RentalRequest, error)
	List(context.Context, string, int, int) ([]RentalRequest, error)
	Get(context.Context, string, string) (RentalRequest, error)
	Cancel(context.Context, string, string) (RentalRequest, error)
}

type Handler struct {
	service RentalService
	logger  *log.Logger
}

func NewHandler(service RentalService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(
	mux *http.ServeMux,
	bearerAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/rentals/quote",
		bearerAuth(http.HandlerFunc(handler.quote)),
	)
	mux.Handle(
		"POST /api/v1/rentals",
		bearerAuth(http.HandlerFunc(handler.create)),
	)
	mux.Handle(
		"GET /api/v1/rentals",
		bearerAuth(http.HandlerFunc(handler.list)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}",
		bearerAuth(http.HandlerFunc(handler.get)),
	)
	mux.Handle(
		"POST /api/v1/rentals/{id}/cancel",
		bearerAuth(http.HandlerFunc(handler.cancel)),
	)
}

func (handler *Handler) quote(response http.ResponseWriter, request *http.Request) {
	_, ok := eligibleClient(request)
	if !ok {
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
		return
	}
	input, err := decodeRentalInput(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid rental request")
		return
	}

	quote, err := handler.service.Quote(request.Context(), input)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, quote)
}

func (handler *Handler) create(response http.ResponseWriter, request *http.Request) {
	client, ok := eligibleClient(request)
	if !ok {
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
		return
	}

	input, err := decodeRentalInput(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid rental request")
		return
	}

	rental, err := handler.service.Create(request.Context(), client.ID, input)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusCreated, rental)
}

func (handler *Handler) list(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset, err := listPagination(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid pagination")
		return
	}
	rentals, err := handler.service.List(request.Context(), client.ID, limit, offset)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rentals)
}

func (handler *Handler) get(response http.ResponseWriter, request *http.Request) {
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
	rental, err := handler.service.Get(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rental)
}

func (handler *Handler) cancel(response http.ResponseWriter, request *http.Request) {
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
	rental, err := handler.service.Cancel(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rental)
}

func eligibleClient(request *http.Request) (auth.Client, bool) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		return auth.Client{}, false
	}
	return client, client.Status == "verified" &&
		client.PhoneVerified &&
		client.ProfileCompleted &&
		client.OfferAccepted
}

func decodeRentalInput(response http.ResponseWriter, request *http.Request) (QuoteRequest, error) {
	var body struct {
		ToolID          string `json:"tool_id"`
		StartDate       string `json:"start_date"`
		EndDate         string `json:"end_date"`
		DeliveryMethod  string `json:"delivery_method"`
		DeliveryAddress string `json:"delivery_address"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		return QuoteRequest{}, err
	}
	if !validUUID(body.ToolID) {
		return QuoteRequest{}, ErrInvalidInput
	}
	startDate, err := time.Parse(time.DateOnly, body.StartDate)
	if err != nil {
		return QuoteRequest{}, ErrInvalidInput
	}
	endDate, err := time.Parse(time.DateOnly, body.EndDate)
	if err != nil {
		return QuoteRequest{}, ErrInvalidInput
	}
	return QuoteRequest{
		ToolID:          body.ToolID,
		StartDate:       startDate,
		EndDate:         endDate,
		DeliveryMethod:  body.DeliveryMethod,
		DeliveryAddress: body.DeliveryAddress,
	}, nil
}

func (handler *Handler) writeServiceError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid rental request")
	case errors.Is(err, ErrOnboardingIncomplete):
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
	case errors.Is(err, ErrToolNotFound):
		writeError(response, http.StatusNotFound, "tool not found")
	case errors.Is(err, ErrNoAvailableUnit):
		writeError(response, http.StatusConflict, "no tool unit is available for selected dates")
	case errors.Is(err, ErrRentalNotFound):
		writeError(response, http.StatusNotFound, "rental request not found")
	case errors.Is(err, ErrRentalNotCancellable):
		writeError(response, http.StatusConflict, "rental request cannot be cancelled")
	default:
		handler.logger.Printf("rental request failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
	return true
}

func listPagination(request *http.Request) (int, int, error) {
	limit := defaultListLimit
	offset := 0
	var err error
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maximumListLimit {
			return 0, 0, ErrInvalidInput
		}
	}
	if value := request.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			return 0, 0, ErrInvalidInput
		}
	}
	return limit, offset, nil
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maximumRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidInput
	}
	return nil
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

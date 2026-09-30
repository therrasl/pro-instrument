package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

type OrganizationService interface {
	Get(context.Context, string) (Organization, error)
	Put(context.Context, string, Input) (Organization, error)
}

type Handler struct {
	service OrganizationService
	logger  *log.Logger
}

func NewHandler(service OrganizationService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/me/organization", authenticate(http.HandlerFunc(handler.get)))
	mux.Handle("PUT /api/v1/me/organization", authenticate(http.HandlerFunc(handler.put)))
}

func (handler *Handler) get(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	organization, err := handler.service.Get(request.Context(), client.ID)
	if errors.Is(err, ErrNotFound) {
		writeError(response, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, organization)
}

func (handler *Handler) put(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	var input Input
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid organization")
		return
	}
	organization, err := handler.service.Put(request.Context(), client.ID, input)
	if errors.Is(err, ErrInvalidInput) {
		writeError(response, http.StatusBadRequest, "invalid organization")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, organization)
}

func (handler *Handler) internalError(response http.ResponseWriter, err error) {
	handler.logger.Printf("organization request failed: %v", err)
	writeError(response, http.StatusInternalServerError, "internal error")
}
func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

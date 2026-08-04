package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const maximumRequestBody = 16 << 10

type Service interface {
	Register(context.Context, string, string, string) error
	Delete(context.Context, string, string) error
}

type Handler struct {
	service Service
	logger  *log.Logger
}

func NewHandler(service Service, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(
	mux *http.ServeMux,
	bearerAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/me/push-tokens",
		bearerAuth(http.HandlerFunc(handler.registerToken)),
	)
	mux.Handle(
		"DELETE /api/v1/me/push-tokens",
		bearerAuth(http.HandlerFunc(handler.deleteToken)),
	)
}

func (handler *Handler) registerToken(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid push token")
		return
	}
	err := handler.service.Register(request.Context(), client.ID, body.Token, body.Platform)
	switch {
	case err == nil:
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidToken):
		writeError(response, http.StatusBadRequest, "invalid push token")
	default:
		handler.logger.Printf("push token registration failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
}

func (handler *Handler) deleteToken(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid push token")
		return
	}
	err := handler.service.Delete(request.Context(), client.ID, body.Token)
	switch {
	case err == nil:
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidToken):
		writeError(response, http.StatusBadRequest, "invalid push token")
	default:
		handler.logger.Printf("push token deletion failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maximumRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}

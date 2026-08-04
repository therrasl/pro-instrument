package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

const maximumRequestBody = 1 << 20

type AuthService interface {
	RequestCode(context.Context, string) error
	VerifyCode(context.Context, string, string) (Session, error)
	Authenticate(context.Context, string) (Client, error)
	UpdateProfile(context.Context, string, ProfilePatch) (Client, error)
	AcceptConsents(context.Context, string, ConsentInput) (ConsentAcceptance, error)
}

type Handler struct {
	service AuthService
	logger  *log.Logger
}

func NewHandler(service AuthService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/request-code", handler.requestCode)
	mux.HandleFunc("POST /api/v1/auth/verify-code", handler.verifyCode)
	mux.Handle("GET /api/v1/me", handler.BearerAuth(http.HandlerFunc(handler.getMe)))
	mux.Handle(
		"GET /api/v1/me/rental-eligibility",
		handler.BearerAuth(http.HandlerFunc(handler.getRentalEligibility)),
	)
	mux.Handle("PATCH /api/v1/me", handler.BearerAuth(http.HandlerFunc(handler.patchMe)))
	mux.Handle("POST /api/v1/me/consents", handler.BearerAuth(http.HandlerFunc(handler.acceptConsents)))
}

func (handler *Handler) requestCode(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid request")
		return
	}

	err := handler.service.RequestCode(request.Context(), body.Phone)
	switch {
	case err == nil:
		writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted"})
	case errors.Is(err, ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid request")
	case errors.Is(err, ErrRateLimited):
		writeError(response, http.StatusTooManyRequests, "too many requests")
	default:
		handler.internalError(response, err)
	}
}

func (handler *Handler) verifyCode(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid request")
		return
	}

	session, err := handler.service.VerifyCode(request.Context(), body.Phone, body.Code)
	if errors.Is(err, ErrInvalidCode) {
		writeError(response, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, session)
}

func (handler *Handler) getMe(response http.ResponseWriter, request *http.Request) {
	client, ok := ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(response, http.StatusOK, client)
}

func (handler *Handler) getRentalEligibility(response http.ResponseWriter, request *http.Request) {
	client, ok := ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	reason := "documents_required"
	switch client.Status {
	case "verified":
		reason = ""
	case "pending_verification":
		reason = "documents_pending"
	case "verification_rejected":
		reason = "documents_rejected"
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"eligible": client.Status == "verified",
		"reason":   reason,
	})
}

func (handler *Handler) patchMe(response http.ResponseWriter, request *http.Request) {
	client, ok := ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		FullName  *string `json:"full_name"`
		BirthDate *string `json:"birth_date"`
		Email     *string `json:"email"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid request")
		return
	}

	updated, err := handler.service.UpdateProfile(request.Context(), client.ID, ProfilePatch{
		FullName:  body.FullName,
		BirthDate: body.BirthDate,
		Email:     body.Email,
	})
	if errors.Is(err, ErrInvalidInput) {
		writeError(response, http.StatusBadRequest, "invalid profile")
		return
	}
	if errors.Is(err, ErrUnauthorized) {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, updated)
}

func (handler *Handler) acceptConsents(response http.ResponseWriter, request *http.Request) {
	client, ok := ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		OfferVersion          string `json:"offer_version"`
		PrivacyVersion        string `json:"privacy_version"`
		OfferAccepted         bool   `json:"offer_accepted"`
		PrivacyAccepted       bool   `json:"privacy_accepted"`
		DataAccuracyConfirmed bool   `json:"data_accuracy_confirmed"`
		RentalRulesAccepted   bool   `json:"rental_rules_accepted"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid request")
		return
	}

	ipAddress, err := requestIPAddress(request)
	if err != nil || len(request.UserAgent()) > 2048 {
		writeError(response, http.StatusBadRequest, "invalid request metadata")
		return
	}

	acceptance, err := handler.service.AcceptConsents(request.Context(), client.ID, ConsentInput{
		OfferVersion:          body.OfferVersion,
		PrivacyVersion:        body.PrivacyVersion,
		OfferAccepted:         body.OfferAccepted,
		PrivacyAccepted:       body.PrivacyAccepted,
		DataAccuracyConfirmed: body.DataAccuracyConfirmed,
		RentalRulesAccepted:   body.RentalRulesAccepted,
		IPAddress:             ipAddress,
		UserAgent:             request.UserAgent(),
	})
	if errors.Is(err, ErrInvalidInput) {
		writeError(response, http.StatusBadRequest, "all consents are required")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusCreated, acceptance)
}

func (handler *Handler) BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(response, http.StatusUnauthorized, "unauthorized")
			return
		}

		client, err := handler.service.Authenticate(request.Context(), parts[1])
		if errors.Is(err, ErrUnauthorized) {
			writeError(response, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			handler.internalError(response, err)
			return
		}

		next.ServeHTTP(response, request.WithContext(withClient(request.Context(), client)))
	})
}

func (handler *Handler) internalError(response http.ResponseWriter, err error) {
	handler.logger.Printf("auth request failed: %v", err)
	writeError(response, http.StatusInternalServerError, "internal server error")
}

type clientContextKey struct{}

func withClient(ctx context.Context, client Client) context.Context {
	return context.WithValue(ctx, clientContextKey{}, client)
}

func ClientFromContext(ctx context.Context) (Client, bool) {
	client, ok := ctx.Value(clientContextKey{}).(Client)
	return client, ok
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

func requestIPAddress(request *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	if net.ParseIP(host) == nil {
		return "", errors.New("invalid remote address")
	}
	return host, nil
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

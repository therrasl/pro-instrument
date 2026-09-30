package bitrix

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

type VerificationWebhookHandler struct {
	repository   WorkerRepository
	reviewer     VerificationReviewer
	pushNotifier VerificationPushNotifier
	enabled      bool
	secret       string
	logger       *log.Logger
}

func NewVerificationWebhookHandler(
	repository WorkerRepository,
	reviewer VerificationReviewer,
	pushNotifier VerificationPushNotifier,
	enabled bool,
	secret string,
	logger *log.Logger,
) *VerificationWebhookHandler {
	return &VerificationWebhookHandler{
		repository:   repository,
		reviewer:     reviewer,
		pushNotifier: pushNotifier,
		enabled:      enabled,
		secret:       secret,
		logger:       logger,
	}
}

func (handler *VerificationWebhookHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/integrations/bitrix/verification", handler.handleVerification)
}

func (handler *VerificationWebhookHandler) handleVerification(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.enabled {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "Bitrix integration is disabled",
		})
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)

	var contactID string
	var phone string
	var rawDecision string
	var reason string

	contentType := request.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		var payload struct {
			ContactID string `json:"contact_id"`
			Phone     string `json:"phone"`
			Decision  string `json:"decision"`
			Reason    string `json:"reason"`
			AuthToken string `json:"auth_token"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		contactID = payload.ContactID
		phone = payload.Phone
		rawDecision = payload.Decision
		reason = payload.Reason
		if !validAnyWebhookSecret(handler.secret, request.Header.Get(bitrixWebhookSecretHeader), payload.AuthToken) {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
	} else {
		if err := request.ParseForm(); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid form data"})
			return
		}
		if !validAnyWebhookSecret(
			handler.secret,
			request.Header.Get(bitrixWebhookSecretHeader),
			request.PostForm.Get("auth[application_token]"),
			request.PostForm.Get("auth_token"),
		) {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		contactID = firstNonEmpty(
			request.PostForm.Get("contact_id"),
			request.PostForm.Get("data[FIELDS][ID]"),
			request.PostForm.Get("data[fields][ID]"),
		)
		phone = request.PostForm.Get("phone")
		rawDecision = firstNonEmpty(
			request.PostForm.Get("decision"),
			request.PostForm.Get("status"),
			request.PostForm.Get("UF_CRM_1786619870585"),
		)
		reason = request.PostForm.Get("reason")
	}

	contactID = strings.TrimSpace(contactID)
	phone = strings.TrimSpace(phone)
	if contactID == "" && phone == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "contact_id or phone is required",
		})
		return
	}

	decision := "approved"
	normalized := strings.ToLower(strings.TrimSpace(rawDecision))
	switch normalized {
	case "rejected", "reject", "отклонено", "нет", "302", "false", "0":
		decision = "rejected"
	case "approved", "approve", "одобрено", "да", "300", "true", "1":
		decision = "approved"
	default:
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid decision: must be approved or rejected",
		})
		return
	}

	clientID, err := handler.repository.FindClientIDByBitrixContact(request.Context(), contactID, phone)
	if err != nil {
		handler.logger.Printf("find client by contact %s error: %v", contactID, err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if clientID == "" {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "client not found"})
		return
	}

	if decision == "approved" {
		if handler.reviewer != nil {
			_, err = handler.reviewer.Approve(request.Context(), clientID, "bitrix_manager")
			if err != nil && err != verification.ErrReviewNotAllowed {
				handler.logger.Printf("approve client %s error: %v", clientID, err)
				writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "approve error"})
				return
			}
		}
		if handler.pushNotifier != nil {
			_ = handler.pushNotifier.NotifyVerificationApproved(request.Context(), clientID)
		}
		handler.logger.Printf("Direct verification webhook: client %s approved", clientID)
	} else {
		trimmedReason := strings.TrimSpace(reason)
		if trimmedReason == "" {
			trimmedReason = "Фотографии документов не соответствуют требованиям к качеству"
		}
		if handler.reviewer != nil {
			_, err = handler.reviewer.Reject(request.Context(), clientID, "bitrix_manager", trimmedReason)
			if err != nil && err != verification.ErrReviewNotAllowed {
				handler.logger.Printf("reject client %s error: %v", clientID, err)
				writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "reject error"})
				return
			}
		}
		if handler.pushNotifier != nil {
			_ = handler.pushNotifier.NotifyVerificationRejected(request.Context(), clientID, trimmedReason)
		}
		handler.logger.Printf("Direct verification webhook: client %s rejected (reason: %s)", clientID, trimmedReason)
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"status":    "ok",
		"client_id": clientID,
		"decision":  decision,
	})
}

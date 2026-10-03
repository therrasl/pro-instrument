package bitrix

import (
	"crypto/hmac"
	"encoding/json"
	"fmt"
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
	bitrixClient Client
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

func (handler *VerificationWebhookHandler) SetBitrixClient(client Client) *VerificationWebhookHandler {
	handler.bitrixClient = client
	return handler
}

func (handler *VerificationWebhookHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/integrations/bitrix/verification", handler.handleVerification)
	mux.HandleFunc("GET /api/v1/integrations/bitrix/quick-review", handler.handleQuickReview)
	mux.HandleFunc("POST /api/v1/integrations/bitrix/quick-review", handler.handleQuickReview)
}

func (handler *VerificationWebhookHandler) handleQuickReview(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.enabled {
		http.Error(response, "Bitrix integration is disabled", http.StatusServiceUnavailable)
		return
	}

	clientID := strings.TrimSpace(request.URL.Query().Get("client_id"))
	decision := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("decision")))
	token := strings.TrimSpace(request.URL.Query().Get("token"))
	reason := strings.TrimSpace(request.URL.Query().Get("reason"))
	if request.Method == http.MethodPost {
		_ = request.ParseForm()
		if r := request.PostForm.Get("reason"); r != "" {
			reason = strings.TrimSpace(r)
		}
	}

	if clientID == "" || (decision != "approved" && decision != "rejected") || token == "" {
		http.Error(response, "Неверные параметры запроса", http.StatusBadRequest)
		return
	}

	expectedToken := GenerateQuickReviewToken(handler.secret, clientID, decision)
	if !hmac.Equal([]byte(token), []byte(expectedToken)) {
		http.Error(response, "Недействительная или устаревшая ссылка проверки", http.StatusForbidden)
		return
	}

	if decision == "rejected" && reason == "" {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Отклонение верификации</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 1rem; box-sizing: border-box; }
.card { background: #fff; padding: 2rem; border-radius: 1rem; box-shadow: 0 10px 25px rgba(0,0,0,0.06); max-width: 480px; width: 100%; }
h2 { margin-top: 0; color: #0f172a; font-size: 1.35rem; }
p { color: #64748b; font-size: 0.95rem; line-height: 1.4; }
textarea { width: 100%; box-sizing: border-box; padding: 0.75rem; border: 1px solid #cbd5e1; border-radius: 0.5rem; font-size: 0.95rem; min-height: 90px; margin: 1rem 0; font-family: inherit; }
button { width: 100%; padding: 0.85rem; background: #ef4444; color: #fff; border: none; border-radius: 0.5rem; font-size: 1rem; font-weight: 600; cursor: pointer; transition: background 0.15s; }
button:hover { background: #dc2626; }
</style>
</head>
<body>
<div class="card">
  <h2>❌ Отклонить верификацию клиента</h2>
  <p>Укажите причину для клиента (она отобразится в мобильном приложении и Push-уведомлении):</p>
  <form method="POST">
    <textarea name="reason" placeholder="Например: Размыто фото разворота паспорта, не виден номер..." required autofocus></textarea>
    <button type="submit">Отклонить документы</button>
  </form>
</div>
</body>
</html>`))
		return
	}

	syncData, _ := handler.repository.GetClientSyncData(request.Context(), clientID)
	clientName := syncData.FullName
	if clientName == "" {
		clientName = syncData.Phone
	}

	if decision == "approved" {
		if handler.reviewer != nil {
			_, err := handler.reviewer.Approve(request.Context(), clientID, "bitrix_manager")
			if err != nil && err != verification.ErrReviewNotAllowed {
				handler.logger.Printf("quick-review approve client %s error: %v", clientID, err)
				http.Error(response, "Ошибка при одобрении верификации", http.StatusInternalServerError)
				return
			}
		}
		if handler.pushNotifier != nil {
			_ = handler.pushNotifier.NotifyVerificationApproved(request.Context(), clientID)
		}
		if handler.bitrixClient != nil && syncData.ContactID != "" {
			_ = handler.bitrixClient.AddContactComment(request.Context(), syncData.ContactID, "✅ Документы клиента ОДОБРЕНЫ менеджером в 1 клик.")
		}

		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Верификация одобрена</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f0fdf4; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 1rem; box-sizing: border-box; }
.card { background: #fff; padding: 2.5rem; border-radius: 1rem; box-shadow: 0 10px 25px rgba(0,0,0,0.05); max-width: 480px; width: 100%%; text-align: center; }
.icon { font-size: 3.5rem; margin-bottom: 0.5rem; }
h2 { color: #15803d; margin: 0 0 0.75rem 0; font-size: 1.4rem; }
p { color: #475569; font-size: 0.95rem; line-height: 1.5; margin: 0.5rem 0; }
.badge { display: inline-block; background: #dcfce7; color: #166534; padding: 0.35rem 0.85rem; border-radius: 9999px; font-weight: 600; font-size: 0.85rem; margin-top: 1rem; }
</style>
</head>
<body>
<div class="card">
  <div class="icon">✅</div>
  <h2>Документы одобрены</h2>
  <p>Клиент <b>%s</b> успешно верифицирован.</p>
  <p>В мобильном приложении отправлено уведомление, аренда и оплата разблокированы.</p>
  <span class="badge">Статус в CRM: Проверен</span>
</div>
</body>
</html>`, clientName)))
		return
	}

	if reason == "" {
		reason = "Фотографии документов не соответствуют требованиям к качеству"
	}
	if handler.reviewer != nil {
		_, err := handler.reviewer.Reject(request.Context(), clientID, "bitrix_manager", reason)
		if err != nil && err != verification.ErrReviewNotAllowed {
			handler.logger.Printf("quick-review reject client %s error: %v", clientID, err)
			http.Error(response, "Ошибка при отклонении верификации", http.StatusInternalServerError)
			return
		}
	}
	if handler.pushNotifier != nil {
		_ = handler.pushNotifier.NotifyVerificationRejected(request.Context(), clientID, reason)
	}
	if handler.bitrixClient != nil && syncData.ContactID != "" {
		_ = handler.bitrixClient.AddContactComment(request.Context(), syncData.ContactID, fmt.Sprintf("❌ Документы клиента ОТКЛОНЕНЫ менеджером в 1 клик.\nПричина: %s", reason))
	}

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Верификация отклонена</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #fef2f2; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 1rem; box-sizing: border-box; }
.card { background: #fff; padding: 2.5rem; border-radius: 1rem; box-shadow: 0 10px 25px rgba(0,0,0,0.05); max-width: 480px; width: 100%%; text-align: center; }
.icon { font-size: 3.5rem; margin-bottom: 0.5rem; }
h2 { color: #b91c1c; margin: 0 0 0.75rem 0; font-size: 1.4rem; }
p { color: #475569; font-size: 0.95rem; line-height: 1.5; margin: 0.5rem 0; }
.reason-box { background: #fff1f2; border: 1px solid #fecdd3; border-radius: 0.5rem; padding: 0.75rem 1rem; margin: 1rem 0; color: #9f1239; font-size: 0.9rem; text-align: left; }
</style>
</head>
<body>
<div class="card">
  <div class="icon">❌</div>
  <h2>Документы отклонены</h2>
  <p>Верификация клиента <b>%s</b> отклонена.</p>
  <div class="reason-box"><b>Причина:</b> %s</div>
  <p>Клиент получил уведомление с инструкцией и может повторно загрузить фотографии в приложении.</p>
</div>
</body>
</html>`, clientName, reason)))
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
			Secret    string `json:"secret"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		contactID = payload.ContactID
		phone = payload.Phone
		rawDecision = payload.Decision
		reason = payload.Reason
		token := firstNonEmpty(payload.AuthToken, payload.Secret)
		if !validAnyWebhookSecret(handler.secret, request.Header.Get(bitrixWebhookSecretHeader), token) {
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

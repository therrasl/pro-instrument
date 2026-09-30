package bitrix

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	bitrixWebhookSecretHeader = "X-Bitrix-Webhook-Secret"
	bitrixEventIDHeader       = "X-Bitrix-Event-ID"
	maximumBitrixEventSize    = 1 << 20
)

type EventRepository interface {
	StoreInboundEvent(
		context.Context,
		string,
		string,
		json.RawMessage,
		time.Time,
	) (string, bool, error)
}

type EventsHandler struct {
	repository EventRepository
	enabled    bool
	secret              string
	logger              *log.Logger
	now                 func() time.Time
	verificationHandler *VerificationWebhookHandler
}

func NewEventsHandler(
	repository EventRepository,
	enabled bool,
	secret string,
	logger *log.Logger,
) *EventsHandler {
	return &EventsHandler{
		repository: repository,
		enabled:    enabled,
		secret:     secret,
		logger:     logger,
		now:        time.Now,
	}
}

func (handler *EventsHandler) SetVerificationHandler(v *VerificationWebhookHandler) *EventsHandler {
	handler.verificationHandler = v
	return handler
}

func (handler *EventsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/integrations/bitrix/events", handler.receive)
	if handler.verificationHandler != nil {
		handler.verificationHandler.Register(mux)
	}
}

func (handler *EventsHandler) receive(response http.ResponseWriter, request *http.Request) {
	if !handler.enabled {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "Bitrix integration is disabled",
		})
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maximumBitrixEventSize)
	if err := request.ParseForm(); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid event"})
		return
	}
	if !validAnyWebhookSecret(
		handler.secret,
		request.Header.Get(bitrixWebhookSecretHeader),
		request.PostForm.Get("auth[application_token]"),
	) {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	dealID, bodyEventID, payload, err := decodeInboundRequest(request)
	if err != nil || strings.TrimSpace(dealID) == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid event"})
		return
	}

	eventKey := strings.TrimSpace(request.Header.Get(bitrixEventIDHeader))
	if eventKey == "" {
		eventKey = strings.TrimSpace(bodyEventID)
	}
	if eventKey == "" {
		hash := sha256.Sum256(append([]byte(dealID+":"), payload...))
		eventKey = "sha256:" + strings.ToLower(hexString(hash[:]))
	}
	if len(eventKey) > 512 || len(dealID) > 128 {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid event"})
		return
	}

	eventID, created, err := handler.repository.StoreInboundEvent(
		request.Context(),
		eventKey,
		dealID,
		payload,
		handler.now().UTC(),
	)
	if err != nil {
		handler.logger.Printf("store Bitrix event failed: %v", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "internal server error",
		})
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{
		"id":        eventID,
		"duplicate": !created,
	})
}

func decodeInboundRequest(
	request *http.Request,
) (string, string, json.RawMessage, error) {
	mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaType == "application/x-www-form-urlencoded" {
		return decodeInboundForm(request.PostForm)
	}

	rawBody, err := io.ReadAll(request.Body)
	if err != nil || len(rawBody) == 0 {
		return "", "", nil, errors.New("empty Bitrix event body")
	}
	return decodeInboundJSON(rawBody)
}

func decodeInboundForm(
	values url.Values,
) (string, string, json.RawMessage, error) {
	dealID := firstNonEmpty(
		values.Get("deal_id"),
		values.Get("contact_id"),
		values.Get("data[FIELDS][ID]"),
		values.Get("data[fields][ID]"),
		values.Get("data[FIELDS][id]"),
	)
	eventID := values.Get("event_id")

	sanitized := make(url.Values, len(values))
	for key, entries := range values {
		if key == "auth[application_token]" {
			continue
		}
		sanitized[key] = append([]string(nil), entries...)
	}
	payload, err := json.Marshal(sanitized)
	return dealID, eventID, payload, err
}

func decodeInboundJSON(
	rawBody []byte,
) (string, string, json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(rawBody, &value); err != nil {
		return "", "", nil, err
	}
	dealID := stringField(value, "deal_id")
	if dealID == "" {
		dealID = stringField(value, "contact_id")
	}
	if dealID == "" {
		dealID = nestedStringField(value, "data", "FIELDS", "ID")
	}
	if dealID == "" {
		dealID = nestedStringField(value, "data", "fields", "id")
	}
	eventID := stringField(value, "event_id")
	return dealID, eventID, json.RawMessage(rawBody), nil
}

func validAnyWebhookSecret(expected string, provided ...string) bool {
	valid := false
	for _, value := range provided {
		if validWebhookSecret(value, expected) {
			valid = true
		}
	}
	return valid
}

func validWebhookSecret(provided string, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return provided != "" &&
		expected != "" &&
		subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func stringField(value map[string]any, key string) string {
	raw, ok := value[key]
	if !ok {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func nestedStringField(value map[string]any, keys ...string) string {
	var current any = value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[key]
		if !ok {
			return ""
		}
	}
	switch typed := current.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func hexString(value []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, current := range value {
		result[index*2] = alphabet[current>>4]
		result[index*2+1] = alphabet[current&0x0f]
	}
	return string(result)
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

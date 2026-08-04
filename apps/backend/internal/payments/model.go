package payments

import (
	"encoding/json"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
)

const (
	StatusCreating       = "creating"
	StatusPending        = "pending"
	StatusSucceeded      = "succeeded"
	StatusCanceled       = "canceled"
	StatusExpired        = "expired"
	StatusRequiresReview = "requires_review"
	StatusFailed         = "failed"

	EventPaymentSucceeded = "payment.succeeded"
	EventPaymentCanceled  = "payment.canceled"
)

type Payment struct {
	ID                string          `json:"id"`
	RentalRequestID   string          `json:"rental_request_id"`
	ProviderPaymentID *string         `json:"provider_payment_id"`
	IdempotencyKey    string          `json:"-"`
	RentalAmount      int64           `json:"rental_amount"`
	DepositAmount     int64           `json:"deposit_amount"`
	DeliveryAmount    int64           `json:"delivery_amount"`
	TotalAmount       int64           `json:"total_amount"`
	Currency          string          `json:"currency"`
	Status            string          `json:"status"`
	ConfirmationURL   *string         `json:"confirmation_url"`
	ProviderPayload   json.RawMessage `json:"-"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	SucceededAt       *time.Time      `json:"succeeded_at,omitempty"`
	CancelledAt       *time.Time      `json:"cancelled_at,omitempty"`
}

type CreateData struct {
	ClientEmail  string
	ToolName     string
	ReceiptItems []yookassa.ReceiptItem
}

type PaymentEvent struct {
	ID                string
	EventKey          string
	ProviderPaymentID string
	EventType         string
	RawPayload        json.RawMessage
	Attempts          int
}

type CreateResult struct {
	Payment Payment
	Created bool
}

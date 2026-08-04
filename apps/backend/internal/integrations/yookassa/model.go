package yookassa

import (
	"encoding/json"
	"fmt"
)

const DefaultBaseURL = "https://api.yookassa.ru/v3"

type Money struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type ConfirmationRequest struct {
	Type      string `json:"type"`
	ReturnURL string `json:"return_url"`
}

type Confirmation struct {
	Type            string `json:"type"`
	ConfirmationURL string `json:"confirmation_url"`
}

type ReceiptCustomer struct {
	Email string `json:"email"`
}

type ReceiptItem struct {
	Description    string `json:"description"`
	Quantity       string `json:"quantity"`
	Amount         Money  `json:"amount"`
	VATCode        int    `json:"vat_code"`
	PaymentMode    string `json:"payment_mode"`
	PaymentSubject string `json:"payment_subject"`
}

type Receipt struct {
	Customer ReceiptCustomer `json:"customer"`
	Items    []ReceiptItem   `json:"items"`
}

type CreatePaymentRequest struct {
	Amount       Money               `json:"amount"`
	Capture      bool                `json:"capture"`
	Confirmation ConfirmationRequest `json:"confirmation"`
	Description  string              `json:"description"`
	Metadata     map[string]string   `json:"metadata"`
	Receipt      *Receipt            `json:"receipt,omitempty"`
}

type CancellationDetails struct {
	Party  string `json:"party"`
	Reason string `json:"reason"`
}

type Payment struct {
	ID                  string               `json:"id"`
	Status              string               `json:"status"`
	Paid                bool                 `json:"paid"`
	Amount              Money                `json:"amount"`
	Confirmation        *Confirmation        `json:"confirmation,omitempty"`
	Metadata            map[string]string    `json:"metadata"`
	ReceiptRegistration string               `json:"receipt_registration,omitempty"`
	CancellationDetails *CancellationDetails `json:"cancellation_details,omitempty"`
	Raw                 json.RawMessage      `json:"-"`
}

type APIError struct {
	StatusCode  int
	Code        string
	Description string
	Temporary   bool
}

func (err *APIError) Error() string {
	if err.Code == "" {
		return fmt.Sprintf("YooKassa HTTP %d: %s", err.StatusCode, err.Description)
	}
	return fmt.Sprintf(
		"YooKassa HTTP %d (%s): %s",
		err.StatusCode,
		err.Code,
		err.Description,
	)
}

func (err *APIError) IsTemporary() bool {
	return err.Temporary
}

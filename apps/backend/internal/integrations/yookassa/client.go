package yookassa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maximumResponseSize = 4 << 20

type Client interface {
	CreatePayment(context.Context, string, CreatePaymentRequest) (Payment, error)
	GetPayment(context.Context, string) (Payment, error)
	CreateRefund(context.Context, string, CreateRefundRequest) (Refund, error)
	GetRefund(context.Context, string) (Refund, error)
}

type HTTPClient struct {
	baseURL   string
	shopID    string
	secretKey string
	client    *http.Client
}

func NewHTTPClient(
	baseURL string,
	shopID string,
	secretKey string,
	timeout time.Duration,
) *HTTPClient {
	return &HTTPClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		shopID:    shopID,
		secretKey: secretKey,
		client:    &http.Client{Timeout: timeout},
	}
}

func (client *HTTPClient) CreatePayment(
	ctx context.Context,
	idempotencyKey string,
	input CreatePaymentRequest,
) (Payment, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return Payment{}, fmt.Errorf("encode YooKassa payment: %w", err)
	}
	return client.call(
		ctx,
		http.MethodPost,
		"/payments",
		idempotencyKey,
		bytes.NewReader(body),
	)
}

func (client *HTTPClient) GetPayment(
	ctx context.Context,
	paymentID string,
) (Payment, error) {
	return client.call(
		ctx,
		http.MethodGet,
		"/payments/"+url.PathEscape(paymentID),
		"",
		nil,
	)
}

func (client *HTTPClient) CreateRefund(
	ctx context.Context,
	idempotencyKey string,
	input CreateRefundRequest,
) (Refund, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return Refund{}, fmt.Errorf("encode YooKassa refund: %w", err)
	}
	return client.callRefund(
		ctx,
		http.MethodPost,
		"/refunds",
		idempotencyKey,
		bytes.NewReader(body),
	)
}

func (client *HTTPClient) GetRefund(
	ctx context.Context,
	refundID string,
) (Refund, error) {
	return client.callRefund(
		ctx,
		http.MethodGet,
		"/refunds/"+url.PathEscape(refundID),
		"",
		nil,
	)
}

func (client *HTTPClient) call(
	ctx context.Context,
	method string,
	path string,
	idempotencyKey string,
	body io.Reader,
) (Payment, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return Payment{}, fmt.Errorf("create YooKassa request: %w", err)
	}
	request.SetBasicAuth(client.shopID, client.secretKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotence-Key", idempotencyKey)
	}

	response, err := client.client.Do(request)
	if err != nil {
		return Payment{}, fmt.Errorf("call YooKassa: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseSize+1))
	if err != nil {
		return Payment{}, fmt.Errorf("read YooKassa response: %w", err)
	}
	if len(responseBody) > maximumResponseSize {
		return Payment{}, errors.New("YooKassa response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Payment{}, decodeAPIError(response.StatusCode, responseBody)
	}

	var payment Payment
	if err := json.Unmarshal(responseBody, &payment); err != nil {
		return Payment{}, fmt.Errorf("decode YooKassa payment: %w", err)
	}
	if payment.ID == "" || payment.Status == "" {
		return Payment{}, errors.New("YooKassa payment response is incomplete")
	}
	payment.Raw = append(json.RawMessage(nil), responseBody...)
	return payment, nil
}

func (client *HTTPClient) callRefund(
	ctx context.Context,
	method string,
	path string,
	idempotencyKey string,
	body io.Reader,
) (Refund, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return Refund{}, fmt.Errorf("create YooKassa refund request: %w", err)
	}
	request.SetBasicAuth(client.shopID, client.secretKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotence-Key", idempotencyKey)
	}

	response, err := client.client.Do(request)
	if err != nil {
		return Refund{}, fmt.Errorf("call YooKassa refund: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseSize+1))
	if err != nil {
		return Refund{}, fmt.Errorf("read YooKassa response: %w", err)
	}
	if len(responseBody) > maximumResponseSize {
		return Refund{}, errors.New("YooKassa response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Refund{}, decodeAPIError(response.StatusCode, responseBody)
	}

	var refund Refund
	if err := json.Unmarshal(responseBody, &refund); err != nil {
		return Refund{}, fmt.Errorf("decode YooKassa refund: %w", err)
	}
	if refund.ID == "" || refund.Status == "" {
		return Refund{}, errors.New("YooKassa refund response is incomplete")
	}
	refund.Raw = append(json.RawMessage(nil), responseBody...)
	return refund, nil
}

func decodeAPIError(status int, body []byte) error {
	var envelope struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &envelope)
	description := envelope.Description
	if description == "" {
		description = http.StatusText(status)
	}
	return &APIError{
		StatusCode:  status,
		Code:        envelope.Code,
		Description: description,
		Temporary: status == http.StatusRequestTimeout ||
			status == http.StatusTooManyRequests ||
			status >= http.StatusInternalServerError,
	}
}

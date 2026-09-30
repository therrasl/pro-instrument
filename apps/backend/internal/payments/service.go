package payments

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
)

var (
	ErrDisabled              = errors.New("YooKassa integration is disabled")
	ErrRentalNotFound        = errors.New("rental request not found")
	ErrRentalNotPayable      = errors.New("rental request is not awaiting payment")
	ErrPaymentExpired        = errors.New("rental payment deadline has expired")
	ErrClientNotVerified     = errors.New("client verification is incomplete")
	ErrHoldNotConfirmed      = errors.New("rental hold is not confirmed and active")
	ErrReceiptEmailRequired  = errors.New("client email is required for fiscal receipt")
	ErrProviderUnavailable   = errors.New("payment provider is unavailable")
	ErrProviderMismatch      = errors.New("payment provider response does not match payment")
	ErrPaymentRequiresReview = errors.New("payment requires manual reconciliation")
	ErrUnsupportedEvent      = errors.New("unsupported payment event")
	ErrWebhookMismatch       = errors.New("payment webhook does not match stored payment")
	ErrDepositNotPaid       = errors.New("deposit is not paid")
	ErrNoRefundableAmount   = errors.New("no refundable deposit amount remaining")
	ErrRefundExceedsDeposit = errors.New("refund amount exceeds refundable deposit")
)

type Repository interface {
	PreparePayment(
		context.Context,
		string,
		string,
		bool,
		time.Time,
	) (Payment, CreateData, bool, error)
	SaveProviderPayment(
		context.Context,
		string,
		yookassa.Payment,
		time.Time,
	) (Payment, error)
	MarkCreationForReview(context.Context, string, string, time.Time) error
	StoreEvent(
		context.Context,
		string,
		string,
		string,
		json.RawMessage,
		time.Time,
	) (string, bool, error)
	ClaimEventByID(context.Context, string, time.Time) (PaymentEvent, bool, error)
	ClaimNextEvent(context.Context, time.Time, time.Time, int) (PaymentEvent, bool, error)
	GetByProviderID(context.Context, string) (Payment, error)
	CompleteSucceeded(
		context.Context,
		PaymentEvent,
		yookassa.Payment,
		string,
		time.Time,
	) error
	CompleteCanceled(
		context.Context,
		PaymentEvent,
		yookassa.Payment,
		bool,
		string,
		time.Time,
	) error
	RetryEvent(context.Context, string, time.Time, string) error
	RejectEvent(context.Context, string, string, time.Time) error
	GetDepositForRefund(context.Context, string) (DepositRefundData, error)
	RecordDepositRefund(context.Context, string, int64, string, json.RawMessage, time.Time) error
	PrepareExtensionPayment(
		context.Context,
		string,
		string,
		string,
		int64,
		bool,
		time.Time,
	) (Payment, CreateData, error)
}

type Service struct {
	repository      Repository
	provider        yookassa.Client
	enabled         bool
	returnURL       string
	receiptsEnabled bool
	taxSystemCode   *int
	vatCode         int
	retryBase       time.Duration
	maxAttempts     int
	logger          *log.Logger
	now             func() time.Time
}

func NewService(
	repository Repository,
	provider yookassa.Client,
	enabled bool,
	returnURL string,
	receiptsEnabled bool,
	retryBase time.Duration,
	maxAttempts int,
	logger *log.Logger,
) *Service {
	defaultTaxSystem := 2
	return &Service{
		repository:      repository,
		provider:        provider,
		enabled:         enabled,
		returnURL:       returnURL,
		receiptsEnabled: receiptsEnabled,
		taxSystemCode:   &defaultTaxSystem,
		vatCode:         1,
		retryBase:       retryBase,
		maxAttempts:     maxAttempts,
		logger:          logger,
		now:             time.Now,
	}
}

func (service *Service) SetFiscalParameters(taxSystemCode int, vatCode int) *Service {
	if taxSystemCode > 0 {
		code := taxSystemCode
		service.taxSystemCode = &code
	}
	if vatCode > 0 {
		service.vatCode = vatCode
	}
	return service
}

func (service *Service) Create(
	ctx context.Context,
	clientID string,
	rentalID string,
) (CreateResult, error) {
	if !service.enabled {
		return CreateResult{}, ErrDisabled
	}
	now := service.now().UTC()
	payment, data, created, err := service.repository.PreparePayment(
		ctx,
		clientID,
		rentalID,
		service.receiptsEnabled,
		now,
	)
	if err != nil {
		return CreateResult{}, err
	}
	if payment.Status == StatusRequiresReview ||
		(payment.Status == StatusCreating && now.Sub(payment.CreatedAt) >= 23*time.Hour) {
		return CreateResult{}, ErrPaymentRequiresReview
	}
	if payment.ProviderPaymentID != nil &&
		payment.ConfirmationURL != nil &&
		payment.Status != StatusCreating {
		return CreateResult{Payment: payment, Created: false}, nil
	}

	input := yookassa.CreatePaymentRequest{
		Amount: yookassa.Money{
			Value:    formatKopecks(payment.TotalAmount),
			Currency: payment.Currency,
		},
		Capture: true,
		Confirmation: yookassa.ConfirmationRequest{
			Type:      "redirect",
			ReturnURL: service.returnURL,
		},
		Description: "Оплата заявки аренды " + payment.RentalRequestID,
		Metadata: map[string]string{
			"rental_id":  payment.RentalRequestID,
			"payment_id": payment.ID,
		},
	}
	if service.receiptsEnabled {
		vatCode := service.vatCode
		if vatCode <= 0 {
			vatCode = 1
		}
		items := make([]yookassa.ReceiptItem, len(data.ReceiptItems))
		for i, item := range data.ReceiptItems {
			item.VATCode = vatCode
			items[i] = item
		}
		input.Receipt = &yookassa.Receipt{
			Customer: yookassa.ReceiptCustomer{
				Email: data.ClientEmail,
				Phone: data.ClientPhone,
			},
			TaxSystemCode: service.taxSystemCode,
			Items:         items,
		}
	}

	providerPayment, err := service.provider.CreatePayment(
		ctx,
		payment.IdempotencyKey,
		input,
	)
	if err != nil {
		if !isTemporary(err) {
			_ = service.repository.MarkCreationForReview(
				ctx,
				payment.ID,
				err.Error(),
				now,
			)
			return CreateResult{}, fmt.Errorf("%w: %v", ErrPaymentRequiresReview, err)
		}
		return CreateResult{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	if err := validateProviderPayment(payment, providerPayment, false); err != nil {
		_ = service.repository.MarkCreationForReview(
			ctx,
			payment.ID,
			err.Error(),
			now,
		)
		return CreateResult{}, err
	}
	if providerPayment.Confirmation == nil ||
		strings.TrimSpace(providerPayment.Confirmation.ConfirmationURL) == "" {
		err := fmt.Errorf("%w: redirect confirmation URL is missing", ErrProviderMismatch)
		_ = service.repository.MarkCreationForReview(ctx, payment.ID, err.Error(), now)
		return CreateResult{}, err
	}

	saved, err := service.repository.SaveProviderPayment(ctx, payment.ID, providerPayment, now)
	if err != nil {
		return CreateResult{}, err
	}
	if !service.receiptsEnabled {
		service.logger.Printf(
			"YooKassa fiscal receipt disabled payment_id=%s rental_id=%s",
			saved.ID,
			saved.RentalRequestID,
		)
	}
	return CreateResult{Payment: saved, Created: created}, nil
}

func (service *Service) CreateExtensionPayment(
	ctx context.Context,
	clientID string,
	rentalID string,
	extensionID string,
	amount int64,
	description string,
) (string, string, error) {
	if !service.enabled {
		return "", "", ErrDisabled
	}
	now := service.now().UTC()
	payment, data, err := service.repository.PrepareExtensionPayment(
		ctx,
		clientID,
		rentalID,
		extensionID,
		amount,
		service.receiptsEnabled,
		now,
	)
	if err != nil {
		return "", "", err
	}
	if payment.ProviderPaymentID != nil && payment.ConfirmationURL != nil && payment.Status != StatusCreating {
		return payment.ID, *payment.ConfirmationURL, nil
	}

	input := yookassa.CreatePaymentRequest{
		Amount: yookassa.Money{
			Value:    formatKopecks(amount),
			Currency: "RUB",
		},
		Capture: true,
		Confirmation: yookassa.ConfirmationRequest{
			Type:      "redirect",
			ReturnURL: service.returnURL,
		},
		Description: description,
		Metadata: map[string]string{
			"rental_id":    rentalID,
			"extension_id": extensionID,
			"payment_type": "extension",
			"payment_id":   payment.ID,
		},
	}
	if service.receiptsEnabled {
		vatCode := service.vatCode
		if vatCode <= 0 {
			vatCode = 1
		}
		items := make([]yookassa.ReceiptItem, len(data.ReceiptItems))
		for i, item := range data.ReceiptItems {
			item.VATCode = vatCode
			items[i] = item
		}
		input.Receipt = &yookassa.Receipt{
			Customer: yookassa.ReceiptCustomer{
				Email: data.ClientEmail,
				Phone: data.ClientPhone,
			},
			TaxSystemCode: service.taxSystemCode,
			Items:         items,
		}
	}

	providerPayment, err := service.provider.CreatePayment(
		ctx,
		payment.IdempotencyKey,
		input,
	)
	if err != nil {
		_ = service.repository.MarkCreationForReview(ctx, payment.ID, err.Error(), now)
		return "", "", fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}

	saved, err := service.repository.SaveProviderPayment(ctx, payment.ID, providerPayment, now)
	if err != nil {
		return "", "", err
	}
	confURL := ""
	if saved.ConfirmationURL != nil {
		confURL = *saved.ConfirmationURL
	}
	return saved.ID, confURL, nil
}

func (service *Service) RefundDeposit(
	ctx context.Context,
	rentalID string,
	amount *int64,
	reason string,
) (RefundResult, error) {
	if !service.enabled {
		return RefundResult{}, ErrDisabled
	}
	now := service.now().UTC()

	deposit, err := service.repository.GetDepositForRefund(ctx, rentalID)
	if err != nil {
		return RefundResult{}, err
	}

	if deposit.DepositStatus == "refunded" || deposit.RefundableAmount <= 0 {
		return RefundResult{
			DepositID:  deposit.DepositID,
			PaymentID:  deposit.PaymentID,
			Amount:     0,
			Status:     "already_refunded",
			RefundedAt: now,
		}, nil
	}
	if deposit.PaymentStatus != StatusSucceeded || deposit.ProviderPaymentID == "" {
		return RefundResult{}, ErrDepositNotPaid
	}

	refundAmount := deposit.RefundableAmount
	if amount != nil && *amount > 0 {
		if *amount > deposit.RefundableAmount {
			return RefundResult{}, ErrRefundExceedsDeposit
		}
		refundAmount = *amount
	}

	idempotencyKey := fmt.Sprintf("refund:%s:%d", deposit.DepositID, refundAmount)
	description := "Возврат обеспечительного платежа"
	if deposit.OrderNumber != "" {
		description = fmt.Sprintf("Возврат обеспечительного платежа по заказу %s", deposit.OrderNumber)
	}
	if reason != "" {
		description += ": " + reason
	}
	if len(description) > 250 {
		description = description[:250]
	}

	req := yookassa.CreateRefundRequest{
		PaymentID: deposit.ProviderPaymentID,
		Amount: yookassa.Money{
			Value:    formatKopecks(refundAmount),
			Currency: "RUB",
		},
		Description: description,
	}

	if service.receiptsEnabled {
		customer := yookassa.ReceiptCustomer{
			Email: deposit.ClientEmail,
			Phone: deposit.ClientPhone,
		}
		vatCode := service.vatCode
		if vatCode <= 0 {
			vatCode = 1
		}
		req.Receipt = &yookassa.Receipt{
			Customer:      customer,
			TaxSystemCode: service.taxSystemCode,
			Items: []yookassa.ReceiptItem{
				{
					Description:    "Возврат: Обеспечительный платеж",
					Quantity:       "1.00",
					Amount: yookassa.Money{
						Value:    formatKopecks(refundAmount),
						Currency: "RUB",
					},
					VATCode:        vatCode,
					PaymentMode:    "full_payment",
					PaymentSubject: "payment",
				},
			},
		}
	}

	refund, err := service.provider.CreateRefund(ctx, idempotencyKey, req)
	if err != nil {
		service.logger.Printf("YooKassa refund failed for rental %s: %v", rentalID, err)
		return RefundResult{}, fmt.Errorf("provider refund failed: %w", err)
	}

	if err := service.repository.RecordDepositRefund(
		ctx,
		deposit.DepositID,
		refundAmount,
		refund.ID,
		refund.Raw,
		now,
	); err != nil {
		service.logger.Printf("record deposit refund failed for rental %s: %v", rentalID, err)
		return RefundResult{}, fmt.Errorf("record deposit refund: %w", err)
	}

	service.logger.Printf(
		"YooKassa refund processed: rental=%s deposit=%s refund_id=%s amount=%s status=%s",
		rentalID,
		deposit.DepositID,
		refund.ID,
		formatKopecks(refundAmount),
		refund.Status,
	)

	return RefundResult{
		DepositID:  deposit.DepositID,
		PaymentID:  deposit.PaymentID,
		RefundID:   refund.ID,
		Amount:     refundAmount,
		Status:     refund.Status,
		RefundedAt: now,
	}, nil
}

func (service *Service) StoreWebhook(
	ctx context.Context,
	raw json.RawMessage,
) (string, bool, error) {
	if !service.enabled {
		return "", false, ErrDisabled
	}
	var notification struct {
		Type   string `json:"type"`
		Event  string `json:"event"`
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	}
	if err := json.Unmarshal(raw, &notification); err != nil ||
		notification.Type != "notification" ||
		strings.TrimSpace(notification.Object.ID) == "" {
		return "", false, ErrWebhookMismatch
	}
	hashInput := notification.Event + ":" + notification.Object.ID
	eventKey := "sha256:" + sha256Hex(hashInput)
	return service.repository.StoreEvent(
		ctx,
		eventKey,
		notification.Object.ID,
		notification.Event,
		raw,
		service.now().UTC(),
	)
}

func (service *Service) ProcessEvent(ctx context.Context, eventID string) error {
	now := service.now().UTC()
	event, claimed, err := service.repository.ClaimEventByID(ctx, eventID, now)
	if err != nil || !claimed {
		return err
	}
	return service.processClaimed(ctx, event, now)
}

func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	if !service.enabled {
		return false, nil
	}
	now := service.now().UTC()
	event, claimed, err := service.repository.ClaimNextEvent(
		ctx,
		now,
		now.Add(-5*time.Minute),
		service.maxAttempts,
	)
	if err != nil || !claimed {
		return false, err
	}
	return true, service.processClaimed(ctx, event, now)
}

func (service *Service) processClaimed(
	ctx context.Context,
	event PaymentEvent,
	now time.Time,
) error {
	switch event.EventType {
	case EventPaymentSucceeded, EventPaymentCanceled:
	default:
		return service.reject(ctx, event, now, ErrUnsupportedEvent)
	}
	expected, err := service.repository.GetByProviderID(ctx, event.ProviderPaymentID)
	if err != nil {
		if errors.Is(err, ErrWebhookMismatch) {
			return service.reject(ctx, event, now, err)
		}
		if retryErr := service.repository.RetryEvent(
			ctx,
			event.ID,
			now.Add(service.retryDelay(event.Attempts)),
			err.Error(),
		); retryErr != nil {
			return retryErr
		}
		return err
	}
	actual, err := service.provider.GetPayment(ctx, event.ProviderPaymentID)
	if err != nil {
		if isTemporary(err) {
			retryAt := now.Add(service.retryDelay(event.Attempts))
			if retryErr := service.repository.RetryEvent(
				ctx,
				event.ID,
				retryAt,
				err.Error(),
			); retryErr != nil {
				return retryErr
			}
			return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
		}
		return service.reject(ctx, event, now, err)
	}
	if err := validateProviderPayment(expected, actual, true); err != nil {
		return service.reject(ctx, event, now, err)
	}

	receiptStatus := fiscalStatus(actual.ReceiptRegistration, service.receiptsEnabled)
	switch event.EventType {
	case EventPaymentSucceeded:
		if actual.Status != "succeeded" || !actual.Paid {
			return service.reject(ctx, event, now, ErrWebhookMismatch)
		}
		return service.repository.CompleteSucceeded(
			ctx,
			event,
			actual,
			receiptStatus,
			now,
		)
	case EventPaymentCanceled:
		if actual.Status != "canceled" || actual.Paid {
			return service.reject(ctx, event, now, ErrWebhookMismatch)
		}
		expired := actual.CancellationDetails != nil &&
			actual.CancellationDetails.Reason == "expired_on_confirmation"
		return service.repository.CompleteCanceled(
			ctx,
			event,
			actual,
			expired,
			receiptStatus,
			now,
		)
	default:
		return service.reject(ctx, event, now, ErrUnsupportedEvent)
	}
}

func (service *Service) reject(
	ctx context.Context,
	event PaymentEvent,
	now time.Time,
	err error,
) error {
	if rejectErr := service.repository.RejectEvent(ctx, event.ID, err.Error(), now); rejectErr != nil {
		return rejectErr
	}
	return err
}

func (service *Service) retryDelay(attempt int) time.Duration {
	exponent := attempt - 1
	if exponent < 0 {
		exponent = 0
	}
	if exponent > 10 {
		exponent = 10
	}
	return service.retryBase * time.Duration(1<<exponent)
}

func validateProviderPayment(
	expected Payment,
	actual yookassa.Payment,
	requireProviderID bool,
) error {
	if requireProviderID &&
		(expected.ProviderPaymentID == nil || actual.ID != *expected.ProviderPaymentID) {
		return ErrWebhookMismatch
	}
	if actual.Amount.Currency != "RUB" ||
		actual.Amount.Currency != expected.Currency ||
		actual.Metadata["rental_id"] != expected.RentalRequestID {
		return ErrProviderMismatch
	}
	amount, err := parseKopecks(actual.Amount.Value)
	if err != nil || amount != expected.TotalAmount {
		return ErrProviderMismatch
	}
	return nil
}

func formatKopecks(value int64) string {
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

func parseKopecks(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) != 2 || strings.HasPrefix(parts[0], "-") {
		return 0, errors.New("invalid monetary amount")
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || major > (math.MaxInt64-99)/100 {
		return 0, errors.New("invalid monetary amount")
	}
	minor, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || minor < 0 || minor > 99 {
		return 0, errors.New("invalid monetary amount")
	}
	return major*100 + minor, nil
}

func fiscalStatus(providerStatus string, enabled bool) string {
	if !enabled {
		return "disabled"
	}
	switch providerStatus {
	case "pending", "succeeded", "canceled":
		return providerStatus
	default:
		return "unknown"
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(sum)*2)
	for index, current := range sum {
		result[index*2] = alphabet[current>>4]
		result[index*2+1] = alphabet[current&0x0f]
	}
	return string(result)
}

type temporaryError interface {
	IsTemporary() bool
}

func isTemporary(err error) bool {
	var value temporaryError
	if errors.As(err, &value) {
		return value.IsTemporary()
	}
	return true
}

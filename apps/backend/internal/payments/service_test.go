package payments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
)

func TestPaymentCreationRequiresPayableVerifiedRental(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "awaiting payment only", err: ErrRentalNotPayable},
		{name: "before payment deadline only", err: ErrPaymentExpired},
		{name: "verified client only", err: ErrClientNotVerified},
		{name: "confirmed hold only", err: ErrHoldNotConfirmed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &repositoryStub{prepareErr: testCase.err}
			service := testService(repository, &providerStub{})
			_, err := service.Create(context.Background(), "client-1", "rental-1")
			if !errors.Is(err, testCase.err) {
				t.Fatalf("expected %v, got %v", testCase.err, err)
			}
		})
	}
}

func TestPaymentAmountComesFromStoredRentalAndCreationIsIdempotent(t *testing.T) {
	payment := preparedPayment()
	repository := &repositoryStub{
		prepared: payment,
		created:  true,
	}
	provider := &providerStub{
		createResult: providerPayment("pending", false, "3500.00"),
	}
	service := testService(repository, provider)

	result, err := service.Create(context.Background(), "client-1", "rental-1")
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if !result.Created ||
		result.Payment.TotalAmount != 350_000 ||
		provider.lastCreate.Amount.Value != "3500.00" ||
		provider.lastCreate.Metadata["rental_id"] != "rental-1" {
		t.Fatalf("unexpected payment creation: %#v request=%#v", result, provider.lastCreate)
	}
	if repository.saved.ProviderPaymentID == nil {
		t.Fatal("provider payment was not persisted")
	}

	repository.prepared = repository.saved
	repository.created = false
	second, err := service.Create(context.Background(), "client-1", "rental-1")
	if err != nil {
		t.Fatalf("repeat payment creation: %v", err)
	}
	if second.Created || provider.createCalls != 1 || second.Payment.ID != result.Payment.ID {
		t.Fatalf(
			"repeat created a second payment: result=%#v provider_calls=%d",
			second,
			provider.createCalls,
		)
	}
}

func TestPaymentProviderFailureDoesNotMutateRental(t *testing.T) {
	repository := &repositoryStub{
		prepared: preparedPayment(),
		created:  true,
	}
	provider := &providerStub{createErr: errors.New("network down")}
	service := testService(repository, provider)

	_, err := service.Create(context.Background(), "client-1", "rental-1")
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected provider failure, got %v", err)
	}
	if repository.saveCalls != 0 ||
		repository.successCalls != 0 ||
		repository.cancelCalls != 0 {
		t.Fatalf("network failure mutated payment lifecycle: %#v", repository)
	}
	if repository.prepared.Status != StatusCreating {
		t.Fatalf("prepared payment was changed: %#v", repository.prepared)
	}
}

func TestMismatchedWebhookIsRejected(t *testing.T) {
	repository := eventRepository()
	provider := &providerStub{
		getResult: providerPayment("succeeded", true, "1.00"),
	}
	service := testService(repository, provider)

	eventID, _, err := service.StoreWebhook(
		context.Background(),
		json.RawMessage(`{
			"type":"notification",
			"event":"payment.succeeded",
			"object":{"id":"provider-1"}
		}`),
	)
	if err != nil {
		t.Fatalf("store webhook: %v", err)
	}
	err = service.ProcessEvent(context.Background(), eventID)
	if !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("expected mismatch rejection, got %v", err)
	}
	if repository.rejectCalls != 1 || repository.successCalls != 0 {
		t.Fatalf("mismatched webhook changed payment: %#v", repository)
	}
}

func TestSucceededWebhookIsIdempotent(t *testing.T) {
	repository := eventRepository()
	provider := &providerStub{
		getResult: providerPayment("succeeded", true, "3500.00"),
	}
	service := testService(repository, provider)
	raw := json.RawMessage(`{
		"type":"notification",
		"event":"payment.succeeded",
		"object":{"id":"provider-1"}
	}`)

	eventID, created, err := service.StoreWebhook(context.Background(), raw)
	if err != nil || !created {
		t.Fatalf("store webhook: created=%t err=%v", created, err)
	}
	if err := service.ProcessEvent(context.Background(), eventID); err != nil {
		t.Fatalf("process webhook: %v", err)
	}
	if repository.successCalls != 1 {
		t.Fatalf("expected one successful transition, got %d", repository.successCalls)
	}

	duplicateID, created, err := service.StoreWebhook(context.Background(), raw)
	if err != nil || created || duplicateID != eventID {
		t.Fatalf("duplicate webhook was not deduplicated: id=%q created=%t err=%v", duplicateID, created, err)
	}
	if err := service.ProcessEvent(context.Background(), duplicateID); err != nil {
		t.Fatalf("process duplicate webhook: %v", err)
	}
	if repository.successCalls != 1 {
		t.Fatalf("duplicate webhook changed state %d times", repository.successCalls)
	}
}

func TestTemporaryWebhookFailureIsScheduledForRetry(t *testing.T) {
	repository := eventRepository()
	provider := &providerStub{getErr: temporaryTestError{}}
	service := testService(repository, provider)
	eventID, _, err := service.StoreWebhook(
		context.Background(),
		json.RawMessage(`{
			"type":"notification",
			"event":"payment.succeeded",
			"object":{"id":"provider-1"}
		}`),
	)
	if err != nil {
		t.Fatalf("store webhook: %v", err)
	}
	err = service.ProcessEvent(context.Background(), eventID)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected retryable provider error, got %v", err)
	}
	if repository.retryCalls != 1 || repository.successCalls != 0 {
		t.Fatalf("temporary error did not schedule safe retry: %#v", repository)
	}
}

func TestExpiredProviderPaymentUsesExpiryTransition(t *testing.T) {
	repository := eventRepository()
	providerPayment := providerPayment("canceled", false, "3500.00")
	providerPayment.CancellationDetails = &yookassa.CancellationDetails{
		Party:  "yoo_money",
		Reason: "expired_on_confirmation",
	}
	service := testService(repository, &providerStub{getResult: providerPayment})
	eventID, _, err := service.StoreWebhook(
		context.Background(),
		json.RawMessage(`{
			"type":"notification",
			"event":"payment.canceled",
			"object":{"id":"provider-1"}
		}`),
	)
	if err != nil {
		t.Fatalf("store canceled webhook: %v", err)
	}
	if err := service.ProcessEvent(context.Background(), eventID); err != nil {
		t.Fatalf("process canceled webhook: %v", err)
	}
	if repository.cancelCalls != 1 || !repository.lastExpired {
		t.Fatalf("expired payment did not use expiry transition: %#v", repository)
	}
}

type providerStub struct {
	createResult yookassa.Payment
	createErr    error
	getResult    yookassa.Payment
	getErr       error
	lastCreate   yookassa.CreatePaymentRequest
	createCalls  int
}

func (provider *providerStub) CreatePayment(
	_ context.Context,
	_ string,
	input yookassa.CreatePaymentRequest,
) (yookassa.Payment, error) {
	provider.createCalls++
	provider.lastCreate = input
	return provider.createResult, provider.createErr
}

func (provider *providerStub) GetPayment(
	_ context.Context,
	_ string,
) (yookassa.Payment, error) {
	return provider.getResult, provider.getErr
}

func (provider *providerStub) CreateRefund(
	_ context.Context,
	_ string,
	input yookassa.CreateRefundRequest,
) (yookassa.Refund, error) {
	return yookassa.Refund{
		ID:        "refund-1",
		PaymentID: input.PaymentID,
		Status:    "succeeded",
		Amount:    input.Amount,
	}, nil
}

func (provider *providerStub) GetRefund(
	_ context.Context,
	refundID string,
) (yookassa.Refund, error) {
	return yookassa.Refund{
		ID:     refundID,
		Status: "succeeded",
	}, nil
}

type repositoryStub struct {
	prepared     Payment
	createData   CreateData
	created      bool
	prepareErr   error
	saved        Payment
	event        PaymentEvent
	eventCreated bool
	claimed      bool
	saveCalls    int
	successCalls int
	cancelCalls  int
	retryCalls   int
	rejectCalls  int
	lastExpired  bool
}

func (repository *repositoryStub) PreparePayment(
	context.Context,
	string,
	string,
	bool,
	time.Time,
) (Payment, CreateData, bool, error) {
	return repository.prepared, repository.createData, repository.created, repository.prepareErr
}

func (repository *repositoryStub) SaveProviderPayment(
	_ context.Context,
	_ string,
	provider yookassa.Payment,
	now time.Time,
) (Payment, error) {
	repository.saveCalls++
	saved := repository.prepared
	saved.ProviderPaymentID = &provider.ID
	saved.Status = StatusPending
	saved.UpdatedAt = now
	url := provider.Confirmation.ConfirmationURL
	saved.ConfirmationURL = &url
	repository.saved = saved
	return saved, nil
}

func (repository *repositoryStub) MarkCreationForReview(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (repository *repositoryStub) StoreEvent(
	_ context.Context,
	_ string,
	providerID string,
	eventType string,
	raw json.RawMessage,
	_ time.Time,
) (string, bool, error) {
	if repository.event.ID != "" {
		return repository.event.ID, false, nil
	}
	repository.event = PaymentEvent{
		ID:                "event-1",
		ProviderPaymentID: providerID,
		EventType:         eventType,
		RawPayload:        raw,
	}
	repository.claimed = false
	return repository.event.ID, true, nil
}

func (repository *repositoryStub) ClaimEventByID(
	context.Context,
	string,
	time.Time,
) (PaymentEvent, bool, error) {
	if repository.claimed {
		return PaymentEvent{}, false, nil
	}
	repository.claimed = true
	repository.event.Attempts++
	return repository.event, true, nil
}

func (repository *repositoryStub) ClaimNextEvent(
	context.Context,
	time.Time,
	time.Time,
	int,
) (PaymentEvent, bool, error) {
	return PaymentEvent{}, false, nil
}

func (repository *repositoryStub) GetByProviderID(
	context.Context,
	string,
) (Payment, error) {
	return repository.prepared, nil
}

func (repository *repositoryStub) CompleteSucceeded(
	context.Context,
	PaymentEvent,
	yookassa.Payment,
	string,
	time.Time,
) error {
	repository.successCalls++
	return nil
}

func (repository *repositoryStub) CompleteCanceled(
	_ context.Context,
	_ PaymentEvent,
	_ yookassa.Payment,
	expired bool,
	_ string,
	_ time.Time,
) error {
	repository.cancelCalls++
	repository.lastExpired = expired
	return nil
}

func (repository *repositoryStub) RetryEvent(
	context.Context,
	string,
	time.Time,
	string,
) error {
	repository.retryCalls++
	return nil
}

func (repository *repositoryStub) RejectEvent(
	context.Context,
	string,
	string,
	time.Time,
) error {
	repository.rejectCalls++
	return nil
}

func (repository *repositoryStub) GetDepositForRefund(
	_ context.Context,
	rentalID string,
) (DepositRefundData, error) {
	return DepositRefundData{
		DepositID:         "deposit-1",
		RentalRequestID:   rentalID,
		OrderNumber:       "PI-2026-000001",
		PaymentID:         "payment-1",
		ProviderPaymentID: "provider-1",
		OriginalAmount:    200000,
		RefundableAmount:  200000,
		DepositStatus:     "paid",
		PaymentStatus:     StatusSucceeded,
		ClientPhone:       "+79991112233",
		ClientEmail:       "test@example.com",
	}, nil
}

func (repository *repositoryStub) RecordDepositRefund(
	_ context.Context,
	depositID string,
	amount int64,
	refundID string,
	providerPayload json.RawMessage,
	now time.Time,
) error {
	return nil
}

func preparedPayment() Payment {
	return Payment{
		ID:                "payment-1",
		RentalRequestID:   "rental-1",
		ProviderPaymentID: nil,
		IdempotencyKey:    "idempotency-1",
		RentalAmount:      100_000,
		DepositAmount:     200_000,
		DeliveryAmount:    50_000,
		TotalAmount:       350_000,
		Currency:          "RUB",
		Status:            StatusCreating,
		ProviderPayload:   json.RawMessage(`{}`),
		ConfirmationURL:   nil,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
}

func eventRepository() *repositoryStub {
	payment := preparedPayment()
	providerID := "provider-1"
	payment.ProviderPaymentID = &providerID
	payment.Status = StatusPending
	return &repositoryStub{prepared: payment, eventCreated: true}
}

func providerPayment(status string, paid bool, amount string) yookassa.Payment {
	return yookassa.Payment{
		ID:     "provider-1",
		Status: status,
		Paid:   paid,
		Amount: yookassa.Money{Value: amount, Currency: "RUB"},
		Confirmation: &yookassa.Confirmation{
			Type:            "redirect",
			ConfirmationURL: "https://yookassa.test/pay/provider-1",
		},
		Metadata: map[string]string{"rental_id": "rental-1"},
		Raw:      json.RawMessage(`{"id":"provider-1"}`),
	}
}

func testService(repository Repository, provider yookassa.Client) *Service {
	return NewService(
		repository,
		provider,
		true,
		"https://app.example.test/return",
		false,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	)
}

type temporaryTestError struct{}

func (temporaryTestError) Error() string     { return "temporary" }
func (temporaryTestError) IsTemporary() bool { return true }

func TestFiscalReceiptCustomerAndTaxSystem(t *testing.T) {
	repository := &repositoryStub{
		prepared: preparedPayment(),
		createData: CreateData{
			ClientPhone:  "+79991112233",
			ClientEmail:  "client@example.test",
			ToolName:     "Перфоратор Bosch",
			ReceiptItems: []yookassa.ReceiptItem{{Description: "Аренда", Amount: yookassa.Money{Value: "3500.00", Currency: "RUB"}}},
		},
		created: true,
	}
	provider := &providerStub{
		createResult: providerPayment("pending", false, "3500.00"),
	}
	service := NewService(
		repository,
		provider,
		true,
		"https://app.example.test/return",
		true,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	).SetFiscalParameters(2, 1)

	_, err := service.Create(context.Background(), "client-1", "rental-1")
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	if provider.lastCreate.Receipt == nil {
		t.Fatal("expected receipt in provider create request")
	}
	receipt := provider.lastCreate.Receipt
	if receipt.Customer.Phone != "+79991112233" || receipt.Customer.Email != "client@example.test" {
		t.Fatalf("unexpected receipt customer: %#v", receipt.Customer)
	}
	if receipt.TaxSystemCode == nil || *receipt.TaxSystemCode != 2 {
		t.Fatalf("unexpected tax system code: %v", receipt.TaxSystemCode)
	}
	if len(receipt.Items) == 0 {
		t.Fatal("expected receipt items")
	}
	for _, item := range receipt.Items {
		if item.VATCode != 1 {
			t.Fatalf("expected VAT code 1, got %d", item.VATCode)
		}
	}
}

func TestRefundDepositSuccessAndReceipt(t *testing.T) {
	repository := &repositoryStub{}
	provider := &providerStub{}
	service := NewService(
		repository,
		provider,
		true,
		"https://app.example.test/return",
		true,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	).SetFiscalParameters(2, 1)

	result, err := service.RefundDeposit(context.Background(), "rental-1", nil, "Завершение заказа")
	if err != nil {
		t.Fatalf("refund deposit: %v", err)
	}
	if result.RefundID != "refund-1" || result.Amount != 200000 || result.Status != "succeeded" {
		t.Fatalf("unexpected refund result: %#v", result)
	}
}

func TestRefundDepositAlreadyRefunded(t *testing.T) {
	provider := &providerStub{}

	// Custom stub that returns already refunded deposit
	alreadyRefundedRepo := &alreadyRefundedRepoStub{}
	serviceWithRefunded := testService(alreadyRefundedRepo, provider)

	result, err := serviceWithRefunded.RefundDeposit(context.Background(), "rental-1", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "already_refunded" || result.Amount != 0 {
		t.Fatalf("expected already_refunded status, got: %#v", result)
	}
}

type alreadyRefundedRepoStub struct {
	repositoryStub
}

func (r *alreadyRefundedRepoStub) GetDepositForRefund(
	_ context.Context,
	rentalID string,
) (DepositRefundData, error) {
	return DepositRefundData{
		DepositID:        "deposit-1",
		RefundableAmount: 0,
		DepositStatus:    "refunded",
		PaymentStatus:    StatusSucceeded,
	}, nil
}

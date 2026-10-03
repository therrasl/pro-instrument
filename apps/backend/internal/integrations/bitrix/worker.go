package bitrix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/payments"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

const workerLease = 5 * time.Minute

const (
	BitrixFieldDays          = "UF_CRM_1755208962363" // double: Количество дней
	BitrixFieldStartTime     = "UF_CRM_1755209011746" // datetime: Время выдачи
	BitrixFieldEndTime       = "UF_CRM_1773210836"    // datetime: Дата/время возврата
	BitrixFieldTool          = "UF_CRM_1755209429146" // string: Какое нужно оборудование
	BitrixFieldDailyPrice    = "UF_CRM_1755209482048" // money: Базовая стоимость в сутки
	BitrixFieldClientName    = "UF_CRM_1755209635979" // string: Контактное лицо: ФИО
	BitrixFieldClientPhone   = "UF_CRM_1756648546267" // string: Контактное лицо: телефон
	BitrixFieldContractNum   = "UF_CRM_1757331206997" // string: Номер договора
	BitrixFieldContractDate  = "UF_CRM_1757331227232" // date: Дата договора
	BitrixFieldDeposit       = "UF_CRM_1758015711985" // money: Обеспечительный платеж
	BitrixFieldClientPayment = "UF_CRM_1755209690479" // money: Оплата от клиента
	BitrixFieldDeliveryCost  = "UF_CRM_1755209709779" // money: Доставка
	BitrixFieldDeliveryAddr  = "UF_CRM_1755209641247" // string: Адрес доставки
	BitrixFieldDebtSurcharge = "UF_CRM_1773211114142" // money: Доплата Арендатора (задолженность/просрочка)
	BitrixFieldDepositRefund = "UF_CRM_1773211126261" // money: Возврат залога Арендатору
)

type WorkerRepository interface {
	ClaimOutbox(context.Context, time.Time, time.Time, int) (OutboxEvent, bool, error)
	CompleteOutbox(context.Context, string, time.Time) error
	RetryOutbox(context.Context, string, time.Time, string) error
	FailOutbox(context.Context, string, int, string) error
	GetClientSyncData(context.Context, string) (ClientSyncData, error)
	SaveContactID(context.Context, string, string) error
	FindClientIDByBitrixContact(context.Context, string, string) (string, error)
	GetRentalSyncData(context.Context, string) (RentalSyncData, error)
	SaveDealID(context.Context, string, string) error
	ClaimInbound(context.Context, time.Time, time.Time, int) (InboundEvent, bool, error)
	CompleteInbound(context.Context, string, string, time.Time) error
	RetryInbound(context.Context, string, time.Time, string) error
	RejectInbound(context.Context, string, int, string, time.Time) error
	CreateRentalFromBitrix(context.Context, DealFull, ContactDetails, string) (string, error)
}

type RentalStatusUpdater interface {
	ApplyBitrixStatus(context.Context, string, string) (string, bool, error)
}

type DepositRefunder interface {
	RefundDeposit(context.Context, string, *int64, string) (payments.RefundResult, error)
}

type VerificationReviewer interface {
	Approve(context.Context, string, string) (verification.Review, error)
	Reject(context.Context, string, string, string) (verification.Review, error)
}

type VerificationPushNotifier interface {
	NotifyVerificationApproved(context.Context, string) error
	NotifyVerificationRejected(context.Context, string, string) error
}

type Worker struct {
	repository               WorkerRepository
	client                   Client
	statusUpdater            RentalStatusUpdater
	depositRefunder          DepositRefunder
	verificationReviewer     VerificationReviewer
	verificationPushNotifier VerificationPushNotifier
	settings                 config.BitrixConfig
	logger                   *log.Logger
	now                      func() time.Time
}

func NewWorker(
	repository WorkerRepository,
	client Client,
	statusUpdater RentalStatusUpdater,
	settings config.BitrixConfig,
	logger *log.Logger,
) *Worker {
	return &Worker{
		repository:    repository,
		client:        client,
		statusUpdater: statusUpdater,
		settings:      settings,
		logger:        logger,
		now:           time.Now,
	}
}

func (worker *Worker) SetDepositRefunder(refunder DepositRefunder) *Worker {
	worker.depositRefunder = refunder
	return worker
}

func (worker *Worker) SetVerificationReviewer(reviewer VerificationReviewer) *Worker {
	worker.verificationReviewer = reviewer
	return worker
}

func (worker *Worker) SetVerificationPushNotifier(notifier VerificationPushNotifier) *Worker {
	worker.verificationPushNotifier = notifier
	return worker
}

func (worker *Worker) Run(ctx context.Context) error {
	if !worker.settings.Enabled {
		worker.logger.Print("Bitrix worker is disabled")
		return nil
	}
	for {
		processed, err := worker.RunOnce(ctx)
		if err != nil {
			worker.logger.Printf("Bitrix worker iteration failed: %v", err)
		}
		if processed {
			continue
		}

		timer := time.NewTimer(worker.settings.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (worker *Worker) RunOnce(ctx context.Context) (bool, error) {
	if !worker.settings.Enabled {
		return false, nil
	}
	now := worker.now().UTC()
	processed := false

	event, ok, err := worker.repository.ClaimOutbox(
		ctx,
		now,
		now.Add(-workerLease),
		worker.settings.MaxAttempts,
	)
	if err != nil {
		return false, err
	}
	if ok {
		processed = true
		if err := worker.processOutbox(ctx, event, now); err != nil {
			return processed, err
		}
	}

	inbound, ok, err := worker.repository.ClaimInbound(
		ctx,
		now,
		now.Add(-workerLease),
		worker.settings.MaxAttempts,
	)
	if err != nil {
		return processed, err
	}
	if ok {
		processed = true
		if err := worker.processInbound(ctx, inbound, now); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

func (worker *Worker) processOutbox(
	ctx context.Context,
	event OutboxEvent,
	now time.Time,
) error {
	var processErr error
	switch event.EventType {
	case EventContactUpsert:
		processErr = worker.upsertContact(ctx, event.AggregateID)
	case EventDealCreate:
		processErr = worker.createDeal(ctx, event.AggregateID)
	case EventDealUpdate:
		processErr = worker.updateDeal(ctx, event.AggregateID)
	default:
		processErr = permanentError{fmt.Errorf("unsupported outbox event %q", event.EventType)}
	}

	if processErr == nil {
		return worker.repository.CompleteOutbox(ctx, event.ID, now)
	}
	if isPermanent(processErr) || event.Attempts >= worker.settings.MaxAttempts {
		return worker.repository.FailOutbox(
			ctx,
			event.ID,
			worker.settings.MaxAttempts,
			processErr.Error(),
		)
	}
	return worker.repository.RetryOutbox(
		ctx,
		event.ID,
		now.Add(worker.retryDelay(event.Attempts)),
		processErr.Error(),
	)
}

func (worker *Worker) upsertContact(ctx context.Context, clientID string) error {
	data, err := worker.repository.GetClientSyncData(ctx, clientID)
	if err != nil {
		return err
	}
	input := ContactInput{
		FullName:       data.FullName,
		Phone:          data.Phone,
		BirthDate:      data.BirthDate,
		Email:          data.Email,
		ClientType:     data.ClientType,
		CompanyName:    data.CompanyName,
		INN:            data.INN,
		KPP:            data.KPP,
		OGRN:           data.OGRN,
		LegalAddress:   data.LegalAddress,
		CompanyContact: data.CompanyContact,
	}
	if data.ContactID != "" {
		_ = worker.client.UpdateContact(ctx, data.ContactID, input)
		return nil
	}

	contactID, found, err := worker.client.FindContactByPhone(ctx, data.Phone)
	if err != nil {
		return classifyClientError(err)
	}
	if found {
		_ = worker.client.UpdateContact(ctx, contactID, input)
	} else {
		contactID, err = worker.client.CreateContact(ctx, input)
		if err != nil {
			return classifyClientError(err)
		}
	}
	return worker.repository.SaveContactID(ctx, clientID, contactID)
}

func (worker *Worker) createDeal(ctx context.Context, rentalID string) error {
	data, err := worker.repository.GetRentalSyncData(ctx, rentalID)
	if err != nil {
		return err
	}
	if data.DealID != "" {
		return nil
	}
	if data.ContactID == "" {
		return dependencyError{errors.New("Bitrix contact is not synchronized yet")}
	}

	dealID, found, err := worker.client.FindDealByRentalID(
		ctx,
		businessNumber(data),
		worker.settings.CategoryID,
	)
	if err != nil {
		return classifyClientError(err)
	}
	if !found {
		dealID, err = worker.client.CreateDeal(ctx, worker.dealFields(data))
		if err != nil {
			return classifyClientError(err)
		}
	}
	return worker.repository.SaveDealID(ctx, rentalID, dealID)
}

func (worker *Worker) updateDeal(ctx context.Context, rentalID string) error {
	data, err := worker.repository.GetRentalSyncData(ctx, rentalID)
	if err != nil {
		return err
	}
	if data.DealID == "" {
		return dependencyError{errors.New("Bitrix deal is not synchronized yet")}
	}
	stageID, ok := worker.stageForStatus(data.Status)
	if !ok {
		return permanentError{fmt.Errorf("rental status %q has no Bitrix stage", data.Status)}
	}

	dailyRate := data.RentalPrice
	if data.RentalDays > 0 {
		dailyRate = data.RentalPrice / int64(data.RentalDays)
	}

	updatePayload := map[string]any{
		"STAGE_ID":               stageID,
		"OPPORTUNITY":            formatKopecks(data.TotalAmount),
		"COMMENTS":               worker.dealComments(data),
		BitrixFieldClientPayment: formatMoney(data.TotalAmount),
		BitrixFieldDeposit:       formatMoney(data.DepositAmount),
		BitrixFieldDailyPrice:    formatMoney(dailyRate),
	}
	if data.EndDate != "" {
		updatePayload["CLOSEDATE"] = data.EndDate + "T19:00:00+03:00"
		if worker.settings.Fields.EndDate != "" {
			updatePayload[worker.settings.Fields.EndDate] = data.EndDate
		}
	}
	if data.Status == rentals.StatusCompleted {
		updatePayload[BitrixFieldDepositRefund] = formatMoney(data.DepositAmount)
	}

	if err := worker.client.UpdateDeal(
		ctx,
		data.DealID,
		updatePayload,
	); err != nil {
		return classifyClientError(err)
	}
	return nil
}

func (worker *Worker) processInbound(
	ctx context.Context,
	event InboundEvent,
	now time.Time,
) error {
	if worker.isContactInbound(event) {
		return worker.processInboundContact(ctx, event, now)
	}

	deal, err := worker.client.GetDeal(ctx, event.BitrixDealID)
	if err != nil {
		if contact, cErr := worker.client.GetContact(ctx, event.BitrixDealID); cErr == nil && contact.ID != "" {
			return worker.processInboundContact(ctx, event, now)
		}
		return worker.handleInboundError(ctx, event, now, classifyClientError(err))
	}
	categoryStr := strconv.Itoa(worker.settings.CategoryID)
	if deal.CategoryID != categoryStr && deal.CategoryID != "0" && deal.CategoryID != "" {
		return worker.handleInboundError(
			ctx,
			event,
			now,
			permanentError{fmt.Errorf("deal category %q is not managed", deal.CategoryID)},
		)
	}
	targetStatus, ok := worker.statusForStage(deal.StageID)
	if !ok {
		return worker.handleInboundError(
			ctx,
			event,
			now,
			permanentError{fmt.Errorf("Bitrix stage %q is not mapped", deal.StageID)},
		)
	}

	rentalID, err := worker.applyInboundStatus(ctx, deal.ID, targetStatus)
	if errors.Is(err, rentals.ErrRentalNotFound) {
		rentalID, err = worker.importDealFromBitrix(ctx, deal.ID, targetStatus)
	}
	if isPermanentRentalStatusError(err) {
		err = permanentError{err}
	}
	if err != nil {
		return worker.handleInboundError(ctx, event, now, err)
	}

	if targetStatus == rentals.StatusCompleted && worker.depositRefunder != nil && rentalID != "" {
		if res, err := worker.depositRefunder.RefundDeposit(ctx, rentalID, nil, "Bitrix completed stage"); err != nil {
			worker.logger.Printf("auto refund deposit failed for rental %s: %v", rentalID, err)
		} else {
			worker.logger.Printf("auto refund deposit processed for rental %s: refund_id=%s amount=%d status=%s", rentalID, res.RefundID, res.Amount, res.Status)
		}
	}

	return worker.repository.CompleteInbound(ctx, event.ID, rentalID, now)
}

func (worker *Worker) isContactInbound(event InboundEvent) bool {
	upper := bytes.ToUpper(event.RawPayload)
	return bytes.Contains(upper, []byte("ONCRMCONTACT")) ||
		bytes.Contains(upper, []byte("\"CONTACT_ID\"")) ||
		bytes.Contains(upper, []byte("\"ENTITY_TYPE\":\"CONTACT\"")) ||
		bytes.Contains(upper, []byte("\"CONTACT\""))
}

func (worker *Worker) processInboundContact(
	ctx context.Context,
	event InboundEvent,
	now time.Time,
) error {
	contact, err := worker.client.GetContact(ctx, event.BitrixDealID)
	if err != nil {
		return worker.handleInboundError(ctx, event, now, classifyClientError(err))
	}

	if contact.VerifiedStatus != "approved" && contact.VerifiedStatus != "rejected" {
		return worker.repository.CompleteInbound(ctx, event.ID, "", now)
	}

	clientID, err := worker.repository.FindClientIDByBitrixContact(ctx, contact.ID, contact.Phone)
	if err != nil {
		return worker.handleInboundError(ctx, event, now, err)
	}
	if clientID == "" {
		return worker.repository.CompleteInbound(ctx, event.ID, "", now)
	}

	if contact.VerifiedStatus == "approved" {
		if worker.verificationReviewer != nil {
			_, err = worker.verificationReviewer.Approve(ctx, clientID, "bitrix_manager")
			if err != nil && !errors.Is(err, verification.ErrReviewNotAllowed) {
				return worker.handleInboundError(ctx, event, now, err)
			}
		}
		if worker.verificationPushNotifier != nil {
			_ = worker.verificationPushNotifier.NotifyVerificationApproved(ctx, clientID)
		}
		if worker.logger != nil {
			worker.logger.Printf("Bitrix contact %s (client %s) approved by manager", contact.ID, clientID)
		}
	} else if contact.VerifiedStatus == "rejected" {
		reason := contact.Comments
		if strings.TrimSpace(reason) == "" {
			reason = "Фотографии документов не соответствуют требованиям к качеству"
		}
		if worker.verificationReviewer != nil {
			_, err = worker.verificationReviewer.Reject(ctx, clientID, "bitrix_manager", reason)
			if err != nil && !errors.Is(err, verification.ErrReviewNotAllowed) {
				return worker.handleInboundError(ctx, event, now, err)
			}
		}
		if worker.verificationPushNotifier != nil {
			_ = worker.verificationPushNotifier.NotifyVerificationRejected(ctx, clientID, reason)
		}
		if worker.logger != nil {
			worker.logger.Printf("Bitrix contact %s (client %s) rejected by manager: %s", contact.ID, clientID, reason)
		}
	}

	return worker.repository.CompleteInbound(ctx, event.ID, clientID, now)
}

func (worker *Worker) importDealFromBitrix(
	ctx context.Context,
	dealID string,
	targetStatus string,
) (string, error) {
	dealFull, err := worker.client.GetDealFull(ctx, dealID)
	if err != nil {
		return "", err
	}
	var contact ContactDetails
	if dealFull.ContactID != "" {
		contact, err = worker.client.GetContact(ctx, dealFull.ContactID)
		if err != nil && worker.logger != nil {
			worker.logger.Printf("Could not get Bitrix contact %s: %v", dealFull.ContactID, err)
		}
	}
	return worker.repository.CreateRentalFromBitrix(ctx, dealFull, contact, targetStatus)
}

func (worker *Worker) applyInboundStatus(
	ctx context.Context,
	dealID string,
	targetStatus string,
) (string, error) {
	path, ok := inboundStatusPath(targetStatus)
	if !ok {
		return "", fmt.Errorf("rental status %q has no inbound transition path", targetStatus)
	}

	var rentalID string
	anchored := false
	for _, status := range path {
		currentRentalID, _, err := worker.statusUpdater.ApplyBitrixStatus(
			ctx,
			dealID,
			status,
		)
		if err == nil {
			rentalID = currentRentalID
			anchored = true
			continue
		}
		if errors.Is(err, rentals.ErrInvalidStatusTransition) && !anchored {
			continue
		}
		return "", err
	}
	if !anchored {
		return "", rentals.ErrInvalidStatusTransition
	}
	return rentalID, nil
}

func inboundStatusPath(targetStatus string) ([]string, bool) {
	forwardPath := []string{
		rentals.StatusAwaitingPayment,
		rentals.StatusPaid,
		rentals.StatusPreparing,
		rentals.StatusReady,
		rentals.StatusRented,
		rentals.StatusAwaitingReturn,
		rentals.StatusInspection,
		rentals.StatusCompleted,
	}
	switch targetStatus {
	case rentals.StatusPendingManager, rentals.StatusRejected:
		return []string{targetStatus}, true
	case rentals.StatusHandedToCourier:
		return []string{
			rentals.StatusAwaitingPayment,
			rentals.StatusPaid,
			rentals.StatusPreparing,
			rentals.StatusReady,
			rentals.StatusHandedToCourier,
		}, true
	}
	for index, status := range forwardPath {
		if status == targetStatus {
			return forwardPath[:index+1], true
		}
	}
	return nil, false
}

func isPermanentRentalStatusError(err error) bool {
	return errors.Is(err, rentals.ErrRentalNotFound) ||
		errors.Is(err, rentals.ErrInvalidStatusTransition) ||
		errors.Is(err, rentals.ErrRentalHoldNotFound) ||
		errors.Is(err, rentals.ErrRentalHoldExpired) ||
		errors.Is(err, rentals.ErrRentalHoldNotActive)
}

func (worker *Worker) handleInboundError(
	ctx context.Context,
	event InboundEvent,
	now time.Time,
	processErr error,
) error {
	if isPermanent(processErr) || event.Attempts >= worker.settings.MaxAttempts {
		return worker.repository.RejectInbound(
			ctx,
			event.ID,
			worker.settings.MaxAttempts,
			processErr.Error(),
			now,
		)
	}
	return worker.repository.RetryInbound(
		ctx,
		event.ID,
		now.Add(worker.retryDelay(event.Attempts)),
		processErr.Error(),
	)
}

func (worker *Worker) dealFields(data RentalSyncData) map[string]any {
	dailyRate := data.RentalPrice
	if data.RentalDays > 0 {
		dailyRate = data.RentalPrice / int64(data.RentalDays)
	}

	startDateTime := data.StartDate + "T09:00:00+03:00"
	endDateTime := data.EndDate + "T19:00:00+03:00"

	fields := map[string]any{
		"TITLE":       dealTitle(businessNumber(data)),
		"CATEGORY_ID": worker.settings.CategoryID,
		"STAGE_ID":    worker.settings.Stages.Application,
		"CONTACT_ID":          data.ContactID,
		"SOURCE_ID":           "MOBILE_APP",
		"SOURCE_DESCRIPTION":  "Мобильное приложение Pro-Instrument",
		"CURRENCY_ID":         "RUB",
		"OPPORTUNITY":         formatKopecks(data.TotalAmount),
		"COMMENTS":    worker.dealComments(data),
		"BEGINDATE":   startDateTime,
		"CLOSEDATE":   endDateTime,

		BitrixFieldDays:          data.RentalDays,
		BitrixFieldStartTime:     startDateTime,
		BitrixFieldEndTime:       endDateTime,
		BitrixFieldTool:          data.ToolName,
		BitrixFieldDailyPrice:    formatMoney(dailyRate),
		BitrixFieldClientName:    data.ClientFullName,
		BitrixFieldClientPhone:   data.ClientPhone,
		BitrixFieldContractNum:   businessNumber(data),
		BitrixFieldContractDate:  data.StartDate,
		BitrixFieldDeposit:       formatMoney(data.DepositAmount),
		BitrixFieldClientPayment: formatMoney(data.TotalAmount),
		BitrixFieldDeliveryCost:  formatMoney(data.DeliveryCost),
		BitrixFieldDeliveryAddr:  data.DeliveryAddress,
	}
	customFields := map[string]any{
		worker.settings.Fields.RentalID:    data.RentalID,
		worker.settings.Fields.Tool:        data.ToolName,
		worker.settings.Fields.StartDate:   data.StartDate,
		worker.settings.Fields.EndDate:     data.EndDate,
		worker.settings.Fields.RentalPrice: formatMoney(data.RentalPrice),
		worker.settings.Fields.Deposit:     formatMoney(data.DepositAmount),
		worker.settings.Fields.Delivery:    formatMoney(data.DeliveryCost),
		worker.settings.Fields.Address:     data.DeliveryAddress,
	}
	for fieldID, value := range customFields {
		if fieldID != "" {
			fields[fieldID] = value
		}
	}
	return fields
}

func (worker *Worker) dealComments(data RentalSyncData) string {
	lines := []string{
		"Номер заказа: " + businessNumber(data),
		"Внутренний rental ID: " + data.RentalID,
		"Тип клиента: " + clientTypeLabel(data.ClientType),
		"Клиент: " + data.ClientFullName,
		"Телефон: " + data.ClientPhone,
		"Инструмент: " + data.ToolName,
		"Период: " + data.StartDate + " (с 09:00) — " + data.EndDate + " (до 19:00)",
		"Аренда: " + formatKopecks(data.RentalPrice) + " RUB",
		"Обеспечительный платеж: " + formatKopecks(data.DepositAmount) + " RUB",
		"Доставка: " + data.DeliveryMethod + ", " + formatKopecks(data.DeliveryCost) + " RUB",
		"Адрес: " + firstNonEmpty(data.DeliveryAddress, "не указан"),
	}
	if data.ClientType == "legal_entity" {
		lines = append(lines, "Организация: "+data.OrganizationName, "ИНН: "+data.INN, "Контактное лицо: "+data.ContactFullName)
	}
	return strings.Join(lines, "\n")
}

func formatMoney(kopecks int64) string {
	return fmt.Sprintf("%.2f|RUB", float64(kopecks)/100.0)
}

func clientTypeLabel(value string) string {
	if value == "legal_entity" {
		return "Юридическое лицо"
	}
	return "Физическое лицо"
}

func businessNumber(data RentalSyncData) string {
	if data.OrderNumber != "" {
		return data.OrderNumber
	}
	return data.RentalID
}

func (worker *Worker) stageForStatus(status string) (string, bool) {
	stages := worker.settings.Stages
	mapping := map[string]string{
		rentals.StatusPendingManager:  stages.Application,
		rentals.StatusAwaitingPayment: stages.AwaitingPayment,
		rentals.StatusPaid:            stages.Paid,
		rentals.StatusPreparing:       stages.Preparing,
		rentals.StatusReady:           stages.Ready,
		rentals.StatusHandedToCourier: stages.Courier,
		rentals.StatusRented:          stages.Rented,
		rentals.StatusAwaitingReturn:  stages.AwaitingReturn,
		rentals.StatusInspection:      stages.Inspection,
		rentals.StatusCompleted:       stages.Completed,
		rentals.StatusRejected:        stages.Failed,
		rentals.StatusCancelled:       stages.Failed,
		rentals.StatusPaymentExpired:  stages.Failed,
	}
	stage, ok := mapping[status]
	return stage, ok
}

func (worker *Worker) statusForStage(stage string) (string, bool) {
	stages := worker.settings.Stages
	mapping := map[string]string{
		stages.Application:     rentals.StatusPendingManager,
		stages.AwaitingPayment: rentals.StatusAwaitingPayment,
		stages.Paid:            rentals.StatusPaid,
		stages.Preparing:       rentals.StatusPreparing,
		stages.Ready:           rentals.StatusReady,
		stages.Courier:         rentals.StatusHandedToCourier,
		stages.Rented:          rentals.StatusRented,
		stages.AwaitingReturn:  rentals.StatusAwaitingReturn,
		stages.Inspection:      rentals.StatusInspection,
		stages.Completed:       rentals.StatusCompleted,
		stages.Failed:          rentals.StatusRejected,
	}
	status, ok := mapping[stage]
	return status, ok
}

func (worker *Worker) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := worker.settings.RetryBase
	for index := 1; index < attempt && delay < time.Hour; index++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func formatKopecks(value int64) string {
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

type permanentError struct {
	error
}

type dependencyError struct {
	error
}

func classifyClientError(err error) error {
	if IsTemporary(err) {
		return err
	}
	return permanentError{err}
}

func isPermanent(err error) bool {
	var permanent permanentError
	return errors.As(err, &permanent)
}

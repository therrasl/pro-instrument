package bitrix

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

const workerLease = 5 * time.Minute

type WorkerRepository interface {
	ClaimOutbox(context.Context, time.Time, time.Time, int) (OutboxEvent, bool, error)
	CompleteOutbox(context.Context, string, time.Time) error
	RetryOutbox(context.Context, string, time.Time, string) error
	FailOutbox(context.Context, string, int, string) error
	GetClientSyncData(context.Context, string) (ClientSyncData, error)
	SaveContactID(context.Context, string, string) error
	GetRentalSyncData(context.Context, string) (RentalSyncData, error)
	SaveDealID(context.Context, string, string) error
	ClaimInbound(context.Context, time.Time, time.Time, int) (InboundEvent, bool, error)
	CompleteInbound(context.Context, string, string, time.Time) error
	RetryInbound(context.Context, string, time.Time, string) error
	RejectInbound(context.Context, string, int, string, time.Time) error
}

type RentalStatusUpdater interface {
	ApplyBitrixStatus(context.Context, string, string) (string, bool, error)
}

type Worker struct {
	repository    WorkerRepository
	client        Client
	statusUpdater RentalStatusUpdater
	settings      config.BitrixConfig
	logger        *log.Logger
	now           func() time.Time
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
	if data.ContactID != "" {
		return nil
	}

	contactID, found, err := worker.client.FindContactByPhone(ctx, data.Phone)
	if err != nil {
		return classifyClientError(err)
	}
	if !found {
		contactID, err = worker.client.CreateContact(ctx, ContactInput{
			FullName: data.FullName,
			Phone:    data.Phone,
		})
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
		data.RentalID,
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
	if err := worker.client.UpdateDeal(
		ctx,
		data.DealID,
		map[string]any{"STAGE_ID": stageID},
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
	deal, err := worker.client.GetDeal(ctx, event.BitrixDealID)
	if err != nil {
		return worker.handleInboundError(ctx, event, now, classifyClientError(err))
	}
	if deal.CategoryID != strconv.Itoa(worker.settings.CategoryID) {
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
	if isPermanentRentalStatusError(err) {
		err = permanentError{err}
	}
	if err != nil {
		return worker.handleInboundError(ctx, event, now, err)
	}
	return worker.repository.CompleteInbound(ctx, event.ID, rentalID, now)
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
	fields := map[string]any{
		"TITLE":       dealTitle(data.RentalID),
		"CATEGORY_ID": worker.settings.CategoryID,
		"STAGE_ID":    worker.settings.Stages.Application,
		"CONTACT_ID":  data.ContactID,
		"CURRENCY_ID": "RUB",
		"OPPORTUNITY": formatKopecks(data.TotalAmount),
		"COMMENTS":    worker.dealComments(data),
	}
	customFields := map[string]any{
		worker.settings.Fields.RentalID:    data.RentalID,
		worker.settings.Fields.Tool:        data.ToolName,
		worker.settings.Fields.StartDate:   data.StartDate,
		worker.settings.Fields.EndDate:     data.EndDate,
		worker.settings.Fields.RentalPrice: formatKopecks(data.RentalPrice),
		worker.settings.Fields.Deposit:     formatKopecks(data.DepositAmount),
		worker.settings.Fields.Delivery:    formatKopecks(data.DeliveryCost),
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
	return strings.Join([]string{
		"Внутренний rental ID: " + data.RentalID,
		"Клиент: " + data.ClientFullName,
		"Телефон: " + data.ClientPhone,
		"Инструмент: " + data.ToolName,
		"Период: " + data.StartDate + " — " + data.EndDate,
		"Аренда: " + formatKopecks(data.RentalPrice) + " RUB",
		"Залог: " + formatKopecks(data.DepositAmount) + " RUB",
		"Доставка: " + data.DeliveryMethod + ", " + formatKopecks(data.DeliveryCost) + " RUB",
		"Адрес: " + firstNonEmpty(data.DeliveryAddress, "не указан"),
	}, "\n")
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

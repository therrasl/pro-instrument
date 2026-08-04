package push

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	defaultChannelID = "rental-updates"
	workerLease      = 2 * time.Minute
	receiptDelay     = 15 * time.Minute
)

var ErrInvalidToken = errors.New("invalid Expo push token")

type TokenRepository interface {
	RegisterToken(context.Context, string, string, string, time.Time) error
	DeleteToken(context.Context, string, string, time.Time) error
}

type DeliveryRepository interface {
	ClaimDelivery(context.Context, time.Time, time.Time, int) (Delivery, bool, error)
	SaveTicket(context.Context, string, string, time.Time) error
	RetryDelivery(context.Context, string, time.Time, string) error
	ClaimReceipt(context.Context, time.Time, time.Time, int) (Receipt, bool, error)
	CompleteReceipt(context.Context, string, time.Time) error
	RetryReceipt(context.Context, string, time.Time, string) error
	DisableInvalidToken(context.Context, string, string, string, time.Time) error
}

type TokenService struct {
	repository TokenRepository
	now        func() time.Time
}

func NewTokenService(repository TokenRepository) *TokenService {
	return &TokenService{repository: repository, now: time.Now}
}

func (service *TokenService) Register(
	ctx context.Context,
	clientID string,
	token string,
	platform string,
) error {
	token = strings.TrimSpace(token)
	platform = strings.TrimSpace(strings.ToLower(platform))
	if !validExpoPushToken(token) || (platform != PlatformAndroid && platform != PlatformIOS) {
		return ErrInvalidToken
	}
	return service.repository.RegisterToken(ctx, clientID, token, platform, service.now().UTC())
}

func (service *TokenService) Delete(ctx context.Context, clientID string, token string) error {
	token = strings.TrimSpace(token)
	if !validExpoPushToken(token) {
		return ErrInvalidToken
	}
	return service.repository.DeleteToken(ctx, clientID, token, service.now().UTC())
}

func validExpoPushToken(token string) bool {
	if len(token) > 512 {
		return false
	}
	return (strings.HasPrefix(token, "ExpoPushToken[") ||
		strings.HasPrefix(token, "ExponentPushToken[")) && strings.HasSuffix(token, "]")
}

type Dispatcher struct {
	repository   DeliveryRepository
	sender       Sender
	pollInterval time.Duration
	retryBase    time.Duration
	maxAttempts  int
	logger       *log.Logger
	now          func() time.Time
}

func NewDispatcher(
	repository DeliveryRepository,
	sender Sender,
	pollInterval time.Duration,
	retryBase time.Duration,
	maxAttempts int,
	logger *log.Logger,
) *Dispatcher {
	return &Dispatcher{
		repository:   repository,
		sender:       sender,
		pollInterval: pollInterval,
		retryBase:    retryBase,
		maxAttempts:  maxAttempts,
		logger:       logger,
		now:          time.Now,
	}
}

func (dispatcher *Dispatcher) Run(ctx context.Context) {
	for {
		processed, err := dispatcher.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			dispatcher.logger.Printf("push worker iteration failed: %v", err)
		}
		if processed {
			continue
		}
		timer := time.NewTimer(dispatcher.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (dispatcher *Dispatcher) RunOnce(ctx context.Context) (bool, error) {
	now := dispatcher.now().UTC()
	delivery, ok, err := dispatcher.repository.ClaimDelivery(
		ctx,
		now,
		now.Add(-workerLease),
		dispatcher.maxAttempts,
	)
	if err != nil {
		return false, err
	}
	if ok {
		message, messageErr := NotificationFor(delivery.Token, delivery.RentalID, delivery.Status)
		if messageErr != nil {
			return true, dispatcher.repository.RetryDelivery(
				ctx, delivery.ID, now.Add(dispatcher.retryDelay(delivery.Attempts)), messageErr.Error(),
			)
		}
		ticketID, sendErr := dispatcher.sender.Send(ctx, message)
		if errors.Is(sendErr, ErrDeviceNotRegistered) {
			return true, dispatcher.repository.DisableInvalidToken(
				ctx, delivery.TokenID, delivery.ID, ErrDeviceNotRegistered.Error(), now,
			)
		}
		if sendErr != nil {
			return true, dispatcher.repository.RetryDelivery(
				ctx, delivery.ID, now.Add(dispatcher.retryDelay(delivery.Attempts)), sendErr.Error(),
			)
		}
		return true, dispatcher.repository.SaveTicket(ctx, delivery.ID, ticketID, now.Add(receiptDelay))
	}

	receipt, ok, err := dispatcher.repository.ClaimReceipt(
		ctx, now, now.Add(-workerLease), dispatcher.maxAttempts,
	)
	if err != nil || !ok {
		return false, err
	}
	ready, receiptErr := dispatcher.sender.CheckReceipt(ctx, receipt.TicketID)
	if errors.Is(receiptErr, ErrDeviceNotRegistered) {
		return true, dispatcher.repository.DisableInvalidToken(
			ctx, receipt.TokenID, receipt.DeliveryID, ErrDeviceNotRegistered.Error(), now,
		)
	}
	if receiptErr != nil {
		return true, dispatcher.repository.RetryReceipt(
			ctx, receipt.DeliveryID, now.Add(dispatcher.retryDelay(receipt.Attempts)), receiptErr.Error(),
		)
	}
	if !ready {
		return true, dispatcher.repository.RetryReceipt(
			ctx, receipt.DeliveryID, now.Add(dispatcher.retryDelay(receipt.Attempts)), "receipt not ready",
		)
	}
	return true, dispatcher.repository.CompleteReceipt(ctx, receipt.DeliveryID, now)
}

func (dispatcher *Dispatcher) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	return dispatcher.retryBase * time.Duration(1<<shift)
}

func NotificationFor(token string, rentalID string, status string) (Message, error) {
	var title string
	var body string
	switch status {
	case "paid":
		title, body = "Оплата получена", "Заказ оплачен и передан в работу."
	case "preparing":
		title, body = "Заказ готовится", "Мы подготавливаем инструмент к выдаче."
	case "ready":
		title, body = "Можно забирать", "Инструмент готов к выдаче."
	case "handed_to_courier":
		title, body = "Заказ у курьера", "Инструмент передан курьеру."
	case "rented":
		title, body = "Аренда началась", "Инструмент передан вам."
	case "awaiting_return":
		title, body = "Пора вернуть инструмент", "Заказ ожидает возврата."
	case "inspection":
		title, body = "Проверяем инструмент", "Инструмент принят и проходит осмотр."
	case "completed":
		title, body = "Аренда завершена", "Спасибо! Заказ успешно завершён."
	case "cancelled":
		title, body = "Заказ отменён", "Заказ был отменён."
	case "payment_expired":
		title, body = "Время оплаты истекло", "Создайте новый заказ, если инструмент ещё нужен."
	default:
		return Message{}, fmt.Errorf("unsupported rental notification status %q", status)
	}
	return Message{
		To:        token,
		Sound:     "default",
		Title:     title,
		Body:      body,
		Data:      map[string]any{"rental_id": rentalID, "status": status},
		ChannelID: defaultChannelID,
		Priority:  "high",
	}, nil
}

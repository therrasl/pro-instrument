package push

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

func TestNotificationForSignificantRentalStatuses(t *testing.T) {
	statuses := []string{
		"paid",
		"preparing",
		"ready",
		"handed_to_courier",
		"rented",
		"awaiting_return",
		"inspection",
		"completed",
		"cancelled",
		"payment_expired",
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			message, err := NotificationFor("ExpoPushToken[test]", "rental-id", status)
			if err != nil {
				t.Fatal(err)
			}
			if message.Title == "" || message.Body == "" ||
				message.Data["rental_id"] != "rental-id" ||
				message.Data["status"] != status ||
				message.ChannelID != "rental-updates" {
				t.Fatalf("unexpected message: %#v", message)
			}
		})
	}
}

func TestDispatcherSendsClaimedDeliveryOnlyOnce(t *testing.T) {
	repository := &fakeDeliveryRepository{delivery: Delivery{
		ID: "delivery-id", TokenID: "token-id", Token: "ExpoPushToken[test]",
		RentalID: "rental-id", Status: "paid", Attempts: 1,
	}}
	sender := &fakeSender{}
	dispatcher := NewDispatcher(
		repository,
		sender,
		time.Millisecond,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	)

	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("first dispatch: processed=%v err=%v", processed, err)
	}
	processed, err = dispatcher.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("duplicate dispatch: processed=%v err=%v", processed, err)
	}
	if sender.calls != 1 || repository.ticketed != 1 {
		t.Fatalf("duplicate notification sent: sends=%d ticketed=%d", sender.calls, repository.ticketed)
	}
}

func TestDispatcherDisablesInvalidToken(t *testing.T) {
	repository := &fakeDeliveryRepository{delivery: Delivery{
		ID: "delivery-id", TokenID: "token-id", Token: "ExpoPushToken[invalid]",
		RentalID: "rental-id", Status: "paid", Attempts: 1,
	}}
	sender := &fakeSender{err: ErrDeviceNotRegistered}
	dispatcher := NewDispatcher(
		repository,
		sender,
		time.Millisecond,
		time.Second,
		5,
		log.New(io.Discard, "", 0),
	)

	_, err := dispatcher.RunOnce(context.Background())
	if err != nil || repository.disabled != 1 {
		t.Fatalf("invalid token was not disabled: disabled=%d err=%v", repository.disabled, err)
	}
}

func TestTokenServiceValidatesExpoTokenAndPlatform(t *testing.T) {
	repository := &fakeTokenRepository{}
	service := NewTokenService(repository)
	if err := service.Register(
		context.Background(),
		"client-id",
		"not-a-push-token",
		"android",
	); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected invalid token error, got %v", err)
	}
	if repository.registered != 0 {
		t.Fatal("invalid token reached repository")
	}
}

type fakeSender struct {
	calls int
	err   error
}

func (sender *fakeSender) Send(context.Context, Message) (string, error) {
	sender.calls++
	return "ticket-id", sender.err
}

func (*fakeSender) CheckReceipt(context.Context, string) (bool, error) {
	return true, nil
}

type fakeDeliveryRepository struct {
	delivery Delivery
	claimed  bool
	ticketed int
	disabled int
}

func (repository *fakeDeliveryRepository) ClaimDelivery(
	context.Context,
	time.Time,
	time.Time,
	int,
) (Delivery, bool, error) {
	if repository.claimed {
		return Delivery{}, false, nil
	}
	repository.claimed = true
	return repository.delivery, true, nil
}

func (repository *fakeDeliveryRepository) SaveTicket(
	context.Context,
	string,
	string,
	time.Time,
) error {
	repository.ticketed++
	return nil
}

func (*fakeDeliveryRepository) RetryDelivery(context.Context, string, time.Time, string) error {
	return nil
}

func (*fakeDeliveryRepository) ClaimReceipt(
	context.Context,
	time.Time,
	time.Time,
	int,
) (Receipt, bool, error) {
	return Receipt{}, false, nil
}

func (*fakeDeliveryRepository) CompleteReceipt(context.Context, string, time.Time) error {
	return nil
}

func (*fakeDeliveryRepository) RetryReceipt(context.Context, string, time.Time, string) error {
	return nil
}

func (repository *fakeDeliveryRepository) DisableInvalidToken(
	context.Context,
	string,
	string,
	string,
	time.Time,
) error {
	repository.disabled++
	return nil
}

type fakeTokenRepository struct {
	registered int
}

func (repository *fakeTokenRepository) RegisterToken(
	context.Context,
	string,
	string,
	string,
	time.Time,
) error {
	repository.registered++
	return nil
}

func (*fakeTokenRepository) DeleteToken(context.Context, string, string, time.Time) error {
	return nil
}

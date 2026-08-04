package rentals

import (
	"context"
	"testing"
	"time"
)

type statusRepositoryStub struct {
	now           time.Time
	holdExpiresAt time.Time
}

func (repository *statusRepositoryStub) ApplyBitrixStatus(
	_ context.Context,
	_ string,
	_ string,
	now time.Time,
	holdExpiresAt time.Time,
) (string, bool, error) {
	repository.now = now
	repository.holdExpiresAt = holdExpiresAt
	return "rental-1", true, nil
}

func TestAwaitingPaymentDeadlineIsExactlyThirtyMinutes(t *testing.T) {
	repository := &statusRepositoryStub{}
	service := NewStatusService(repository, 30*time.Minute)
	service.now = func() time.Time {
		return time.Date(
			2026,
			time.August,
			1,
			10,
			0,
			0,
			0,
			time.FixedZone("MSK", 3*60*60),
		)
	}

	if _, _, err := service.ApplyBitrixStatus(
		context.Background(),
		"deal-1",
		StatusAwaitingPayment,
	); err != nil {
		t.Fatalf("apply awaiting payment: %v", err)
	}
	if repository.holdExpiresAt.Sub(repository.now) != 30*time.Minute {
		t.Fatalf(
			"payment TTL=%s, want %s",
			repository.holdExpiresAt.Sub(repository.now),
			30*time.Minute,
		)
	}
	if repository.now.Location() != time.UTC ||
		repository.holdExpiresAt.Location() != time.UTC {
		t.Fatalf(
			"payment deadline must use UTC: now=%s expires=%s",
			repository.now.Location(),
			repository.holdExpiresAt.Location(),
		)
	}
}

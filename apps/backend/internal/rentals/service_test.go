package rentals

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	testToolID   = "10000000-0000-4000-8000-000000000001"
	testClientID = "40000000-0000-4000-8000-000000000001"
)

type fakeRepository struct {
	getPricing  func(context.Context, string, time.Time, time.Time, time.Time) (ToolPricing, error)
	create      func(context.Context, CreateCommand) (RentalRequest, error)
	list        func(context.Context, string, int, int) ([]RentalRequest, error)
	get         func(context.Context, string, string) (RentalRequest, error)
	cancel      func(context.Context, string, string, time.Time) (RentalRequest, error)
	expireHolds func(context.Context, time.Time) (int64, error)
}

func (repository fakeRepository) GetAvailableToolPricing(
	ctx context.Context,
	toolID string,
	startDate time.Time,
	endDate time.Time,
	now time.Time,
) (ToolPricing, error) {
	return repository.getPricing(ctx, toolID, startDate, endDate, now)
}

func (repository fakeRepository) Create(
	ctx context.Context,
	command CreateCommand,
) (RentalRequest, error) {
	return repository.create(ctx, command)
}

func (repository fakeRepository) ListByClient(
	ctx context.Context,
	clientID string,
	limit int,
	offset int,
) ([]RentalRequest, error) {
	return repository.list(ctx, clientID, limit, offset)
}

func (repository fakeRepository) GetByClient(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	return repository.get(ctx, clientID, rentalID)
}

func (repository fakeRepository) CancelByClient(
	ctx context.Context,
	clientID string,
	rentalID string,
	now time.Time,
) (RentalRequest, error) {
	return repository.cancel(ctx, clientID, rentalID, now)
}

func (repository fakeRepository) ExpireHolds(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	return repository.expireHolds(ctx, now)
}

func TestQuoteCalculatesAmountsInKopecks(t *testing.T) {
	repository := defaultFakeRepository()
	repository.getPricing = func(
		_ context.Context,
		toolID string,
		_ time.Time,
		_ time.Time,
		_ time.Time,
	) (ToolPricing, error) {
		if toolID != testToolID {
			t.Fatalf("unexpected tool id: %q", toolID)
		}
		return ToolPricing{DailyPrice: 50_000, DepositAmount: 200_000}, nil
	}
	service := NewService(repository, 30*time.Minute, 100_000)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	}

	quote, err := service.Quote(context.Background(), QuoteRequest{
		ToolID:          testToolID,
		StartDate:       time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		EndDate:         time.Date(2026, time.August, 4, 0, 0, 0, 0, time.UTC),
		DeliveryMethod:  DeliveryCourier,
		DeliveryAddress: "Москва, ул. Тестовая, 1",
	})
	if err != nil {
		t.Fatalf("quote rental: %v", err)
	}

	if quote.RentalDays != 3 ||
		quote.DailyPrice != 50_000 ||
		quote.RentalPrice != 150_000 ||
		quote.DepositAmount != 200_000 ||
		quote.DeliveryCost != 100_000 ||
		quote.TotalAmount != 450_000 {
		t.Fatalf("unexpected quote: %#v", quote)
	}
}

func TestCourierRequiresAddress(t *testing.T) {
	repository := defaultFakeRepository()
	repository.getPricing = func(
		context.Context,
		string,
		time.Time,
		time.Time,
		time.Time,
	) (ToolPricing, error) {
		t.Fatal("repository must not be called for invalid courier request")
		return ToolPricing{}, nil
	}
	service := NewService(repository, 30*time.Minute, 100_000)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	}

	_, err := service.Quote(context.Background(), QuoteRequest{
		ToolID:         testToolID,
		StartDate:      time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		EndDate:        time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		DeliveryMethod: DeliveryCourier,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestCreateRentalUsesServerCommand(t *testing.T) {
	repository := defaultFakeRepository()
	repository.create = func(_ context.Context, command CreateCommand) (RentalRequest, error) {
		if command.ClientID != testClientID ||
			command.ToolID != testToolID ||
			command.RentalDays != 2 ||
			command.CourierFee != 100_000 ||
			command.DeliveryAddress != nil {
			t.Fatalf("unexpected create command: %#v", command)
		}
		if command.ExpiresAt.Sub(command.Now) != 30*time.Minute {
			t.Fatalf("unexpected hold expiry: %s", command.ExpiresAt.Sub(command.Now))
		}
		return RentalRequest{
			ID:         "50000000-0000-4000-8000-000000000001",
			ClientID:   command.ClientID,
			ToolID:     command.ToolID,
			RentalDays: command.RentalDays,
			Status:     StatusPendingManager,
		}, nil
	}
	service := NewService(repository, 30*time.Minute, 100_000)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	}

	rental, err := service.Create(context.Background(), testClientID, QuoteRequest{
		ToolID:         testToolID,
		StartDate:      time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		EndDate:        time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		DeliveryMethod: DeliverySelfPickup,
	})
	if err != nil {
		t.Fatalf("create rental: %v", err)
	}
	if rental.Status != StatusPendingManager || rental.ClientID != testClientID {
		t.Fatalf("unexpected rental: %#v", rental)
	}
}

func TestQuoteReturnsNoAvailableUnit(t *testing.T) {
	repository := defaultFakeRepository()
	repository.getPricing = func(
		context.Context,
		string,
		time.Time,
		time.Time,
		time.Time,
	) (ToolPricing, error) {
		return ToolPricing{}, ErrNoAvailableUnit
	}
	service := NewService(repository, 30*time.Minute, 0)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	}

	_, err := service.Quote(context.Background(), QuoteRequest{
		ToolID:         testToolID,
		StartDate:      time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		EndDate:        time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		DeliveryMethod: DeliverySelfPickup,
	})
	if !errors.Is(err, ErrNoAvailableUnit) {
		t.Fatalf("expected no available unit, got %v", err)
	}
}

func TestGetRentalKeepsClientScope(t *testing.T) {
	repository := defaultFakeRepository()
	repository.get = func(
		_ context.Context,
		clientID string,
		rentalID string,
	) (RentalRequest, error) {
		if clientID != testClientID || rentalID != "50000000-0000-4000-8000-000000000001" {
			t.Fatalf("unexpected ownership scope: client=%q rental=%q", clientID, rentalID)
		}
		return RentalRequest{}, ErrRentalNotFound
	}
	service := NewService(repository, 30*time.Minute, 0)

	_, err := service.Get(
		context.Background(),
		testClientID,
		"50000000-0000-4000-8000-000000000001",
	)
	if !errors.Is(err, ErrRentalNotFound) {
		t.Fatalf("expected scoped not found, got %v", err)
	}
}

func TestCancelRentalUsesOwnerScopeAndUTC(t *testing.T) {
	repository := defaultFakeRepository()
	expectedNow := time.Date(2026, time.August, 1, 7, 0, 0, 0, time.UTC)
	repository.cancel = func(
		_ context.Context,
		clientID string,
		rentalID string,
		now time.Time,
	) (RentalRequest, error) {
		if clientID != testClientID ||
			rentalID != "50000000-0000-4000-8000-000000000001" ||
			!now.Equal(expectedNow) ||
			now.Location() != time.UTC {
			t.Fatalf(
				"unexpected cancel scope: client=%q rental=%q now=%s",
				clientID,
				rentalID,
				now,
			)
		}
		return RentalRequest{ID: rentalID, Status: StatusCancelled}, nil
	}
	service := NewService(repository, 30*time.Minute, 0)
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

	rental, err := service.Cancel(
		context.Background(),
		testClientID,
		"50000000-0000-4000-8000-000000000001",
	)
	if err != nil || rental.Status != StatusCancelled {
		t.Fatalf("cancel rental: rental=%#v err=%v", rental, err)
	}
}

func defaultFakeRepository() fakeRepository {
	return fakeRepository{
		getPricing: func(
			context.Context,
			string,
			time.Time,
			time.Time,
			time.Time,
		) (ToolPricing, error) {
			return ToolPricing{}, nil
		},
		create: func(context.Context, CreateCommand) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
		list: func(context.Context, string, int, int) ([]RentalRequest, error) {
			return nil, nil
		},
		get: func(context.Context, string, string) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
		cancel: func(context.Context, string, string, time.Time) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
		expireHolds: func(context.Context, time.Time) (int64, error) {
			return 0, nil
		},
	}
}

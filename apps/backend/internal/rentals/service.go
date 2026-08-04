package rentals

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalidInput         = errors.New("invalid rental input")
	ErrOnboardingIncomplete = errors.New("rental onboarding is incomplete")
	ErrToolNotFound         = errors.New("tool not found")
	ErrNoAvailableUnit      = errors.New("no tool unit is available")
	ErrRentalNotFound       = errors.New("rental request not found")
	ErrRentalNotCancellable = errors.New("rental request cannot be cancelled")
)

type Repository interface {
	GetAvailableToolPricing(
		context.Context,
		string,
		time.Time,
		time.Time,
		time.Time,
	) (ToolPricing, error)
	Create(context.Context, CreateCommand) (RentalRequest, error)
	ListByClient(context.Context, string, int, int) ([]RentalRequest, error)
	GetByClient(context.Context, string, string) (RentalRequest, error)
	CancelByClient(context.Context, string, string, time.Time) (RentalRequest, error)
	ExpireHolds(context.Context, time.Time) (int64, error)
}

type Service struct {
	repository Repository
	holdTTL    time.Duration
	courierFee int64
	now        func() time.Time
}

func NewService(repository Repository, holdTTL time.Duration, courierFee int64) *Service {
	return &Service{
		repository: repository,
		holdTTL:    holdTTL,
		courierFee: courierFee,
		now:        time.Now,
	}
}

func (service *Service) Quote(ctx context.Context, input QuoteRequest) (Quote, error) {
	now := service.now().UTC()
	input, days, err := service.validate(input, now)
	if err != nil {
		return Quote{}, err
	}

	pricing, err := service.repository.GetAvailableToolPricing(
		ctx,
		input.ToolID,
		input.StartDate,
		input.EndDate,
		now,
	)
	if err != nil {
		return Quote{}, err
	}

	return calculateQuote(input, days, pricing, service.courierFee)
}

func (service *Service) Create(
	ctx context.Context,
	clientID string,
	input QuoteRequest,
) (RentalRequest, error) {
	now := service.now().UTC()
	input, days, err := service.validate(input, now)
	if err != nil {
		return RentalRequest{}, err
	}

	var deliveryAddress *string
	if input.DeliveryMethod == DeliveryCourier {
		deliveryAddress = &input.DeliveryAddress
	}

	return service.repository.Create(ctx, CreateCommand{
		ClientID:        clientID,
		ToolID:          input.ToolID,
		StartDate:       input.StartDate,
		EndDate:         input.EndDate,
		RentalDays:      days,
		DeliveryMethod:  input.DeliveryMethod,
		DeliveryAddress: deliveryAddress,
		CourierFee:      service.courierFee,
		ExpiresAt:       now.Add(service.holdTTL),
		Now:             now,
	})
}

func (service *Service) List(
	ctx context.Context,
	clientID string,
	limit int,
	offset int,
) ([]RentalRequest, error) {
	if _, err := service.repository.ExpireHolds(ctx, service.now().UTC()); err != nil {
		return nil, err
	}
	return service.repository.ListByClient(ctx, clientID, limit, offset)
}

func (service *Service) Get(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	if _, err := service.repository.ExpireHolds(ctx, service.now().UTC()); err != nil {
		return RentalRequest{}, err
	}
	return service.repository.GetByClient(ctx, clientID, rentalID)
}

func (service *Service) Cancel(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	return service.repository.CancelByClient(
		ctx,
		clientID,
		rentalID,
		service.now().UTC(),
	)
}

func (service *Service) ExpireHolds(ctx context.Context) (int64, error) {
	return service.repository.ExpireHolds(ctx, service.now().UTC())
}

func (service *Service) validate(
	input QuoteRequest,
	now time.Time,
) (QuoteRequest, int, error) {
	input.ToolID = strings.TrimSpace(input.ToolID)
	input.DeliveryMethod = strings.TrimSpace(input.DeliveryMethod)
	input.DeliveryAddress = strings.TrimSpace(input.DeliveryAddress)

	if input.ToolID == "" ||
		input.StartDate.IsZero() ||
		input.EndDate.IsZero() ||
		input.EndDate.Before(input.StartDate) {
		return QuoteRequest{}, 0, ErrInvalidInput
	}
	if input.StartDate.Before(dateOnly(now)) {
		return QuoteRequest{}, 0, ErrInvalidInput
	}
	switch input.DeliveryMethod {
	case DeliverySelfPickup:
		input.DeliveryAddress = ""
	case DeliveryCourier:
		if input.DeliveryAddress == "" || len([]rune(input.DeliveryAddress)) > 1000 {
			return QuoteRequest{}, 0, ErrInvalidInput
		}
	default:
		return QuoteRequest{}, 0, ErrInvalidInput
	}

	days64 := int64(input.EndDate.Sub(input.StartDate)/(24*time.Hour)) + 1
	if days64 < 1 || days64 > math.MaxInt32 {
		return QuoteRequest{}, 0, ErrInvalidInput
	}
	return input, int(days64), nil
}

func calculateQuote(
	input QuoteRequest,
	days int,
	pricing ToolPricing,
	courierFee int64,
) (Quote, error) {
	if pricing.DailyPrice < 0 || pricing.DepositAmount < 0 || courierFee < 0 {
		return Quote{}, ErrInvalidInput
	}
	if pricing.DailyPrice != 0 && int64(days) > math.MaxInt64/pricing.DailyPrice {
		return Quote{}, ErrInvalidInput
	}
	rentalPrice := pricing.DailyPrice * int64(days)
	deliveryCost := int64(0)
	if input.DeliveryMethod == DeliveryCourier {
		deliveryCost = courierFee
	}
	if pricing.DepositAmount > math.MaxInt64-rentalPrice ||
		deliveryCost > math.MaxInt64-rentalPrice-pricing.DepositAmount {
		return Quote{}, ErrInvalidInput
	}

	return Quote{
		ToolID:         input.ToolID,
		StartDate:      input.StartDate.Format(time.DateOnly),
		EndDate:        input.EndDate.Format(time.DateOnly),
		RentalDays:     days,
		DailyPrice:     pricing.DailyPrice,
		RentalPrice:    rentalPrice,
		DepositAmount:  pricing.DepositAmount,
		DeliveryCost:   deliveryCost,
		TotalAmount:    rentalPrice + pricing.DepositAmount + deliveryCost,
		DeliveryMethod: input.DeliveryMethod,
	}, nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

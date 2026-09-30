package orderdocs

import (
	"context"
	"errors"
	"log"
	"time"
)

type Service struct {
	repository Repository
	generator  *Generator
	logger     *log.Logger
}

func NewService(repository Repository, generator *Generator, logger *log.Logger) *Service {
	return &Service{repository: repository, generator: generator, logger: logger}
}

func (service *Service) EnsureRental(ctx context.Context, rentalID string) error {
	data, err := service.repository.GetRental(ctx, rentalID)
	if errors.Is(err, ErrRentalNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	existing, err := service.repository.ListDocumentTypes(ctx, rentalID)
	if err != nil {
		return err
	}
	for _, kind := range documentTypesFor(data.Status, data.HasSuccessfulPayment) {
		if existing[kind] {
			continue
		}
		document, err := service.generator.Generate(data, kind)
		if err != nil {
			return err
		}
		if err := service.repository.SaveDocument(ctx, data, document); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := service.ensureAll(ctx); err != nil {
			service.logger.Printf("order documents sync failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (service *Service) ensureAll(ctx context.Context) error {
	ids, err := service.repository.ListRentalIDs(ctx, 500)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := service.EnsureRental(ctx, id); err != nil {
			service.logger.Printf("order documents rental %s failed: %v", id, err)
		}
	}
	return nil
}

func documentTypesFor(status string, paid bool) []string {
	result := []string{RentalContract, Invoice}
	if paid {
		result = append(result, PaymentReceipt)
	}
	switch status {
	case "rented", "awaiting_return", "inspection", "completed":
		result = append(result, TransferAct)
	}
	if status == "completed" {
		result = append(result, ReturnAct, ClosingDocument)
	}
	return result
}

package bitrix

import (
	"context"
	"time"
)

type ReconcileRepository interface {
	Reconcile(context.Context, time.Time) (int64, error)
}

type ReconciliationService struct {
	repository ReconcileRepository
	now        func() time.Time
}

func NewReconciliationService(repository ReconcileRepository) *ReconciliationService {
	return &ReconciliationService{repository: repository, now: time.Now}
}

func (service *ReconciliationService) Run(ctx context.Context) (int64, error) {
	return service.repository.Reconcile(ctx, service.now().UTC())
}

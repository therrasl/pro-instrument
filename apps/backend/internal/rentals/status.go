package rentals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidStatusTransition = errors.New("invalid rental status transition")
	ErrRentalHoldNotFound      = errors.New("rental tool hold not found")
	ErrRentalHoldExpired       = errors.New("rental tool hold expired")
	ErrRentalHoldNotActive     = errors.New("rental tool hold is not active")
)

type StatusRepository interface {
	ApplyBitrixStatus(
		context.Context,
		string,
		string,
		time.Time,
		time.Time,
	) (string, bool, error)
}

type StatusService struct {
	repository StatusRepository
	holdTTL    time.Duration
	now        func() time.Time
}

func NewStatusService(repository StatusRepository, holdTTL time.Duration) *StatusService {
	return &StatusService{
		repository: repository,
		holdTTL:    holdTTL,
		now:        time.Now,
	}
}

func (service *StatusService) ApplyBitrixStatus(
	ctx context.Context,
	bitrixDealID string,
	targetStatus string,
) (string, bool, error) {
	now := service.now().UTC()
	return service.repository.ApplyBitrixStatus(
		ctx,
		bitrixDealID,
		targetStatus,
		now,
		now.Add(service.holdTTL),
	)
}

func (repository *PostgresRepository) ApplyBitrixStatus(
	ctx context.Context,
	bitrixDealID string,
	targetStatus string,
	now time.Time,
	holdExpiresAt time.Time,
) (string, bool, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("begin Bitrix rental transition: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var rentalID string
	var currentStatus string
	err = transaction.QueryRow(
		ctx,
		`SELECT rr.id::text, rr.status
		 FROM rental_requests AS rr
		 WHERE rr.bitrix_deal_id = $1
		 FOR UPDATE OF rr`,
		bitrixDealID,
	).Scan(&rentalID, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrRentalNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("lock rental for Bitrix transition: %w", err)
	}

	if currentStatus == targetStatus {
		if err := transaction.Commit(ctx); err != nil {
			return "", false, fmt.Errorf("commit idempotent Bitrix transition: %w", err)
		}
		return rentalID, false, nil
	}
	if !validStatusTransition(currentStatus, targetStatus) {
		return "", false, ErrInvalidStatusTransition
	}

	switch targetStatus {
	case StatusAwaitingPayment:
		var holdStatus string
		var currentHoldExpiresAt time.Time
		err := transaction.QueryRow(
			ctx,
			`SELECT status, expires_at
			 FROM tool_holds
			 WHERE rental_request_id = $1::uuid
			 FOR UPDATE`,
			rentalID,
		).Scan(&holdStatus, &currentHoldExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, ErrRentalHoldNotFound
		}
		if err != nil {
			return "", false, fmt.Errorf("lock rental hold for Bitrix transition: %w", err)
		}
		if holdStatus == "expired" || !currentHoldExpiresAt.After(now) {
			return "", false, ErrRentalHoldExpired
		}
		if holdStatus != "active" {
			return "", false, ErrRentalHoldNotActive
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tool_holds
			 SET status = 'confirmed', expires_at = $2, updated_at = $3
			 WHERE rental_request_id = $1::uuid`,
			rentalID,
			holdExpiresAt,
			now,
		); err != nil {
			return "", false, fmt.Errorf("confirm rental hold: %w", err)
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE rental_requests
			 SET status = $2, expires_at = $3, updated_at = $4
			 WHERE id = $1::uuid`,
			rentalID,
			targetStatus,
			holdExpiresAt,
			now,
		); err != nil {
			return "", false, fmt.Errorf("open rental payment: %w", err)
		}
	default:
		if _, err := transaction.Exec(
			ctx,
			`UPDATE rental_requests SET status = $2, updated_at = $3 WHERE id = $1::uuid`,
			rentalID,
			targetStatus,
			now,
		); err != nil {
			return "", false, fmt.Errorf("update rental from Bitrix: %w", err)
		}
	}

	if targetStatus == StatusRejected || targetStatus == StatusCompleted {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tool_holds
			 SET status = 'released', updated_at = $2
			 WHERE rental_request_id = $1::uuid
			   AND status IN ('active', 'confirmed')`,
			rentalID,
			now,
		); err != nil {
			return "", false, fmt.Errorf("release rental hold: %w", err)
		}
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO rental_status_history (
			rental_request_id,
			from_status,
			to_status,
			actor_type,
			actor_id,
			created_at
		)
		VALUES ($1::uuid, $2, $3, 'integration', $4, $5)`,
		rentalID,
		currentStatus,
		targetStatus,
		bitrixDealID,
		now,
	); err != nil {
		return "", false, fmt.Errorf("record Bitrix rental transition: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("commit Bitrix rental transition: %w", err)
	}
	return rentalID, true, nil
}

func validStatusTransition(currentStatus string, targetStatus string) bool {
	if targetStatus == StatusRejected {
		switch currentStatus {
		case StatusPendingManager,
			StatusAwaitingPayment,
			StatusPaid,
			StatusPreparing,
			StatusReady,
			StatusHandedToCourier,
			StatusRented,
			StatusAwaitingReturn,
			StatusInspection:
			return true
		default:
			return false
		}
	}

	switch currentStatus {
	case StatusPendingManager:
		return targetStatus == StatusAwaitingPayment
	case StatusAwaitingPayment:
		return targetStatus == StatusPaid
	case StatusPaid:
		return targetStatus == StatusPreparing
	case StatusPreparing:
		return targetStatus == StatusReady
	case StatusReady:
		return targetStatus == StatusHandedToCourier || targetStatus == StatusRented
	case StatusHandedToCourier:
		return targetStatus == StatusRented
	case StatusRented:
		return targetStatus == StatusAwaitingReturn
	case StatusAwaitingReturn:
		return targetStatus == StatusInspection
	case StatusInspection:
		return targetStatus == StatusCompleted
	default:
		return false
	}
}

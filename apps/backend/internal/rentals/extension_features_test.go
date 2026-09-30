package rentals

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigration17ContainsRentalExtensionAndInspectionPhotosSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000017_rental_extensions_and_inspection_photos.up.sql"))
	if err != nil {
		t.Fatalf("read migration 17: %v", err)
	}
	content := string(data)
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS rental_extensions",
		"previous_end_date DATE NOT NULL",
		"new_end_date DATE NOT NULL",
		"additional_days INTEGER NOT NULL",
		"daily_price BIGINT NOT NULL",
		"amount BIGINT NOT NULL",
		"payment_type TEXT NOT NULL DEFAULT 'initial'",
		"extension_id UUID REFERENCES rental_extensions(id)",
		"CREATE TABLE IF NOT EXISTS rental_inspection_photos",
		"phase TEXT NOT NULL",
		"photo_type TEXT NOT NULL",
		"UNIQUE (rental_request_id, phase, photo_type)",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("migration 17 missing expected fragment: %q", expected)
		}
	}
}

type fakeExtensionPaymentCreator struct {
	createFn func(ctx context.Context, clientID, rentalID, extensionID string, amount int64, description string) (string, string, error)
}

func (f *fakeExtensionPaymentCreator) CreateExtensionPayment(ctx context.Context, clientID, rentalID, extensionID string, amount int64, description string) (string, string, error) {
	if f.createFn != nil {
		return f.createFn(ctx, clientID, rentalID, extensionID, amount, description)
	}
	return "pay-123", "https://yookassa.ru/confirm/123", nil
}

type fakeInspectionPhotoNotifier struct {
	notifiedDeals []string
}

func (f *fakeInspectionPhotoNotifier) NotifyInspectionPhotos(ctx context.Context, dealID string, phase string, orderNumber string, photos []InspectionPhoto) error {
	f.notifiedDeals = append(f.notifiedDeals, dealID)
	return nil
}

func TestQuoteExtensionValidationAndCalculation(t *testing.T) {
	repo := defaultFakeRepository()

	activeRental := RentalRequest{
		ID:            testRentalID,
		ClientID:      testClientID,
		ToolID:        testToolID,
		StartDate:     "2026-09-20",
		EndDate:       "2026-09-25",
		RentalDays:    5,
		RentalPrice:   25_000, // 5_000 per day
		TotalAmount:   35_000,
		DepositAmount: 10_000,
		Status:        StatusRented,
	}

	repo.get = func(ctx context.Context, clientID, rentalID string) (RentalRequest, error) {
		return activeRental, nil
	}

	service := NewService(repo, 30*time.Minute, 100_000)
	service.now = func() time.Time {
		return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	}

	// 1. Success: extend by 3 days (to 2026-09-28)
	quote, err := service.QuoteExtension(context.Background(), testClientID, testRentalID, "2026-09-28")
	if err != nil {
		t.Fatalf("unexpected error quoting extension: %v", err)
	}
	if quote.AdditionalDays != 3 {
		t.Errorf("expected 3 additional days, got %d", quote.AdditionalDays)
	}
	if quote.DailyPrice != 5_000 {
		t.Errorf("expected daily price 5000, got %d", quote.DailyPrice)
	}
	if quote.Amount != 15_000 {
		t.Errorf("expected amount 15000, got %d", quote.Amount)
	}
	if quote.NewEndDate != "2026-09-28" {
		t.Errorf("expected new end date 2026-09-28, got %s", quote.NewEndDate)
	}

	// 2. Reject: new end date not after current end date
	_, err = service.QuoteExtension(context.Background(), testClientID, testRentalID, "2026-09-25")
	if err != ErrInvalidExtensionDate {
		t.Errorf("expected ErrInvalidExtensionDate, got %v", err)
	}

	// 3. Tool unit is not available (collision)
	collisionRepo := repo
	collisionRepo.checkExtensionAvailability = func(ctx context.Context, toolID string, start, end time.Time, unitID string, now time.Time) (bool, error) {
		return false, nil
	}
	service.repository = collisionRepo
	quoteUnavailable, err := service.QuoteExtension(context.Background(), testClientID, testRentalID, "2026-09-28")
	if err != nil {
		t.Fatalf("unexpected error quoting extension: %v", err)
	}
	if quoteUnavailable.Available {
		t.Errorf("expected Available=false, got true")
	}

	_, err = service.CreateExtension(context.Background(), testClientID, testRentalID, "2026-09-28")
	if err != ErrExtensionUnavailable {
		t.Errorf("expected ErrExtensionUnavailable, got %v", err)
	}
}

func TestUploadInspectionPhoto(t *testing.T) {
	tempDir := t.TempDir()
	repo := defaultFakeRepository()

	dealID := "12345"
	activeRental := RentalRequest{
		ID:           testRentalID,
		ClientID:     testClientID,
		Status:       StatusRented,
		BitrixDealID: &dealID,
	}
	repo.get = func(ctx context.Context, clientID, rentalID string) (RentalRequest, error) {
		return activeRental, nil
	}

	var savedPhoto InspectionPhoto
	repo.saveInspectionPhoto = func(ctx context.Context, photo InspectionPhoto) (InspectionPhoto, error) {
		savedPhoto = photo
		savedPhoto.ID = "photo-100"
		return savedPhoto, nil
	}
	repo.listInspectionPhotos = func(ctx context.Context, clientID, rentalID string) ([]InspectionPhoto, error) {
		return []InspectionPhoto{savedPhoto}, nil
	}

	service := NewService(repo, 30*time.Minute, 100_000, "", tempDir)
	notifier := &fakeInspectionPhotoNotifier{}
	service.SetInspectionPhotoNotifier(notifier)

	// Valid upload
	photoData := []byte("fake image data")
	photo, err := service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		PhotoPhaseHandover,
		PhotoTypeBody,
		"image/jpeg",
		"camera.jpg",
		"Looks good",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if err != nil {
		t.Fatalf("unexpected upload error: %v", err)
	}
	if photo.ID != "photo-100" {
		t.Errorf("expected photo-100, got %s", photo.ID)
	}
	if len(notifier.notifiedDeals) != 1 || notifier.notifiedDeals[0] != "12345" {
		t.Errorf("expected deal 12345 notified, got %v", notifier.notifiedDeals)
	}

	// Verify file was written
	onDisk, err := os.ReadFile(filepath.Join(tempDir, photo.StorageKey))
	if err != nil {
		t.Fatalf("read saved photo from disk: %v", err)
	}
	if !bytes.Equal(onDisk, photoData) {
		t.Errorf("written photo content mismatch")
	}

	// Invalid phase reject
	_, err = service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		"invalid_phase",
		PhotoTypeBody,
		"image/jpeg",
		"camera.jpg",
		"",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if err != ErrInvalidPhotoPhase {
		t.Errorf("expected ErrInvalidPhotoPhase, got %v", err)
	}

	// Invalid type reject
	_, err = service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		PhotoPhaseHandover,
		"invalid_type",
		"image/jpeg",
		"camera.jpg",
		"",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if err != ErrInvalidPhotoType {
		t.Errorf("expected ErrInvalidPhotoType, got %v", err)
	}

	// In status rented, return phase must be rejected
	_, err = service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		PhotoPhaseReturn,
		PhotoTypeBody,
		"image/jpeg",
		"camera.jpg",
		"",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if !errors.Is(err, ErrPhotoPhaseNotAllowed) {
		t.Errorf("expected ErrPhotoPhaseNotAllowed for return phase on rented tool, got %v", err)
	}

	// In status awaiting_return, return phase allowed, handover phase rejected
	activeRental.Status = StatusAwaitingReturn
	_, err = service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		PhotoPhaseHandover,
		PhotoTypeBody,
		"image/jpeg",
		"camera.jpg",
		"",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if !errors.Is(err, ErrPhotoPhaseNotAllowed) {
		t.Errorf("expected ErrPhotoPhaseNotAllowed for handover phase during return, got %v", err)
	}

	_, err = service.UploadInspectionPhoto(
		context.Background(),
		testClientID,
		testRentalID,
		PhotoPhaseReturn,
		PhotoTypeBody,
		"image/jpeg",
		"camera.jpg",
		"",
		int64(len(photoData)),
		bytes.NewReader(photoData),
	)
	if err != nil {
		t.Errorf("expected success for return phase during return, got %v", err)
	}
}

package rentals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
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
	ErrDocumentNotFound     = errors.New("order document not found")
	ErrRentalNotActive      = errors.New("rental is not active for extension")
	ErrInvalidExtensionDate = errors.New("new end date must be after current end date")
	ErrExtensionUnavailable = errors.New("tool is not available for requested extension period")
	ErrInvalidPhotoPhase    = errors.New("invalid inspection photo phase")
	ErrPhotoPhaseNotAllowed = errors.New("inspection photo phase is not allowed for current rental status")
	ErrInvalidPhotoType     = errors.New("invalid inspection photo type")
	ErrInvalidPhotoFile     = errors.New("invalid inspection photo file")
	ErrPhotoNotFound        = errors.New("inspection photo not found")
)

type ExtensionPaymentCreator interface {
	CreateExtensionPayment(ctx context.Context, clientID, rentalID, extensionID string, amount int64, description string) (string, string, error)
}

type InspectionPhotoNotifier interface {
	NotifyInspectionPhotos(ctx context.Context, dealID string, phase string, orderNumber string, photos []InspectionPhoto) error
}

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
	ListDocumentsByClient(context.Context, string, string) ([]OrderDocument, error)
	GetDocumentByClient(context.Context, string, string, string) (OrderDocument, error)
	ExpireHolds(context.Context, time.Time) (int64, error)
	GetActiveRentalForExtension(context.Context, string, string) (RentalRequest, error)
	CheckExtensionAvailability(context.Context, string, time.Time, time.Time, string, time.Time) (bool, error)
	CreateExtension(context.Context, string, time.Time, time.Time, int, int64, int64, time.Time) (RentalExtension, error)
	GetExtension(context.Context, string) (RentalExtension, error)
	ListExtensionsByRental(context.Context, string, string) ([]RentalExtension, error)
	UpdateExtensionPaymentID(context.Context, string, string) error
	SaveInspectionPhoto(context.Context, InspectionPhoto) (InspectionPhoto, error)
	ListInspectionPhotos(context.Context, string, string) ([]InspectionPhoto, error)
	GetInspectionPhoto(context.Context, string, string, string) (InspectionPhoto, error)
}

type Service struct {
	repository              Repository
	holdTTL                 time.Duration
	courierFee              int64
	pickupAddress           string
	now                     func() time.Time
	storagePath             string
	documentGenerator       interface {
		EnsureRental(context.Context, string) error
	}
	extensionPaymentCreator ExtensionPaymentCreator
	photoNotifier           InspectionPhotoNotifier
}

func (service *Service) SetDocumentGenerator(generator interface {
	EnsureRental(context.Context, string) error
}) {
	service.documentGenerator = generator
}

func (service *Service) SetExtensionPaymentCreator(creator ExtensionPaymentCreator) {
	service.extensionPaymentCreator = creator
}

func (service *Service) SetInspectionPhotoNotifier(notifier InspectionPhotoNotifier) {
	service.photoNotifier = notifier
}

func NewService(repository Repository, holdTTL time.Duration, courierFee int64, optional ...string) *Service {
	pickupAddress := ""
	storagePath := "./storage"
	if len(optional) > 0 {
		pickupAddress = optional[0]
	}
	if len(optional) > 1 && strings.TrimSpace(optional[1]) != "" {
		storagePath = optional[1]
	}
	return &Service{
		repository:    repository,
		holdTTL:       holdTTL,
		courierFee:    courierFee,
		pickupAddress: strings.TrimSpace(pickupAddress),
		now:           time.Now,
		storagePath:   storagePath,
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

	quote, err := calculateQuote(input, days, pricing, service.courierFee)
	if err == nil && input.DeliveryMethod == DeliverySelfPickup {
		quote.PickupAddress = service.pickupAddress
	}
	return quote, err
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

	rental, err := service.repository.Create(ctx, CreateCommand{
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
	if err == nil && service.documentGenerator != nil {
		_ = service.documentGenerator.EnsureRental(ctx, rental.ID)
	}
	return service.withPickupAddress(rental), err
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
	rentals, err := service.repository.ListByClient(ctx, clientID, limit, offset)
	for index := range rentals {
		rentals[index] = service.withPickupAddress(rentals[index])
	}
	return rentals, err
}

func (service *Service) Get(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	if _, err := service.repository.ExpireHolds(ctx, service.now().UTC()); err != nil {
		return RentalRequest{}, err
	}
	rental, err := service.repository.GetByClient(ctx, clientID, rentalID)
	return service.withPickupAddress(rental), err
}

func (service *Service) Cancel(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	rental, err := service.repository.CancelByClient(
		ctx,
		clientID,
		rentalID,
		service.now().UTC(),
	)
	return service.withPickupAddress(rental), err
}

func (service *Service) ListDocuments(ctx context.Context, clientID string, rentalID string) ([]OrderDocument, error) {
	if service.documentGenerator != nil {
		_ = service.documentGenerator.EnsureRental(ctx, rentalID)
	}
	documents, err := service.repository.ListDocumentsByClient(ctx, clientID, rentalID)
	for index := range documents {
		documents[index].DownloadURL = "/api/v1/rentals/" + url.PathEscape(rentalID) + "/documents/" + url.PathEscape(documents[index].ID) + "/download"
	}
	return documents, err
}

func (service *Service) OpenDocument(ctx context.Context, clientID, rentalID, documentID string) (DocumentContent, error) {
	if service.documentGenerator != nil {
		_ = service.documentGenerator.EnsureRental(ctx, rentalID)
	}
	document, err := service.repository.GetDocumentByClient(ctx, clientID, rentalID, documentID)
	if err != nil {
		return DocumentContent{}, err
	}
	if document.StorageKey == nil {
		return DocumentContent{}, ErrDocumentNotFound
	}
	key := filepath.Clean(strings.TrimSpace(*document.StorageKey))
	if key == "." || filepath.IsAbs(key) || key == ".." || strings.HasPrefix(key, ".."+string(filepath.Separator)) {
		return DocumentContent{}, ErrDocumentNotFound
	}
	root, err := filepath.Abs(service.storagePath)
	if err != nil {
		return DocumentContent{}, err
	}
	path := filepath.Join(root, key)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return DocumentContent{}, ErrDocumentNotFound
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return DocumentContent{}, ErrDocumentNotFound
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return DocumentContent{}, ErrDocumentNotFound
	}
	resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return DocumentContent{}, ErrDocumentNotFound
	}
	file, err := os.Open(resolvedPath)
	if errors.Is(err, os.ErrNotExist) {
		return DocumentContent{}, ErrDocumentNotFound
	}
	if err != nil {
		return DocumentContent{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return DocumentContent{}, ErrDocumentNotFound
	}
	return DocumentContent{Document: document, File: file, Size: info.Size()}, nil
}

func (service *Service) withPickupAddress(rental RentalRequest) RentalRequest {
	if rental.DeliveryMethod == DeliverySelfPickup {
		rental.PickupAddress = service.pickupAddress
	}
	return rental
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

func (service *Service) QuoteExtension(
	ctx context.Context,
	clientID string,
	rentalID string,
	newEndDateStr string,
) (ExtensionQuote, error) {
	now := service.now().UTC()
	rental, err := service.repository.GetActiveRentalForExtension(ctx, clientID, rentalID)
	if err != nil {
		return ExtensionQuote{}, err
	}

	newEndDate, err := time.Parse(time.DateOnly, strings.TrimSpace(newEndDateStr))
	if err != nil {
		return ExtensionQuote{}, fmt.Errorf("%w: invalid date format", ErrInvalidInput)
	}

	currentEndDate, err := time.Parse(time.DateOnly, rental.EndDate)
	if err != nil {
		return ExtensionQuote{}, fmt.Errorf("parse rental end date: %w", err)
	}

	if !newEndDate.After(currentEndDate) {
		return ExtensionQuote{}, ErrInvalidExtensionDate
	}

	additionalDays := int(newEndDate.Sub(currentEndDate) / (24 * time.Hour))
	if additionalDays < 1 {
		return ExtensionQuote{}, ErrInvalidExtensionDate
	}

	dailyPrice := rental.RentalPrice
	if rental.RentalDays > 0 {
		dailyPrice = rental.RentalPrice / int64(rental.RentalDays)
	}
	amount := dailyPrice * int64(additionalDays)

	available, err := service.repository.CheckExtensionAvailability(
		ctx,
		rental.ToolUnitID,
		currentEndDate,
		newEndDate,
		rental.ID,
		now,
	)
	if err != nil {
		return ExtensionQuote{}, err
	}

	return ExtensionQuote{
		RentalID:       rental.ID,
		OrderNumber:    rental.OrderNumber,
		ToolID:         rental.ToolID,
		CurrentEndDate: rental.EndDate,
		NewEndDate:     newEndDate.Format(time.DateOnly),
		AdditionalDays: additionalDays,
		DailyPrice:     dailyPrice,
		Amount:         amount,
		Available:      available,
	}, nil
}

func (service *Service) CreateExtension(
	ctx context.Context,
	clientID string,
	rentalID string,
	newEndDateStr string,
) (RentalExtension, error) {
	now := service.now().UTC()
	rental, err := service.repository.GetActiveRentalForExtension(ctx, clientID, rentalID)
	if err != nil {
		return RentalExtension{}, err
	}

	newEndDate, err := time.Parse(time.DateOnly, strings.TrimSpace(newEndDateStr))
	if err != nil {
		return RentalExtension{}, fmt.Errorf("%w: invalid date format", ErrInvalidInput)
	}

	currentEndDate, err := time.Parse(time.DateOnly, rental.EndDate)
	if err != nil {
		return RentalExtension{}, fmt.Errorf("parse rental end date: %w", err)
	}

	if !newEndDate.After(currentEndDate) {
		return RentalExtension{}, ErrInvalidExtensionDate
	}

	available, err := service.repository.CheckExtensionAvailability(
		ctx,
		rental.ToolUnitID,
		currentEndDate,
		newEndDate,
		rental.ID,
		now,
	)
	if err != nil {
		return RentalExtension{}, err
	}
	if !available {
		return RentalExtension{}, ErrExtensionUnavailable
	}

	additionalDays := int(newEndDate.Sub(currentEndDate) / (24 * time.Hour))
	if additionalDays < 1 {
		return RentalExtension{}, ErrInvalidExtensionDate
	}

	dailyPrice := rental.RentalPrice
	if rental.RentalDays > 0 {
		dailyPrice = rental.RentalPrice / int64(rental.RentalDays)
	}
	amount := dailyPrice * int64(additionalDays)

	ext, err := service.repository.CreateExtension(
		ctx,
		rental.ID,
		currentEndDate,
		newEndDate,
		additionalDays,
		dailyPrice,
		amount,
		now,
	)
	if err != nil {
		return RentalExtension{}, err
	}

	if service.extensionPaymentCreator != nil {
		orderNum := rental.OrderNumber
		if orderNum == "" {
			orderNum = rental.ID
		}
		desc := fmt.Sprintf("Продление аренды заказа %s до %s", orderNum, ext.NewEndDate)
		paymentID, confURL, err := service.extensionPaymentCreator.CreateExtensionPayment(
			ctx,
			clientID,
			rental.ID,
			ext.ID,
			amount,
			desc,
		)
		if err != nil {
			return RentalExtension{}, fmt.Errorf("create extension payment: %w", err)
		}
		_ = service.repository.UpdateExtensionPaymentID(ctx, ext.ID, paymentID)
		ext.PaymentID = &paymentID
		ext.ConfirmationURL = &confURL
	}

	return ext, nil
}

func (service *Service) ListExtensions(
	ctx context.Context,
	clientID string,
	rentalID string,
) ([]RentalExtension, error) {
	return service.repository.ListExtensionsByRental(ctx, clientID, rentalID)
}

func (service *Service) UploadInspectionPhoto(
	ctx context.Context,
	clientID string,
	rentalID string,
	phase string,
	photoType string,
	comment string,
	fileName string,
	mimeType string,
	fileSize int64,
	content io.Reader,
) (InspectionPhoto, error) {
	rental, err := service.repository.GetByClient(ctx, clientID, rentalID)
	if err != nil {
		return InspectionPhoto{}, err
	}

	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase != PhotoPhaseHandover && phase != PhotoPhaseReturn {
		return InspectionPhoto{}, ErrInvalidPhotoPhase
	}

	switch phase {
	case PhotoPhaseHandover:
		if rental.Status != StatusRented && rental.Status != StatusReady && rental.Status != StatusHandedToCourier {
			return InspectionPhoto{}, fmt.Errorf("%w: handover photos are only allowed during equipment handover (status: %s)", ErrPhotoPhaseNotAllowed, rental.Status)
		}
	case PhotoPhaseReturn:
		if rental.Status != StatusAwaitingReturn && rental.Status != StatusInspection {
			return InspectionPhoto{}, fmt.Errorf("%w: return photos are only allowed during equipment return (status: %s)", ErrPhotoPhaseNotAllowed, rental.Status)
		}
	}

	photoType = strings.ToLower(strings.TrimSpace(photoType))
	switch photoType {
	case PhotoTypeBody, PhotoTypeEquipment, PhotoTypeBattery, PhotoTypeSerialNumber, PhotoTypeCleanliness:
	default:
		return InspectionPhoto{}, ErrInvalidPhotoType
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		switch mimeType {
		case "image/jpeg", "image/jpg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		default:
			return InspectionPhoto{}, ErrInvalidPhotoFile
		}
	}
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		return InspectionPhoto{}, ErrInvalidPhotoFile
	}
	if fileSize <= 0 || fileSize > 15*1024*1024 {
		return InspectionPhoto{}, ErrInvalidPhotoFile
	}

	randomPart := randomHex(8)
	relKey := filepath.Join("inspection-photos", rentalID, fmt.Sprintf("%s_%s_%s%s", phase, photoType, randomPart, ext))
	absPath := filepath.Join(service.storagePath, relKey)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return InspectionPhoto{}, fmt.Errorf("create inspection photo dir: %w", err)
	}

	dst, err := os.Create(absPath)
	if err != nil {
		return InspectionPhoto{}, fmt.Errorf("create inspection photo file: %w", err)
	}
	defer dst.Close()

	written, err := io.Copy(dst, content)
	if err != nil {
		_ = os.Remove(absPath)
		return InspectionPhoto{}, fmt.Errorf("save inspection photo: %w", err)
	}

	photo := InspectionPhoto{
		RentalRequestID: rentalID,
		Phase:           phase,
		PhotoType:       photoType,
		StorageKey:      relKey,
		FileName:        fileName,
		MIMEType:        mimeType,
		FileSize:        written,
		Comment:         strings.TrimSpace(comment),
		URL:             fmt.Sprintf("/api/v1/rentals/%s/photos/", rentalID),
	}

	saved, err := service.repository.SaveInspectionPhoto(ctx, photo)
	if err != nil {
		_ = os.Remove(absPath)
		return InspectionPhoto{}, err
	}
	saved.URL = fmt.Sprintf("/api/v1/rentals/%s/photos/%s", rentalID, saved.ID)

	if service.photoNotifier != nil && rental.BitrixDealID != nil && *rental.BitrixDealID != "" {
		photos, _ := service.repository.ListInspectionPhotos(ctx, clientID, rentalID)
		for i := range photos {
			photos[i].URL = fmt.Sprintf("/api/v1/rentals/%s/photos/%s", rentalID, photos[i].ID)
		}
		_ = service.photoNotifier.NotifyInspectionPhotos(ctx, *rental.BitrixDealID, phase, rental.OrderNumber, photos)
	}

	return saved, nil
}

func (service *Service) ListInspectionPhotos(
	ctx context.Context,
	clientID string,
	rentalID string,
) ([]InspectionPhoto, error) {
	photos, err := service.repository.ListInspectionPhotos(ctx, clientID, rentalID)
	if err != nil {
		return nil, err
	}
	for i := range photos {
		photos[i].URL = fmt.Sprintf("/api/v1/rentals/%s/photos/%s", rentalID, photos[i].ID)
	}
	return photos, nil
}

func (service *Service) OpenInspectionPhoto(
	ctx context.Context,
	clientID string,
	rentalID string,
	photoID string,
) (InspectionPhoto, *os.File, error) {
	photo, err := service.repository.GetInspectionPhoto(ctx, clientID, rentalID, photoID)
	if err != nil {
		return InspectionPhoto{}, nil, err
	}
	key := filepath.Clean(strings.TrimSpace(photo.StorageKey))
	if key == "." || filepath.IsAbs(key) || key == ".." || strings.HasPrefix(key, ".."+string(filepath.Separator)) {
		return InspectionPhoto{}, nil, ErrPhotoNotFound
	}
	root, err := filepath.Abs(service.storagePath)
	if err != nil {
		return InspectionPhoto{}, nil, err
	}
	path := filepath.Join(root, key)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return InspectionPhoto{}, nil, ErrPhotoNotFound
	}
	if err != nil {
		return InspectionPhoto{}, nil, fmt.Errorf("open photo file: %w", err)
	}
	photo.URL = fmt.Sprintf("/api/v1/rentals/%s/photos/%s", rentalID, photo.ID)
	return photo, file, nil
}

func randomHex(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}


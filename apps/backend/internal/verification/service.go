package verification

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrDocumentNotFound     = errors.New("document not found")
	ErrDocumentExists       = errors.New("document already exists")
	ErrDocumentsIncomplete  = errors.New("required documents are incomplete")
	ErrSubmissionNotAllowed = errors.New("document submission not allowed")
	ErrReviewNotAllowed     = errors.New("verification review not allowed")
)

type Repository interface {
	CreateDocument(context.Context, Document) (Document, error)
	ListDocuments(context.Context, string) ([]Document, error)
	GetDocument(context.Context, string, string) (Document, error)
	GetDocumentByID(context.Context, string) (Document, error)
	DeleteDocument(context.Context, string, string) (Document, error)
	SubmitDocuments(context.Context, string) error
	CreateReview(context.Context, string, string, string, *string, time.Time) (Review, error)
}

type DocumentNotifier interface {
	NotifyDocumentsSubmitted(context.Context, string, []Document) error
}

type Service struct {
	repository  Repository
	storage     FileStorage
	notifier    DocumentNotifier
	maximumSize int64
	now         func() time.Time
	randomKey   func() (string, error)
}

func NewService(repository Repository, storage FileStorage, maximumSize int64) *Service {
	return &Service{
		repository:  repository,
		storage:     storage,
		maximumSize: maximumSize,
		now:         time.Now,
		randomKey:   generateStorageKey,
	}
}

func (service *Service) SetNotifier(notifier DocumentNotifier) *Service {
	service.notifier = notifier
	return service
}

func (service *Service) UploadDocument(
	ctx context.Context,
	clientID string,
	rawDocumentType string,
	content io.Reader,
) (Document, error) {
	documentType, err := ValidateDocumentType(rawDocumentType)
	if err != nil {
		return Document{}, err
	}

	data, err := io.ReadAll(io.LimitReader(content, service.maximumSize+1))
	if err != nil {
		return Document{}, fmt.Errorf("read document: %w", err)
	}
	mimeType, extension, err := validateDocument(data, service.maximumSize)
	if err != nil {
		return Document{}, err
	}

	randomKey, err := service.randomKey()
	if err != nil {
		return Document{}, fmt.Errorf("generate storage key: %w", err)
	}
	storageKey := randomKey + extension

	if err := service.storage.Save(ctx, storageKey, bytes.NewReader(data)); err != nil {
		return Document{}, fmt.Errorf("store document: %w", err)
	}

	document, err := service.repository.CreateDocument(ctx, Document{
		ClientID:     clientID,
		DocumentType: documentType,
		StorageKey:   storageKey,
		MIMEType:     mimeType,
		SizeBytes:    int64(len(data)),
	})
	if err != nil {
		_ = service.storage.Delete(ctx, storageKey)
		return Document{}, err
	}

	return document, nil
}

func (service *Service) ListDocuments(ctx context.Context, clientID string) ([]Document, error) {
	return service.repository.ListDocuments(ctx, clientID)
}

func (service *Service) GetDocumentContent(
	ctx context.Context,
	clientID string,
	documentID string,
) (Document, io.ReadCloser, error) {
	document, err := service.repository.GetDocument(ctx, clientID, documentID)
	if err != nil {
		return Document{}, nil, err
	}
	content, err := service.storage.Open(ctx, document.StorageKey)
	if err != nil {
		return Document{}, nil, err
	}
	return document, content, nil
}

func (service *Service) GetDocumentContentByID(
	ctx context.Context,
	documentID string,
) (Document, io.ReadCloser, error) {
	document, err := service.repository.GetDocumentByID(ctx, documentID)
	if err != nil {
		return Document{}, nil, err
	}
	content, err := service.storage.Open(ctx, document.StorageKey)
	if err != nil {
		return Document{}, nil, err
	}
	return document, content, nil
}

func (service *Service) DeleteDocument(ctx context.Context, clientID string, documentID string) error {
	document, err := service.repository.DeleteDocument(ctx, clientID, documentID)
	if err != nil {
		return err
	}
	if err := service.storage.Delete(ctx, document.StorageKey); err != nil {
		return fmt.Errorf("delete document file: %w", err)
	}
	return nil
}

func (service *Service) SubmitDocuments(ctx context.Context, clientID string) error {
	if err := service.repository.SubmitDocuments(ctx, clientID); err != nil {
		return err
	}
	if service.notifier != nil {
		documents, _ := service.repository.ListDocuments(ctx, clientID)
		go func() {
			asyncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = service.notifier.NotifyDocumentsSubmitted(asyncCtx, clientID, documents)
		}()
	}
	return nil
}

func (service *Service) Approve(
	ctx context.Context,
	clientID string,
	reviewerID string,
) (Review, error) {
	reviewerID = strings.TrimSpace(reviewerID)
	if reviewerID == "" {
		return Review{}, ErrReviewNotAllowed
	}
	return service.repository.CreateReview(
		ctx,
		clientID,
		reviewerID,
		"approved",
		nil,
		service.now().UTC(),
	)
}

func (service *Service) Reject(
	ctx context.Context,
	clientID string,
	reviewerID string,
	reason string,
) (Review, error) {
	reviewerID = strings.TrimSpace(reviewerID)
	reason = strings.TrimSpace(reason)
	if reviewerID == "" || reason == "" || len([]rune(reason)) > 1000 {
		return Review{}, ErrReviewNotAllowed
	}
	return service.repository.CreateReview(
		ctx,
		clientID,
		reviewerID,
		"rejected",
		&reason,
		service.now().UTC(),
	)
}

func generateStorageKey() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

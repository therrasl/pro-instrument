package verification

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type fakeRepository struct {
	createDocument  func(context.Context, Document) (Document, error)
	listDocuments   func(context.Context, string) ([]Document, error)
	getDocument     func(context.Context, string, string) (Document, error)
	deleteDocument  func(context.Context, string, string) (Document, error)
	submitDocuments func(context.Context, string) error
	createReview    func(context.Context, string, string, string, *string, time.Time) (Review, error)
}

func (repository fakeRepository) CreateDocument(ctx context.Context, document Document) (Document, error) {
	return repository.createDocument(ctx, document)
}

func (repository fakeRepository) ListDocuments(ctx context.Context, clientID string) ([]Document, error) {
	return repository.listDocuments(ctx, clientID)
}

func (repository fakeRepository) GetDocument(
	ctx context.Context,
	clientID string,
	documentID string,
) (Document, error) {
	return repository.getDocument(ctx, clientID, documentID)
}

func (repository fakeRepository) GetDocumentByID(
	ctx context.Context,
	documentID string,
) (Document, error) {
	return repository.getDocument(ctx, "", documentID)
}

func (repository fakeRepository) DeleteDocument(
	ctx context.Context,
	clientID string,
	documentID string,
) (Document, error) {
	return repository.deleteDocument(ctx, clientID, documentID)
}

func (repository fakeRepository) SubmitDocuments(
	ctx context.Context,
	clientID string,
) error {
	return repository.submitDocuments(ctx, clientID)
}

func (repository fakeRepository) CreateReview(
	ctx context.Context,
	clientID string,
	reviewerID string,
	decision string,
	reason *string,
	createdAt time.Time,
) (Review, error) {
	return repository.createReview(ctx, clientID, reviewerID, decision, reason, createdAt)
}

type fakeStorage struct {
	savedKey     string
	savedContent []byte
	deletedKey   string
}

func (storage *fakeStorage) Save(_ context.Context, key string, content io.Reader) error {
	value, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	storage.savedKey = key
	storage.savedContent = value
	return nil
}

func (storage *fakeStorage) Delete(_ context.Context, key string) error {
	storage.deletedKey = key
	return nil
}

func (storage *fakeStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(storage.savedContent)), nil
}

func TestUploadDocumentStoresRandomKeyAndMetadata(t *testing.T) {
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	storage := &fakeStorage{}
	repository := defaultRepository()
	repository.createDocument = func(_ context.Context, document Document) (Document, error) {
		document.ID = "document-id"
		return document, nil
	}

	service := NewService(repository, storage, 1024)
	service.randomKey = func() (string, error) { return "random-storage-key", nil }

	document, err := service.UploadDocument(
		context.Background(),
		"client-id",
		string(DocumentPassportMain),
		bytes.NewReader(content),
	)
	if err != nil {
		t.Fatalf("upload document: %v", err)
	}
	if storage.savedKey != "random-storage-key.png" ||
		!bytes.Equal(storage.savedContent, content) ||
		document.StorageKey != storage.savedKey ||
		document.MIMEType != "image/png" {
		t.Fatalf("unexpected stored document: %#v", document)
	}
}

func TestUploadDocumentRejectsOversizedContent(t *testing.T) {
	service := NewService(defaultRepository(), &fakeStorage{}, 4)

	_, err := service.UploadDocument(
		context.Background(),
		"client-id",
		string(DocumentPassportMain),
		bytes.NewReader([]byte("%PDF-1.7\n")),
	)
	if !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("expected document too large, got %v", err)
	}
}

func TestDeleteDocumentDeletesOnlyReturnedStorageKey(t *testing.T) {
	storage := &fakeStorage{}
	repository := defaultRepository()
	repository.deleteDocument = func(
		_ context.Context,
		clientID string,
		documentID string,
	) (Document, error) {
		if clientID != "owner-id" || documentID != "document-id" {
			t.Fatalf("unexpected owner lookup: client=%q document=%q", clientID, documentID)
		}
		return Document{StorageKey: "random-storage-key.pdf"}, nil
	}

	service := NewService(repository, storage, 1024)
	if err := service.DeleteDocument(context.Background(), "owner-id", "document-id"); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if storage.deletedKey != "random-storage-key.pdf" {
		t.Fatalf("unexpected deleted key: %q", storage.deletedKey)
	}
}

func TestSubmitDocumentsDelegatesAuthenticatedClient(t *testing.T) {
	repository := defaultRepository()
	repository.submitDocuments = func(_ context.Context, clientID string) error {
		if clientID != "client-id" {
			t.Fatalf("unexpected client: %q", clientID)
		}
		return nil
	}

	service := NewService(repository, &fakeStorage{}, 1024)
	if err := service.SubmitDocuments(context.Background(), "client-id"); err != nil {
		t.Fatalf("submit documents: %v", err)
	}
}

func TestRejectRequiresReason(t *testing.T) {
	service := NewService(defaultRepository(), &fakeStorage{}, 1024)

	_, err := service.Reject(context.Background(), "client-id", "reviewer-id", " ")
	if !errors.Is(err, ErrReviewNotAllowed) {
		t.Fatalf("expected review not allowed, got %v", err)
	}
}

func TestApproveAndRejectDelegateReviewDecision(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	var decisions []string
	repository := defaultRepository()
	repository.createReview = func(
		_ context.Context,
		clientID string,
		reviewerID string,
		decision string,
		reason *string,
		createdAt time.Time,
	) (Review, error) {
		if clientID != "client-id" || reviewerID != "reviewer-id" || !createdAt.Equal(now) {
			t.Fatal("unexpected review parameters")
		}
		if decision == "rejected" && (reason == nil || *reason != "Документы нечитаемы") {
			t.Fatalf("unexpected rejection reason: %v", reason)
		}
		decisions = append(decisions, decision)
		return Review{Decision: decision}, nil
	}

	service := NewService(repository, &fakeStorage{}, 1024)
	service.now = func() time.Time { return now }

	if _, err := service.Approve(context.Background(), "client-id", "reviewer-id"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := service.Reject(
		context.Background(),
		"client-id",
		"reviewer-id",
		" Документы нечитаемы ",
	); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if len(decisions) != 2 || decisions[0] != "approved" || decisions[1] != "rejected" {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}

func defaultRepository() fakeRepository {
	return fakeRepository{
		createDocument: func(context.Context, Document) (Document, error) {
			return Document{}, nil
		},
		listDocuments: func(context.Context, string) ([]Document, error) {
			return nil, nil
		},
		getDocument: func(context.Context, string, string) (Document, error) {
			return Document{}, nil
		},
		deleteDocument: func(context.Context, string, string) (Document, error) {
			return Document{}, nil
		},
		submitDocuments: func(context.Context, string) error {
			return nil
		},
		createReview: func(
			context.Context,
			string,
			string,
			string,
			*string,
			time.Time,
		) (Review, error) {
			return Review{}, nil
		},
	}
}

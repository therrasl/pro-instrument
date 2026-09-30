package verification_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

const (
	testClientID   = "40000000-0000-4000-8000-000000000001"
	testDocumentID = "50000000-0000-4000-8000-000000000001"
)

type fakeDocumentService struct {
	upload func(context.Context, string, string, io.Reader) (verification.Document, error)
	list   func(context.Context, string) ([]verification.Document, error)
	get    func(context.Context, string, string) (verification.Document, io.ReadCloser, error)
	delete func(context.Context, string, string) error
	submit func(context.Context, string) error
}

func (service fakeDocumentService) UploadDocument(
	ctx context.Context,
	clientID string,
	documentType string,
	content io.Reader,
) (verification.Document, error) {
	return service.upload(ctx, clientID, documentType, content)
}

func (service fakeDocumentService) ListDocuments(
	ctx context.Context,
	clientID string,
) ([]verification.Document, error) {
	return service.list(ctx, clientID)
}

func (service fakeDocumentService) GetDocumentContent(
	ctx context.Context,
	clientID string,
	documentID string,
) (verification.Document, io.ReadCloser, error) {
	return service.get(ctx, clientID, documentID)
}

func (service fakeDocumentService) GetDocumentContentByID(
	ctx context.Context,
	documentID string,
) (verification.Document, io.ReadCloser, error) {
	return service.get(ctx, "", documentID)
}

func (service fakeDocumentService) DeleteDocument(
	ctx context.Context,
	clientID string,
	documentID string,
) error {
	return service.delete(ctx, clientID, documentID)
}

func (service fakeDocumentService) SubmitDocuments(
	ctx context.Context,
	clientID string,
) error {
	return service.submit(ctx, clientID)
}

type fakeAuthService struct{}

func (fakeAuthService) RequestCode(context.Context, string) error {
	return nil
}

func (fakeAuthService) VerifyCode(context.Context, string, string) (auth.Session, error) {
	return auth.Session{}, nil
}

func (fakeAuthService) Authenticate(_ context.Context, token string) (auth.Client, error) {
	if token != "session-token" {
		return auth.Client{}, auth.ErrUnauthorized
	}
	return auth.Client{ID: testClientID, Status: "profile_completed"}, nil
}

func (fakeAuthService) UpdateProfile(
	context.Context,
	string,
	auth.ProfilePatch,
) (auth.Client, error) {
	return auth.Client{}, nil
}

func (fakeAuthService) AcceptConsents(
	context.Context,
	string,
	auth.ConsentInput,
) (auth.ConsentAcceptance, error) {
	return auth.ConsentAcceptance{}, nil
}

func TestUploadDocument(t *testing.T) {
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	service := defaultDocumentService()
	service.upload = func(
		_ context.Context,
		clientID string,
		documentType string,
		reader io.Reader,
	) (verification.Document, error) {
		actual, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read upload: %v", err)
		}
		if clientID != testClientID ||
			documentType != string(verification.DocumentPassportMain) ||
			!bytes.Equal(actual, content) {
			t.Fatal("unexpected upload arguments")
		}
		return verification.Document{
			ID:           testDocumentID,
			DocumentType: verification.DocumentPassportMain,
			StorageKey:   "private-random-key.png",
			MIMEType:     "image/png",
			SizeBytes:    int64(len(content)),
		}, nil
	}

	body, contentType := multipartBody(t, "original-passport-name.png", content)
	response := serveDocumentRequest(
		service,
		http.MethodPost,
		"/api/v1/me/documents",
		body,
		contentType,
		"Bearer session-token",
	)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("private-random-key")) ||
		bytes.Contains(response.Body.Bytes(), []byte("original-passport-name")) {
		t.Fatalf("response exposes storage details: %s", response.Body.String())
	}
}

func TestListDocumentsReturnsMetadataOnly(t *testing.T) {
	service := defaultDocumentService()
	service.list = func(_ context.Context, clientID string) ([]verification.Document, error) {
		if clientID != testClientID {
			t.Fatalf("unexpected client: %q", clientID)
		}
		return []verification.Document{{
			ID:           testDocumentID,
			DocumentType: verification.DocumentPassportMain,
			StorageKey:   "private-random-key.pdf",
			MIMEType:     "application/pdf",
			SizeBytes:    100,
		}}, nil
	}

	response := serveDocumentRequest(
		service,
		http.MethodGet,
		"/api/v1/me/documents",
		nil,
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("private-random-key")) {
		t.Fatalf("response exposes storage key: %s", response.Body.String())
	}

	var documents []verification.Document
	if err := json.NewDecoder(response.Body).Decode(&documents); err != nil {
		t.Fatalf("decode documents: %v", err)
	}
	if len(documents) != 1 || documents[0].ID != testDocumentID {
		t.Fatalf("unexpected documents: %#v", documents)
	}
}

func TestDeleteDocumentUsesAuthenticatedOwner(t *testing.T) {
	service := defaultDocumentService()
	service.delete = func(_ context.Context, clientID string, documentID string) error {
		if clientID != testClientID || documentID != testDocumentID {
			t.Fatalf("unexpected delete arguments: client=%q document=%q", clientID, documentID)
		}
		return nil
	}

	response := serveDocumentRequest(
		service,
		http.MethodDelete,
		"/api/v1/me/documents/"+testDocumentID,
		nil,
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
}

func TestSubmitDocumentsUsesAuthenticatedClient(t *testing.T) {
	service := defaultDocumentService()
	service.submit = func(_ context.Context, clientID string) error {
		if clientID != testClientID {
			t.Fatalf("unexpected client: %q", clientID)
		}
		return nil
	}

	response := serveDocumentRequest(
		service,
		http.MethodPost,
		"/api/v1/me/documents/submit",
		nil,
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
}

func TestSubmitDocumentsRejectsIncompleteSet(t *testing.T) {
	service := defaultDocumentService()
	service.submit = func(context.Context, string) error {
		return verification.ErrDocumentsIncomplete
	}

	response := serveDocumentRequest(
		service,
		http.MethodPost,
		"/api/v1/me/documents/submit",
		nil,
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
}

func TestGetDocumentContentStreamsOnlyAuthenticatedOwnerDocument(t *testing.T) {
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	service := defaultDocumentService()
	service.get = func(
		_ context.Context,
		clientID string,
		documentID string,
	) (verification.Document, io.ReadCloser, error) {
		if clientID != testClientID || documentID != testDocumentID {
			t.Fatalf("unexpected content lookup: client=%q document=%q", clientID, documentID)
		}
		return verification.Document{MIMEType: "image/png"}, io.NopCloser(bytes.NewReader(content)), nil
	}

	response := serveDocumentRequest(
		service,
		http.MethodGet,
		"/api/v1/me/documents/"+testDocumentID+"/content",
		nil,
		"",
		"Bearer session-token",
	)

	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("unexpected content response: status=%d body=%v", response.Code, response.Body.Bytes())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("unexpected cache control: %q", response.Header().Get("Cache-Control"))
	}
}

func TestDocumentsRequireAuthentication(t *testing.T) {
	response := serveDocumentRequest(
		defaultDocumentService(),
		http.MethodGet,
		"/api/v1/me/documents",
		nil,
		"",
		"",
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func defaultDocumentService() fakeDocumentService {
	return fakeDocumentService{
		upload: func(context.Context, string, string, io.Reader) (verification.Document, error) {
			return verification.Document{}, nil
		},
		list: func(context.Context, string) ([]verification.Document, error) {
			return nil, nil
		},
		get: func(context.Context, string, string) (verification.Document, io.ReadCloser, error) {
			return verification.Document{}, io.NopCloser(bytes.NewReader(nil)), nil
		},
		delete: func(context.Context, string, string) error {
			return nil
		},
		submit: func(context.Context, string) error {
			return nil
		},
	}
}

func multipartBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("document_type", string(verification.DocumentPassportMain)); err != nil {
		t.Fatalf("write document type: %v", err)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	return body, writer.FormDataContentType()
}

func serveDocumentRequest(
	service verification.DocumentService,
	method string,
	target string,
	body io.Reader,
	contentType string,
	authorization string,
) *httptest.ResponseRecorder {
	logger := log.New(io.Discard, "", 0)
	authHandler := auth.NewHandler(fakeAuthService{}, logger)
	documentHandler := verification.NewHandler(service, logger, 1024)

	mux := http.NewServeMux()
	documentHandler.Register(mux, authHandler.BearerAuth)

	request := httptest.NewRequest(method, target, body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestViewDocumentPublicEndpoint(t *testing.T) {
	fileBytes := []byte("fake-image-bytes")
	service := fakeDocumentService{
		get: func(_ context.Context, _ string, docID string) (verification.Document, io.ReadCloser, error) {
			if docID != testDocumentID {
				return verification.Document{}, nil, verification.ErrDocumentNotFound
			}
			return verification.Document{
				ID:       testDocumentID,
				MIMEType: "image/jpeg",
			}, io.NopCloser(bytes.NewReader(fileBytes)), nil
		},
	}

	rec := serveDocumentRequest(service, http.MethodGet, "/api/v1/verification/documents/"+testDocumentID+"/view", nil, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %s", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != string(fileBytes) {
		t.Errorf("expected file bytes %q, got %q", fileBytes, rec.Body.Bytes())
	}

	rec = serveDocumentRequest(service, http.MethodGet, "/api/v1/verification/documents/50000000-0000-4000-8000-000000000099/view", nil, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}
}

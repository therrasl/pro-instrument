package verification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const multipartOverheadAllowance int64 = 1 << 20

type DocumentService interface {
	UploadDocument(context.Context, string, string, io.Reader) (Document, error)
	ListDocuments(context.Context, string) ([]Document, error)
	GetDocumentContent(context.Context, string, string) (Document, io.ReadCloser, error)
	GetDocumentContentByID(context.Context, string) (Document, io.ReadCloser, error)
	DeleteDocument(context.Context, string, string) error
	SubmitDocuments(context.Context, string) error
}

type Handler struct {
	service     DocumentService
	logger      *log.Logger
	maximumSize int64
}

func NewHandler(service DocumentService, logger *log.Logger, maximumSize int64) *Handler {
	return &Handler{service: service, logger: logger, maximumSize: maximumSize}
}

func (handler *Handler) Register(
	mux *http.ServeMux,
	authenticate func(http.Handler) http.Handler,
) {
	mux.Handle("POST /api/v1/me/documents", authenticate(http.HandlerFunc(handler.uploadDocument)))
	mux.Handle("GET /api/v1/me/documents", authenticate(http.HandlerFunc(handler.listDocuments)))
	mux.Handle(
		"GET /api/v1/me/documents/{id}/content",
		authenticate(http.HandlerFunc(handler.getDocumentContent)),
	)
	mux.HandleFunc("GET /api/v1/verification/documents/{id}/view", handler.viewDocument)
	mux.Handle(
		"DELETE /api/v1/me/documents/{id}",
		authenticate(http.HandlerFunc(handler.deleteDocument)),
	)
	mux.Handle(
		"POST /api/v1/me/documents/submit",
		authenticate(http.HandlerFunc(handler.submitDocuments)),
	)
}

func (handler *Handler) uploadDocument(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		handler.maximumSize+multipartOverheadAllowance,
	)
	if err := request.ParseMultipartForm(handler.maximumSize); err != nil {
		writeError(response, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	defer func() {
		_ = request.MultipartForm.RemoveAll()
	}()

	if !validMultipartShape(request) {
		writeError(response, http.StatusBadRequest, "invalid multipart upload")
		return
	}

	fileHeader := request.MultipartForm.File["file"][0]
	if fileHeader.Size <= 0 || fileHeader.Size > handler.maximumSize {
		writeError(response, http.StatusBadRequest, "invalid document size")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		handler.internalError(response, err)
		return
	}
	defer func() {
		_ = file.Close()
	}()

	document, err := handler.service.UploadDocument(
		request.Context(),
		client.ID,
		request.MultipartForm.Value["document_type"][0],
		file,
	)
	switch {
	case err == nil:
		writeJSON(response, http.StatusCreated, document)
	case errors.Is(err, ErrInvalidDocument),
		errors.Is(err, ErrDocumentTooLarge),
		errors.Is(err, ErrUnsupportedMIME):
		writeError(response, http.StatusBadRequest, "invalid document")
	case errors.Is(err, ErrDocumentExists):
		writeError(response, http.StatusConflict, "document type already uploaded")
	default:
		handler.internalError(response, err)
	}
}

func (handler *Handler) listDocuments(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	documents, err := handler.service.ListDocuments(request.Context(), client.ID)
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, documents)
}

func (handler *Handler) getDocumentContent(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	documentID := request.PathValue("id")
	if !validUUID(documentID) {
		writeError(response, http.StatusBadRequest, "invalid document id")
		return
	}

	document, content, err := handler.service.GetDocumentContent(
		request.Context(),
		client.ID,
		documentID,
	)
	if errors.Is(err, ErrDocumentNotFound) {
		writeError(response, http.StatusNotFound, "document not found")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}
	defer func() {
		_ = content.Close()
	}()

	response.Header().Set("Content-Type", document.MIMEType)
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, content); err != nil {
		handler.logger.Printf("stream document content failed: %v", err)
	}
}

func (handler *Handler) deleteDocument(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	documentID := request.PathValue("id")
	if !validUUID(documentID) {
		writeError(response, http.StatusBadRequest, "invalid document id")
		return
	}

	err := handler.service.DeleteDocument(request.Context(), client.ID, documentID)
	if errors.Is(err, ErrDocumentNotFound) {
		writeError(response, http.StatusNotFound, "document not found")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) viewDocument(response http.ResponseWriter, request *http.Request) {
	documentID := request.PathValue("id")
	if !validUUID(documentID) {
		writeError(response, http.StatusBadRequest, "invalid document id")
		return
	}

	document, content, err := handler.service.GetDocumentContentByID(
		request.Context(),
		documentID,
	)
	if errors.Is(err, ErrDocumentNotFound) {
		writeError(response, http.StatusNotFound, "document not found")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}
	defer func() {
		_ = content.Close()
	}()

	response.Header().Set("Content-Type", document.MIMEType)
	response.Header().Set("Content-Disposition", "inline")
	response.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(response, content); err != nil {
		handler.logger.Printf("stream document view failed: %v", err)
	}
}

func (handler *Handler) submitDocuments(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	err := handler.service.SubmitDocuments(request.Context(), client.ID)
	switch {
	case err == nil:
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrDocumentsIncomplete):
		writeError(response, http.StatusConflict, "documents incomplete")
	case errors.Is(err, ErrSubmissionNotAllowed):
		writeError(response, http.StatusConflict, "document submission not allowed")
	default:
		handler.internalError(response, err)
	}
}

func (handler *Handler) internalError(response http.ResponseWriter, err error) {
	handler.logger.Printf("verification request failed: %v", err)
	writeError(response, http.StatusInternalServerError, "internal server error")
}

func validMultipartShape(request *http.Request) bool {
	if request.MultipartForm == nil ||
		len(request.MultipartForm.Value) != 1 ||
		len(request.MultipartForm.File) != 1 {
		return false
	}
	documentTypes, hasDocumentType := request.MultipartForm.Value["document_type"]
	files, hasFile := request.MultipartForm.File["file"]
	return hasDocumentType &&
		len(documentTypes) == 1 &&
		documentTypes[0] != "" &&
		hasFile &&
		len(files) == 1
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(value) == nil && id.Valid
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

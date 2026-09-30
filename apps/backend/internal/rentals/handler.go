package rentals

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const (
	maximumRequestBody = 1 << 20
	defaultListLimit   = 20
	maximumListLimit   = 100
)

type RentalService interface {
	Quote(context.Context, QuoteRequest) (Quote, error)
	Create(context.Context, string, QuoteRequest) (RentalRequest, error)
	List(context.Context, string, int, int) ([]RentalRequest, error)
	Get(context.Context, string, string) (RentalRequest, error)
	Cancel(context.Context, string, string) (RentalRequest, error)
	ListDocuments(context.Context, string, string) ([]OrderDocument, error)
	OpenDocument(context.Context, string, string, string) (DocumentContent, error)
	QuoteExtension(context.Context, string, string, string) (ExtensionQuote, error)
	CreateExtension(context.Context, string, string, string) (RentalExtension, error)
	ListExtensions(context.Context, string, string) ([]RentalExtension, error)
	UploadInspectionPhoto(context.Context, string, string, string, string, string, string, string, int64, io.Reader) (InspectionPhoto, error)
	ListInspectionPhotos(context.Context, string, string) ([]InspectionPhoto, error)
	OpenInspectionPhoto(context.Context, string, string, string) (InspectionPhoto, *os.File, error)
}

type Handler struct {
	service RentalService
	logger  *log.Logger
}

func NewHandler(service RentalService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(
	mux *http.ServeMux,
	bearerAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/rentals/quote",
		bearerAuth(http.HandlerFunc(handler.quote)),
	)
	mux.Handle(
		"POST /api/v1/rentals",
		bearerAuth(http.HandlerFunc(handler.create)),
	)
	mux.Handle(
		"GET /api/v1/rentals",
		bearerAuth(http.HandlerFunc(handler.list)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}",
		bearerAuth(http.HandlerFunc(handler.get)),
	)
	mux.Handle(
		"POST /api/v1/rentals/{id}/cancel",
		bearerAuth(http.HandlerFunc(handler.cancel)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}/documents",
		bearerAuth(http.HandlerFunc(handler.documents)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}/documents/{documentID}/download",
		bearerAuth(http.HandlerFunc(handler.downloadDocument)),
	)
	mux.Handle(
		"POST /api/v1/rentals/{id}/extension-quote",
		bearerAuth(http.HandlerFunc(handler.extensionQuote)),
	)
	mux.Handle(
		"POST /api/v1/rentals/{id}/extensions",
		bearerAuth(http.HandlerFunc(handler.createExtension)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}/extensions",
		bearerAuth(http.HandlerFunc(handler.listExtensions)),
	)
	mux.Handle(
		"POST /api/v1/rentals/{id}/photos",
		bearerAuth(http.HandlerFunc(handler.uploadPhoto)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}/photos",
		bearerAuth(http.HandlerFunc(handler.listPhotos)),
	)
	mux.Handle(
		"GET /api/v1/rentals/{id}/photos/{photoID}",
		http.HandlerFunc(handler.getPhoto),
	)
}

func (handler *Handler) downloadDocument(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID, documentID := request.PathValue("id"), request.PathValue("documentID")
	if !validUUID(rentalID) || !validUUID(documentID) {
		writeError(response, http.StatusBadRequest, "invalid document")
		return
	}
	content, err := handler.service.OpenDocument(request.Context(), client.ID, rentalID, documentID)
	if handler.writeServiceError(response, err) {
		return
	}
	defer content.File.Close()
	mimeType := "application/pdf"
	if content.Document.MIMEType != nil && *content.Document.MIMEType != "" {
		mimeType = *content.Document.MIMEType
	}
	filename := filepath.Base(content.Document.Title)
	if filepath.Ext(filename) == "" {
		if extensions, _ := mime.ExtensionsByType(mimeType); len(extensions) > 0 {
			filename += extensions[0]
		}
	}
	response.Header().Set("Content-Type", mimeType)
	response.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Length", strconv.FormatInt(content.Size, 10))
	http.ServeContent(response, request, filename, content.Document.CreatedAt, content.File)
}

func (handler *Handler) documents(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	documents, err := handler.service.ListDocuments(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, documents)
}

func (handler *Handler) quote(response http.ResponseWriter, request *http.Request) {
	_, ok := eligibleClient(request)
	if !ok {
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
		return
	}
	input, err := decodeRentalInput(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid rental request")
		return
	}

	quote, err := handler.service.Quote(request.Context(), input)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, quote)
}

func (handler *Handler) create(response http.ResponseWriter, request *http.Request) {
	client, ok := eligibleClient(request)
	if !ok {
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
		return
	}

	input, err := decodeRentalInput(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid rental request")
		return
	}

	rental, err := handler.service.Create(request.Context(), client.ID, input)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusCreated, rental)
}

func (handler *Handler) list(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset, err := listPagination(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid pagination")
		return
	}
	rentals, err := handler.service.List(request.Context(), client.ID, limit, offset)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rentals)
}

func (handler *Handler) get(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	rental, err := handler.service.Get(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rental)
}

func (handler *Handler) cancel(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}

	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	rental, err := handler.service.Cancel(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, rental)
}

func eligibleClient(request *http.Request) (auth.Client, bool) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		return auth.Client{}, false
	}
	return client, client.Status == "verified" &&
		client.PhoneVerified &&
		client.ProfileCompleted &&
		client.OfferAccepted
}

func decodeRentalInput(response http.ResponseWriter, request *http.Request) (QuoteRequest, error) {
	var body struct {
		ToolID          string `json:"tool_id"`
		StartDate       string `json:"start_date"`
		EndDate         string `json:"end_date"`
		DeliveryMethod  string `json:"delivery_method"`
		DeliveryAddress string `json:"delivery_address"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		return QuoteRequest{}, err
	}
	if !validUUID(body.ToolID) {
		return QuoteRequest{}, ErrInvalidInput
	}
	startDate, err := time.Parse(time.DateOnly, body.StartDate)
	if err != nil {
		return QuoteRequest{}, ErrInvalidInput
	}
	endDate, err := time.Parse(time.DateOnly, body.EndDate)
	if err != nil {
		return QuoteRequest{}, ErrInvalidInput
	}
	return QuoteRequest{
		ToolID:          body.ToolID,
		StartDate:       startDate,
		EndDate:         endDate,
		DeliveryMethod:  body.DeliveryMethod,
		DeliveryAddress: body.DeliveryAddress,
	}, nil
}

func (handler *Handler) writeServiceError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid rental request")
	case errors.Is(err, ErrOnboardingIncomplete):
		writeError(response, http.StatusForbidden, "rental onboarding is incomplete")
	case errors.Is(err, ErrToolNotFound):
		writeError(response, http.StatusNotFound, "tool not found")
	case errors.Is(err, ErrNoAvailableUnit):
		writeError(response, http.StatusConflict, "no tool unit is available for selected dates")
	case errors.Is(err, ErrRentalNotFound):
		writeError(response, http.StatusNotFound, "rental request not found")
	case errors.Is(err, ErrDocumentNotFound):
		writeError(response, http.StatusNotFound, "document not found")
	case errors.Is(err, ErrRentalNotCancellable):
		writeError(response, http.StatusConflict, "rental request cannot be cancelled")
	case errors.Is(err, ErrRentalNotActive):
		writeError(response, http.StatusConflict, "rental is not active for extension")
	case errors.Is(err, ErrInvalidExtensionDate):
		writeError(response, http.StatusBadRequest, "new end date must be after current end date")
	case errors.Is(err, ErrExtensionUnavailable):
		writeError(response, http.StatusConflict, "tool is not available for requested extension period")
	case errors.Is(err, ErrInvalidPhotoPhase):
		writeError(response, http.StatusBadRequest, "invalid inspection photo phase")
	case errors.Is(err, ErrPhotoPhaseNotAllowed):
		writeError(response, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidPhotoType):
		writeError(response, http.StatusBadRequest, "invalid inspection photo type")
	case errors.Is(err, ErrInvalidPhotoFile):
		writeError(response, http.StatusBadRequest, "invalid inspection photo file (must be jpg, png, or webp up to 15MB)")
	case errors.Is(err, ErrPhotoNotFound):
		writeError(response, http.StatusNotFound, "inspection photo not found")
	default:
		handler.logger.Printf("rental request failed: %v", err)
		writeError(response, http.StatusInternalServerError, "internal server error")
	}
	return true
}

func (handler *Handler) extensionQuote(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	var body ExtensionQuoteRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid extension request")
		return
	}
	quote, err := handler.service.QuoteExtension(request.Context(), client.ID, rentalID, body.NewEndDate)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, quote)
}

func (handler *Handler) createExtension(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	var body ExtensionQuoteRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid extension request")
		return
	}
	ext, err := handler.service.CreateExtension(request.Context(), client.ID, rentalID, body.NewEndDate)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusCreated, ext)
}

func (handler *Handler) listExtensions(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	extensions, err := handler.service.ListExtensions(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	if extensions == nil {
		extensions = []RentalExtension{}
	}
	writeJSON(response, http.StatusOK, extensions)
}

func (handler *Handler) uploadPhoto(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}

	if err := request.ParseMultipartForm(16 << 20); err != nil {
		writeError(response, http.StatusBadRequest, "invalid multipart request")
		return
	}

	phase := request.FormValue("phase")
	photoType := request.FormValue("photo_type")
	comment := request.FormValue("comment")

	file, header, err := request.FormFile("file")
	if err != nil {
		writeError(response, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	photo, err := handler.service.UploadInspectionPhoto(
		request.Context(),
		client.ID,
		rentalID,
		phase,
		photoType,
		comment,
		header.Filename,
		mimeType,
		header.Size,
		file,
	)
	if handler.writeServiceError(response, err) {
		return
	}
	writeJSON(response, http.StatusCreated, photo)
}

func (handler *Handler) listPhotos(response http.ResponseWriter, request *http.Request) {
	client, ok := auth.ClientFromContext(request.Context())
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		writeError(response, http.StatusBadRequest, "invalid rental id")
		return
	}
	photos, err := handler.service.ListInspectionPhotos(request.Context(), client.ID, rentalID)
	if handler.writeServiceError(response, err) {
		return
	}
	if photos == nil {
		photos = []InspectionPhoto{}
	}
	writeJSON(response, http.StatusOK, photos)
}

func (handler *Handler) getPhoto(response http.ResponseWriter, request *http.Request) {
	clientID := ""
	if client, ok := auth.ClientFromContext(request.Context()); ok {
		clientID = client.ID
	}
	rentalID, photoID := request.PathValue("id"), request.PathValue("photoID")
	if !validUUID(rentalID) || !validUUID(photoID) {
		writeError(response, http.StatusBadRequest, "invalid id")
		return
	}
	photo, file, err := handler.service.OpenInspectionPhoto(request.Context(), clientID, rentalID, photoID)
	if handler.writeServiceError(response, err) {
		return
	}
	defer file.Close()

	mimeType := "image/jpeg"
	if photo.MIMEType != "" {
		mimeType = photo.MIMEType
	}
	response.Header().Set("Content-Type", mimeType)
	response.Header().Set("Cache-Control", "public, max-age=86400")
	response.Header().Set("Content-Length", strconv.FormatInt(photo.FileSize, 10))
	http.ServeContent(response, request, photo.FileName, photo.CreatedAt, file)
}

func listPagination(request *http.Request) (int, int, error) {
	limit := defaultListLimit
	offset := 0
	var err error
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maximumListLimit {
			return 0, 0, ErrInvalidInput
		}
	}
	if value := request.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			return 0, 0, ErrInvalidInput
		}
	}
	return limit, offset, nil
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maximumRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidInput
	}
	return nil
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

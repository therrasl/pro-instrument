package rentals

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const testRentalID = "50000000-0000-4000-8000-000000000001"

type fakeRentalService struct {
	quote        func(context.Context, QuoteRequest) (Quote, error)
	create       func(context.Context, string, QuoteRequest) (RentalRequest, error)
	list         func(context.Context, string, int, int) ([]RentalRequest, error)
	get          func(context.Context, string, string) (RentalRequest, error)
	cancel       func(context.Context, string, string) (RentalRequest, error)
	documents    func(context.Context, string, string) ([]OrderDocument, error)
	openDocument func(context.Context, string, string, string) (DocumentContent, error)
}

func (service fakeRentalService) ListDocuments(ctx context.Context, clientID string, rentalID string) ([]OrderDocument, error) {
	if service.documents == nil {
		return []OrderDocument{}, nil
	}
	return service.documents(ctx, clientID, rentalID)
}

func (service fakeRentalService) OpenDocument(ctx context.Context, clientID, rentalID, documentID string) (DocumentContent, error) {
	if service.openDocument != nil {
		return service.openDocument(ctx, clientID, rentalID, documentID)
	}
	return DocumentContent{}, ErrDocumentNotFound
}

func (service fakeRentalService) Quote(
	ctx context.Context,
	input QuoteRequest,
) (Quote, error) {
	return service.quote(ctx, input)
}

func (service fakeRentalService) Create(
	ctx context.Context,
	clientID string,
	input QuoteRequest,
) (RentalRequest, error) {
	return service.create(ctx, clientID, input)
}

func (service fakeRentalService) List(
	ctx context.Context,
	clientID string,
	limit int,
	offset int,
) ([]RentalRequest, error) {
	return service.list(ctx, clientID, limit, offset)
}

func (service fakeRentalService) Get(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	return service.get(ctx, clientID, rentalID)
}

func (service fakeRentalService) Cancel(
	ctx context.Context,
	clientID string,
	rentalID string,
) (RentalRequest, error) {
	return service.cancel(ctx, clientID, rentalID)
}

func (service fakeRentalService) QuoteExtension(context.Context, string, string, string) (ExtensionQuote, error) {
	return ExtensionQuote{}, nil
}

func (service fakeRentalService) CreateExtension(context.Context, string, string, string) (RentalExtension, error) {
	return RentalExtension{}, nil
}

func (service fakeRentalService) ListExtensions(context.Context, string, string) ([]RentalExtension, error) {
	return []RentalExtension{}, nil
}

func (service fakeRentalService) UploadInspectionPhoto(context.Context, string, string, string, string, string, string, string, int64, io.Reader) (InspectionPhoto, error) {
	return InspectionPhoto{}, nil
}

func (service fakeRentalService) ListInspectionPhotos(context.Context, string, string) ([]InspectionPhoto, error) {
	return []InspectionPhoto{}, nil
}

func (service fakeRentalService) OpenInspectionPhoto(context.Context, string, string, string) (InspectionPhoto, *os.File, error) {
	return InspectionPhoto{}, nil, ErrPhotoNotFound
}

type rentalAuthService struct {
	client auth.Client
}

func (service rentalAuthService) RequestCode(context.Context, string) error {
	return nil
}

func (service rentalAuthService) VerifyCode(
	context.Context,
	string,
	string,
) (auth.Session, error) {
	return auth.Session{}, nil
}

func (service rentalAuthService) Authenticate(context.Context, string) (auth.Client, error) {
	return service.client, nil
}

func (service rentalAuthService) UpdateProfile(
	context.Context,
	string,
	auth.ProfilePatch,
) (auth.Client, error) {
	return auth.Client{}, nil
}

func (service rentalAuthService) AcceptConsents(
	context.Context,
	string,
	auth.ConsentInput,
) (auth.ConsentAcceptance, error) {
	return auth.ConsentAcceptance{}, nil
}

func TestCreateRentalAPIUsesAuthenticatedClient(t *testing.T) {
	service := defaultFakeRentalService()
	service.create = func(
		_ context.Context,
		clientID string,
		input QuoteRequest,
	) (RentalRequest, error) {
		if clientID != testClientID ||
			input.ToolID != testToolID ||
			input.DeliveryMethod != DeliverySelfPickup {
			t.Fatalf("unexpected create input: client=%q input=%#v", clientID, input)
		}
		return RentalRequest{
			ID:       testRentalID,
			ClientID: clientID,
			ToolID:   input.ToolID,
			Status:   StatusPendingManager,
		}, nil
	}

	response := serveRentalRequest(
		t,
		service,
		eligibleAuthClient(),
		http.MethodPost,
		"/api/v1/rentals",
		`{
			"tool_id":"10000000-0000-4000-8000-000000000001",
			"start_date":"2026-08-02",
			"end_date":"2026-08-03",
			"delivery_method":"self_pickup"
		}`,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body)
	}
}

func TestRentalAPIRejectsIncompleteOnboarding(t *testing.T) {
	service := defaultFakeRentalService()
	service.quote = func(context.Context, QuoteRequest) (Quote, error) {
		t.Fatal("rental service must not run before onboarding")
		return Quote{}, nil
	}

	response := serveRentalRequest(
		t,
		service,
		auth.Client{ID: testClientID, Status: "pending_verification"},
		http.MethodPost,
		"/api/v1/rentals/quote",
		`{
			"tool_id":"10000000-0000-4000-8000-000000000001",
			"start_date":"2026-08-02",
			"end_date":"2026-08-03",
			"delivery_method":"self_pickup"
		}`,
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
}

func TestClientCannotReadForeignRentalAPI(t *testing.T) {
	service := defaultFakeRentalService()
	service.get = func(
		_ context.Context,
		clientID string,
		rentalID string,
	) (RentalRequest, error) {
		if clientID != testClientID || rentalID != testRentalID {
			t.Fatalf("unexpected scoped lookup: client=%q rental=%q", clientID, rentalID)
		}
		return RentalRequest{}, ErrRentalNotFound
	}

	response := serveRentalRequest(
		t,
		service,
		eligibleAuthClient(),
		http.MethodGet,
		"/api/v1/rentals/"+testRentalID,
		"",
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestCancelRentalAPIUsesAuthenticatedOwner(t *testing.T) {
	service := defaultFakeRentalService()
	service.cancel = func(
		_ context.Context,
		clientID string,
		rentalID string,
	) (RentalRequest, error) {
		if clientID != testClientID || rentalID != testRentalID {
			t.Fatalf("unexpected cancel scope: client=%q rental=%q", clientID, rentalID)
		}
		return RentalRequest{ID: rentalID, ClientID: clientID, Status: StatusCancelled}, nil
	}

	response := serveRentalRequest(
		t,
		service,
		eligibleAuthClient(),
		http.MethodPost,
		"/api/v1/rentals/"+testRentalID+"/cancel",
		"",
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("unexpected cancellation response: %d %s", response.Code, response.Body)
	}
}

func TestCancelRentalAPIRejectsStatusAfterPayment(t *testing.T) {
	service := defaultFakeRentalService()
	service.cancel = func(context.Context, string, string) (RentalRequest, error) {
		return RentalRequest{}, ErrRentalNotCancellable
	}

	response := serveRentalRequest(
		t,
		service,
		eligibleAuthClient(),
		http.MethodPost,
		"/api/v1/rentals/"+testRentalID+"/cancel",
		"",
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
}

func TestDownloadOrderDocumentUsesOwnerScopeAndSafeHeaders(t *testing.T) {
	const documentID = "60000000-0000-4000-8000-000000000001"
	file, err := os.CreateTemp(t.TempDir(), "contract-*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("%PDF-1.4\ntest"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	service := defaultFakeRentalService()
	service.openDocument = func(_ context.Context, clientID, rentalID, gotDocumentID string) (DocumentContent, error) {
		if clientID != testClientID || rentalID != testRentalID || gotDocumentID != documentID {
			t.Fatalf("wrong document scope")
		}
		return DocumentContent{Document: OrderDocument{ID: documentID, Title: "Договор аренды", CreatedAt: time.Now()}, File: file, Size: 13}, nil
	}
	response := serveRentalRequest(t, service, eligibleAuthClient(), http.MethodGet, "/api/v1/rentals/"+testRentalID+"/documents/"+documentID+"/download", "")
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe response: %d %#v", response.Code, response.Header())
	}
}

func serveRentalRequest(
	t *testing.T,
	service RentalService,
	client auth.Client,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	logger := log.New(io.Discard, "", 0)
	authHandler := auth.NewHandler(rentalAuthService{client: client}, logger)
	mux := http.NewServeMux()
	NewHandler(service, logger).Register(mux, authHandler.BearerAuth)

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func eligibleAuthClient() auth.Client {
	return auth.Client{
		ID:               testClientID,
		Status:           "verified",
		PhoneVerified:    true,
		ProfileCompleted: true,
		OfferAccepted:    true,
	}
}

func defaultFakeRentalService() fakeRentalService {
	return fakeRentalService{
		quote: func(context.Context, QuoteRequest) (Quote, error) {
			return Quote{}, nil
		},
		create: func(context.Context, string, QuoteRequest) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
		list: func(context.Context, string, int, int) ([]RentalRequest, error) {
			return nil, nil
		},
		get: func(context.Context, string, string) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
		cancel: func(context.Context, string, string) (RentalRequest, error) {
			return RentalRequest{}, nil
		},
	}
}

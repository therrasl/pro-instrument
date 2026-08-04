package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/catalog"
)

const (
	categoryID = "10000000-0000-4000-8000-000000000001"
	toolID     = "20000000-0000-4000-8000-000000000001"
)

type fakeService struct {
	listCategories func(context.Context) ([]catalog.Category, error)
	listTools      func(context.Context, catalog.ListToolsFilter) ([]catalog.Tool, error)
	getTool        func(context.Context, string) (catalog.Tool, error)
}

func (service fakeService) ListCategories(ctx context.Context) ([]catalog.Category, error) {
	return service.listCategories(ctx)
}

func (service fakeService) ListTools(ctx context.Context, filter catalog.ListToolsFilter) ([]catalog.Tool, error) {
	return service.listTools(ctx, filter)
}

func (service fakeService) GetTool(ctx context.Context, id string) (catalog.Tool, error) {
	return service.getTool(ctx, id)
}

func TestListCategories(t *testing.T) {
	service := fakeService{
		listCategories: func(context.Context) ([]catalog.Category, error) {
			return []catalog.Category{{ID: categoryID, Name: "Пилы", Slug: "saws"}}, nil
		},
		listTools: func(context.Context, catalog.ListToolsFilter) ([]catalog.Tool, error) {
			return nil, nil
		},
		getTool: func(context.Context, string) (catalog.Tool, error) {
			return catalog.Tool{}, nil
		},
	}

	response := serveRequest(service, http.MethodGet, "/api/v1/categories")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var categories []catalog.Category
	if err := json.NewDecoder(response.Body).Decode(&categories); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(categories) != 1 || categories[0].ID != categoryID {
		t.Fatalf("unexpected categories response: %#v", categories)
	}
}

func TestListToolsParsesFilters(t *testing.T) {
	var received catalog.ListToolsFilter
	service := fakeService{
		listCategories: func(context.Context) ([]catalog.Category, error) {
			return nil, nil
		},
		listTools: func(_ context.Context, filter catalog.ListToolsFilter) ([]catalog.Tool, error) {
			received = filter
			return []catalog.Tool{{ID: toolID, AvailableUnits: 2}}, nil
		},
		getTool: func(context.Context, string) (catalog.Tool, error) {
			return catalog.Tool{}, nil
		},
	}

	target := "/api/v1/tools?category_id=" + categoryID + "&search=bosch&available=true&limit=10&offset=20"
	response := serveRequest(service, http.MethodGet, target)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if received.CategoryID != categoryID ||
		received.Search != "bosch" ||
		!received.AvailableOnly ||
		received.Limit != 10 ||
		received.Offset != 20 {
		t.Fatalf("unexpected filters: %#v", received)
	}

	var tools []catalog.Tool
	if err := json.NewDecoder(response.Body).Decode(&tools); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tools) != 1 || tools[0].AvailableUnits != 2 {
		t.Fatalf("unexpected tools response: %#v", tools)
	}
}

func TestGetToolNotFound(t *testing.T) {
	service := fakeService{
		listCategories: func(context.Context) ([]catalog.Category, error) {
			return nil, nil
		},
		listTools: func(context.Context, catalog.ListToolsFilter) ([]catalog.Tool, error) {
			return nil, nil
		},
		getTool: func(context.Context, string) (catalog.Tool, error) {
			return catalog.Tool{}, catalog.ErrNotFound
		},
	}

	response := serveRequest(service, http.MethodGet, "/api/v1/tools/"+toolID)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestToolAssetIsServedByBackend(t *testing.T) {
	response := serveRequest(
		defaultCatalogService(),
		http.MethodGet,
		"/static/tools/rotary-hammer.jpg",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "image/jpeg" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if response.Body.Len() == 0 {
		t.Fatal("tool image response is empty")
	}
}

func TestHandlerHidesInternalErrors(t *testing.T) {
	service := fakeService{
		listCategories: func(context.Context) ([]catalog.Category, error) {
			return nil, errors.New("database credentials leaked")
		},
		listTools: func(context.Context, catalog.ListToolsFilter) ([]catalog.Tool, error) {
			return nil, nil
		},
		getTool: func(context.Context, string) (catalog.Tool, error) {
			return catalog.Tool{}, nil
		},
	}

	response := serveRequest(service, http.MethodGet, "/api/v1/categories")

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, response.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "internal server error" {
		t.Fatalf("unexpected error response: %#v", body)
	}
}

func serveRequest(service catalog.CatalogService, method string, target string) *httptest.ResponseRecorder {
	handler := catalog.NewHandler(service, log.New(io.Discard, "", 0))
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	return response
}

func defaultCatalogService() fakeService {
	return fakeService{
		listCategories: func(context.Context) ([]catalog.Category, error) { return nil, nil },
		listTools: func(context.Context, catalog.ListToolsFilter) ([]catalog.Tool, error) {
			return nil, nil
		},
		getTool: func(context.Context, string) (catalog.Tool, error) {
			return catalog.Tool{}, nil
		},
	}
}

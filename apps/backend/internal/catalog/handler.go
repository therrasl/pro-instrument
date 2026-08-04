package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	defaultLimit = 20
	maximumLimit = 100
)

type CatalogService interface {
	ListCategories(context.Context) ([]Category, error)
	ListTools(context.Context, ListToolsFilter) ([]Tool, error)
	GetTool(context.Context, string) (Tool, error)
}

type Handler struct {
	service CatalogService
	logger  *log.Logger
}

func NewHandler(service CatalogService, logger *log.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/categories", handler.listCategories)
	mux.HandleFunc("GET /api/v1/tools", handler.listTools)
	mux.HandleFunc("GET /api/v1/tools/{id}", handler.getTool)
	mux.Handle("GET /static/tools/", toolAssetsHandler())
}

func (handler *Handler) listCategories(response http.ResponseWriter, request *http.Request) {
	categories, err := handler.service.ListCategories(request.Context())
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, categories)
}

func (handler *Handler) listTools(response http.ResponseWriter, request *http.Request) {
	filter, err := parseListToolsFilter(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}

	tools, err := handler.service.ListTools(request.Context(), filter)
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, tools)
}

func (handler *Handler) getTool(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !validUUID(id) {
		writeError(response, http.StatusBadRequest, "invalid tool id")
		return
	}

	tool, err := handler.service.GetTool(request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(response, http.StatusNotFound, "tool not found")
		return
	}
	if err != nil {
		handler.internalError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, tool)
}

func (handler *Handler) internalError(response http.ResponseWriter, err error) {
	handler.logger.Printf("catalog request failed: %v", err)
	writeError(response, http.StatusInternalServerError, "internal server error")
}

func parseListToolsFilter(request *http.Request) (ListToolsFilter, error) {
	query := request.URL.Query()
	filter := ListToolsFilter{
		Search: strings.TrimSpace(query.Get("search")),
		Limit:  defaultLimit,
	}

	if categoryID := query.Get("category_id"); categoryID != "" {
		if !validUUID(categoryID) {
			return ListToolsFilter{}, errors.New("invalid category_id")
		}
		filter.CategoryID = categoryID
	}

	if available := query.Get("available"); available != "" {
		value, err := strconv.ParseBool(available)
		if err != nil {
			return ListToolsFilter{}, errors.New("invalid available")
		}
		filter.AvailableOnly = value
	}

	if limit := query.Get("limit"); limit != "" {
		value, err := strconv.Atoi(limit)
		if err != nil || value < 1 || value > maximumLimit {
			return ListToolsFilter{}, errors.New("invalid limit")
		}
		filter.Limit = value
	}

	if offset := query.Get("offset"); offset != "" {
		value, err := strconv.Atoi(offset)
		if err != nil || value < 0 {
			return ListToolsFilter{}, errors.New("invalid offset")
		}
		filter.Offset = value
	}

	return filter, nil
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

package catalog

import "context"

type CatalogRepository interface {
	ListCategories(context.Context) ([]Category, error)
	ListTools(context.Context, ListToolsFilter) ([]Tool, error)
	GetTool(context.Context, string) (Tool, error)
}

type Service struct {
	repository CatalogRepository
}

func NewService(repository CatalogRepository) *Service {
	return &Service{repository: repository}
}

func (service *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return service.repository.ListCategories(ctx)
}

func (service *Service) ListTools(ctx context.Context, filter ListToolsFilter) ([]Tool, error) {
	return service.repository.ListTools(ctx, filter)
}

func (service *Service) GetTool(ctx context.Context, id string) (Tool, error) {
	return service.repository.GetTool(ctx, id)
}

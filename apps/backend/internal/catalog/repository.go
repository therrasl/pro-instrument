package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("catalog item not found")

type databaseQueries interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	database databaseQueries
}

func NewRepository(database *pgxpool.Pool) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) ListCategories(ctx context.Context) ([]Category, error) {
	const query = `
		SELECT id::text, name, slug, created_at, updated_at
		FROM categories
		ORDER BY name, id
	`

	rows, err := repository.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(
			&category.ID,
			&category.Name,
			&category.Slug,
			&category.CreatedAt,
			&category.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, category)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}

	return categories, nil
}

func (repository *Repository) ListTools(ctx context.Context, filter ListToolsFilter) ([]Tool, error) {
	query, arguments := buildListToolsQuery(filter)
	rows, err := repository.database.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query tools: %w", err)
	}
	defer rows.Close()

	tools := make([]Tool, 0)
	for rows.Next() {
		tool, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tools: %w", err)
	}

	return tools, nil
}

func (repository *Repository) GetTool(ctx context.Context, id string) (Tool, error) {
	const query = `
		SELECT
			t.id::text,
			t.category_id::text,
			t.name,
			t.slug,
			t.short_description,
			t.description,
			COALESCE(
				(
					SELECT jsonb_agg(ti.image_url ORDER BY ti.position, ti.id)
					FROM tool_images AS ti
					WHERE ti.tool_id = t.id
				),
				'[]'::jsonb
			),
			t.specifications,
			t.equipment,
			t.daily_price,
			t.deposit_amount,
			t.is_active,
			COUNT(tu.id) FILTER (WHERE tu.status = 'available') AS available_units,
			t.created_at,
			t.updated_at
		FROM tools AS t
		LEFT JOIN tool_units AS tu ON tu.tool_id = t.id
		WHERE t.id = $1::uuid AND t.is_active = TRUE
		GROUP BY t.id
	`

	tool, err := scanTool(repository.database.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrNotFound
	}
	if err != nil {
		return Tool{}, err
	}

	return tool, nil
}

func buildListToolsQuery(filter ListToolsFilter) (string, []any) {
	var query strings.Builder
	query.WriteString(`
		SELECT
			t.id::text,
			t.category_id::text,
			t.name,
			t.slug,
			t.short_description,
			t.description,
			COALESCE(
				(
					SELECT jsonb_agg(ti.image_url ORDER BY ti.position, ti.id)
					FROM tool_images AS ti
					WHERE ti.tool_id = t.id
				),
				'[]'::jsonb
			),
			t.specifications,
			t.equipment,
			t.daily_price,
			t.deposit_amount,
			t.is_active,
			COUNT(tu.id) FILTER (WHERE tu.status = 'available') AS available_units,
			t.created_at,
			t.updated_at
		FROM tools AS t
		LEFT JOIN tool_units AS tu ON tu.tool_id = t.id
		WHERE t.is_active = TRUE
	`)

	arguments := make([]any, 0, 4)
	if filter.CategoryID != "" {
		arguments = append(arguments, filter.CategoryID)
		fmt.Fprintf(&query, " AND t.category_id = $%d::uuid", len(arguments))
	}
	if filter.Search != "" {
		arguments = append(arguments, filter.Search)
		placeholder := len(arguments)
		fmt.Fprintf(
			&query,
			" AND (t.name ILIKE '%%' || $%d || '%%' OR t.short_description ILIKE '%%' || $%d || '%%')",
			placeholder,
			placeholder,
		)
	}

	query.WriteString(" GROUP BY t.id")
	if filter.AvailableOnly {
		query.WriteString(" HAVING COUNT(tu.id) FILTER (WHERE tu.status = 'available') > 0")
	}

	arguments = append(arguments, filter.Limit)
	limitPlaceholder := len(arguments)
	arguments = append(arguments, filter.Offset)
	offsetPlaceholder := len(arguments)
	fmt.Fprintf(
		&query,
		" ORDER BY t.name, t.id LIMIT $%d OFFSET $%d",
		limitPlaceholder,
		offsetPlaceholder,
	)

	return query.String(), arguments
}

type rowScanner interface {
	Scan(...any) error
}

func scanTool(row rowScanner) (Tool, error) {
	var tool Tool
	var imageURLs []byte
	var specifications []byte
	var equipment []byte

	if err := row.Scan(
		&tool.ID,
		&tool.CategoryID,
		&tool.Name,
		&tool.Slug,
		&tool.ShortDescription,
		&tool.Description,
		&imageURLs,
		&specifications,
		&equipment,
		&tool.DailyPrice,
		&tool.DepositAmount,
		&tool.IsActive,
		&tool.AvailableUnits,
		&tool.CreatedAt,
		&tool.UpdatedAt,
	); err != nil {
		return Tool{}, fmt.Errorf("scan tool: %w", err)
	}

	if err := json.Unmarshal(imageURLs, &tool.ImageURLs); err != nil {
		return Tool{}, fmt.Errorf("decode tool images: %w", err)
	}
	tool.Specifications = json.RawMessage(specifications)
	tool.Equipment = json.RawMessage(equipment)

	return tool, nil
}

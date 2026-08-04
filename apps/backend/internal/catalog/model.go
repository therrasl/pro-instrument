package catalog

import (
	"encoding/json"
	"time"
)

type Category struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Tool struct {
	ID               string          `json:"id"`
	CategoryID       string          `json:"category_id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	ShortDescription string          `json:"short_description"`
	Description      string          `json:"description"`
	ImageURLs        []string        `json:"image_urls"`
	Specifications   json.RawMessage `json:"specifications"`
	Equipment        json.RawMessage `json:"equipment"`
	DailyPrice       int             `json:"daily_price"`
	DepositAmount    int             `json:"deposit_amount"`
	IsActive         bool            `json:"is_active"`
	AvailableUnits   int64           `json:"available_units"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type ListToolsFilter struct {
	CategoryID    string
	Search        string
	AvailableOnly bool
	Limit         int
	Offset        int
}

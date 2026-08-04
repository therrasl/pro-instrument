package push

import "time"

const (
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

type Delivery struct {
	ID        string
	TokenID   string
	Token     string
	RentalID  string
	Status    string
	Attempts  int
	CreatedAt time.Time
}

type Receipt struct {
	DeliveryID string
	TokenID    string
	TicketID   string
	Attempts   int
}

type Message struct {
	To        string         `json:"to"`
	Sound     string         `json:"sound"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Data      map[string]any `json:"data"`
	ChannelID string         `json:"channelId,omitempty"`
	Priority  string         `json:"priority,omitempty"`
}

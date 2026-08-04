package rentals

import "time"

const (
	DeliverySelfPickup = "self_pickup"
	DeliveryCourier    = "courier"

	StatusPendingManager  = "pending_manager"
	StatusAwaitingPayment = "awaiting_payment"
	StatusPaid            = "paid"
	StatusPreparing       = "preparing"
	StatusReady           = "ready"
	StatusHandedToCourier = "handed_to_courier"
	StatusRented          = "rented"
	StatusAwaitingReturn  = "awaiting_return"
	StatusInspection      = "inspection"
	StatusCompleted       = "completed"
	StatusRejected        = "rejected"
	StatusCancelled       = "cancelled"
	StatusPaymentExpired  = "payment_expired"
)

type QuoteRequest struct {
	ToolID          string
	StartDate       time.Time
	EndDate         time.Time
	DeliveryMethod  string
	DeliveryAddress string
}

type Quote struct {
	ToolID         string `json:"tool_id"`
	StartDate      string `json:"start_date"`
	EndDate        string `json:"end_date"`
	RentalDays     int    `json:"rental_days"`
	DailyPrice     int64  `json:"daily_price"`
	RentalPrice    int64  `json:"rental_price"`
	DepositAmount  int64  `json:"deposit_amount"`
	DeliveryCost   int64  `json:"delivery_cost"`
	TotalAmount    int64  `json:"total_amount"`
	DeliveryMethod string `json:"delivery_method"`
}

type RentalRequest struct {
	ID               string     `json:"id"`
	ClientID         string     `json:"client_id"`
	ToolID           string     `json:"tool_id"`
	ToolUnitID       string     `json:"tool_unit_id"`
	StartDate        string     `json:"start_date"`
	EndDate          string     `json:"end_date"`
	RentalDays       int        `json:"rental_days"`
	RentalPrice      int64      `json:"rental_price"`
	DepositAmount    int64      `json:"deposit_amount"`
	DeliveryCost     int64      `json:"delivery_cost"`
	TotalAmount      int64      `json:"total_amount"`
	DeliveryMethod   string     `json:"delivery_method"`
	DeliveryAddress  *string    `json:"delivery_address"`
	Status           string     `json:"status"`
	PaymentAvailable bool       `json:"payment_available"`
	ExpiresAt        time.Time  `json:"expires_at"`
	PaymentExpiresAt *time.Time `json:"payment_expires_at"`
	BitrixDealID     *string    `json:"bitrix_deal_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ToolPricing struct {
	DailyPrice    int64
	DepositAmount int64
}

type CreateCommand struct {
	ClientID        string
	ToolID          string
	StartDate       time.Time
	EndDate         time.Time
	RentalDays      int
	DeliveryMethod  string
	DeliveryAddress *string
	CourierFee      int64
	ExpiresAt       time.Time
	Now             time.Time
}

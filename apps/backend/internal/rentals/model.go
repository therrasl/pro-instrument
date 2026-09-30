package rentals

import (
	"os"
	"time"
)

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
	PickupAddress  string `json:"pickup_address,omitempty"`
}

type RentalRequest struct {
	ID               string     `json:"id"`
	OrderNumber      string     `json:"order_number"`
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
	PickupAddress    string     `json:"pickup_address,omitempty"`
	Status           string     `json:"status"`
	PaymentAvailable bool       `json:"payment_available"`
	ExpiresAt        time.Time  `json:"expires_at"`
	PaymentExpiresAt *time.Time `json:"payment_expires_at"`
	BitrixDealID     *string    `json:"bitrix_deal_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type OrderDocument struct {
	ID           string    `json:"id"`
	OrderNumber  string    `json:"order_number"`
	DocumentType string    `json:"document_type"`
	Title        string    `json:"title"`
	DownloadURL  string    `json:"download_url"`
	MIMEType     *string   `json:"mime_type"`
	CreatedAt    time.Time `json:"created_at"`
	StorageKey   *string   `json:"-"`
}

type DocumentContent struct {
	Document OrderDocument
	File     *os.File
	Size     int64
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

const (
	PhotoPhaseHandover = "handover"
	PhotoPhaseReturn   = "return"

	PhotoTypeBody         = "body"
	PhotoTypeEquipment    = "equipment"
	PhotoTypeBattery      = "battery"
	PhotoTypeSerialNumber = "serial_number"
	PhotoTypeCleanliness  = "cleanliness"

	ExtensionStatusPendingPayment = "pending_payment"
	ExtensionStatusPaid           = "paid"
	ExtensionStatusCancelled      = "cancelled"
	ExtensionStatusExpired        = "expired"
)

type ExtensionQuoteRequest struct {
	NewEndDate string `json:"new_end_date"`
}

type ExtensionQuote struct {
	RentalID        string `json:"rental_id"`
	OrderNumber     string `json:"order_number"`
	ToolID          string `json:"tool_id"`
	CurrentEndDate  string `json:"current_end_date"`
	NewEndDate      string `json:"new_end_date"`
	AdditionalDays  int    `json:"additional_days"`
	DailyPrice      int64  `json:"daily_price"`
	Amount          int64  `json:"amount"`
	Available       bool   `json:"available"`
}

type RentalExtension struct {
	ID               string     `json:"id"`
	RentalRequestID  string     `json:"rental_request_id"`
	PreviousEndDate  string     `json:"previous_end_date"`
	NewEndDate       string     `json:"new_end_date"`
	AdditionalDays   int        `json:"additional_days"`
	DailyPrice       int64      `json:"daily_price"`
	Amount           int64      `json:"amount"`
	Status           string     `json:"status"`
	PaymentID        *string    `json:"payment_id,omitempty"`
	ConfirmationURL  *string    `json:"confirmation_url,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	PaidAt           *time.Time `json:"paid_at,omitempty"`
}

type InspectionPhoto struct {
	ID              string    `json:"id"`
	RentalRequestID string    `json:"rental_request_id"`
	Phase           string    `json:"phase"`
	PhotoType       string    `json:"photo_type"`
	StorageKey      string    `json:"-"`
	FileName        string    `json:"file_name"`
	MIMEType        string    `json:"mime_type"`
	FileSize        int64     `json:"file_size"`
	Comment         string    `json:"comment,omitempty"`
	URL             string    `json:"url"`
	CreatedAt       time.Time `json:"created_at"`
}


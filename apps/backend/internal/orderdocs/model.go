package orderdocs

import "time"

const (
	RentalContract  = "rental_contract"
	Invoice         = "invoice"
	PaymentReceipt  = "payment_receipt"
	TransferAct     = "transfer_act"
	ReturnAct       = "return_act"
	ClosingDocument = "closing_document"
)

type RentalData struct {
	RentalID             string
	OrderNumber          string
	Status               string
	ToolName             string
	DailyPrice           int64
	RentalDays           int
	StartDate            string
	EndDate              string
	RentalPrice          int64
	DepositAmount        int64
	DeliveryCost         int64
	TotalAmount          int64
	DeliveryMethod       string
	DeliveryAddress      string
	ClientType           string
	ClientFullName       string
	ClientPhone          string
	CompanyName          string
	INN                  string
	KPP                  string
	OGRN                 string
	LegalAddress         string
	ActualAddress        string
	SettlementAccount    string
	BIK                  string
	CorrespondentAccount string
	BankName             string
	Email                string
	Phone                string
	ContactFullName      string
	ContactPosition      string
	HasSuccessfulPayment bool
	CreatedAt            time.Time
}

type GeneratedDocument struct {
	Type       string
	Title      string
	StorageKey string
}

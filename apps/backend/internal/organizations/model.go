package organizations

import "time"

type Organization struct {
	ClientID             string    `json:"client_id"`
	CompanyName          string    `json:"company_name"`
	INN                  string    `json:"inn"`
	KPP                  *string   `json:"kpp"`
	OGRN                 string    `json:"ogrn"`
	LegalAddress         string    `json:"legal_address"`
	ActualAddress        *string   `json:"actual_address"`
	SettlementAccount    *string   `json:"settlement_account"`
	BIK                  *string   `json:"bik"`
	CorrespondentAccount *string   `json:"correspondent_account"`
	BankName             *string   `json:"bank_name"`
	Email                string    `json:"email"`
	Phone                string    `json:"phone"`
	ContactFullName      string    `json:"contact_full_name"`
	ContactPosition      *string   `json:"contact_position"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type Input struct {
	CompanyName          string  `json:"company_name"`
	INN                  string  `json:"inn"`
	KPP                  *string `json:"kpp"`
	OGRN                 string  `json:"ogrn"`
	LegalAddress         string  `json:"legal_address"`
	ActualAddress        *string `json:"actual_address"`
	SettlementAccount    *string `json:"settlement_account"`
	BIK                  *string `json:"bik"`
	CorrespondentAccount *string `json:"correspondent_account"`
	BankName             *string `json:"bank_name"`
	Email                string  `json:"email"`
	Phone                string  `json:"phone"`
	ContactFullName      string  `json:"contact_full_name"`
	ContactPosition      *string `json:"contact_position"`
}

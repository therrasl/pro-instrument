package bitrix

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	EventContactUpsert = "bitrix.contact.upsert"
	EventDealCreate    = "bitrix.deal.create"
	EventDealUpdate    = "bitrix.deal.update"
)

type Client interface {
	FindContactByPhone(context.Context, string) (string, bool, error)
	CreateContact(context.Context, ContactInput) (string, error)
	FindDealByRentalID(context.Context, string, int) (string, bool, error)
	CreateDeal(context.Context, map[string]any) (string, error)
	UpdateDeal(context.Context, string, map[string]any) error
	GetDeal(context.Context, string) (DealState, error)
	GetDealFull(context.Context, string) (DealFull, error)
	GetContact(context.Context, string) (ContactDetails, error)
}

type ContactInput struct {
	FullName string
	Phone    string
}

type ContactDetails struct {
	ID       string
	FullName string
	Phone    string
}

type DealState struct {
	ID         string
	CategoryID string
	StageID    string
}

type DealFull struct {
	ID          string
	Title       string
	CategoryID  string
	StageID     string
	ContactID   string
	Opportunity string
	BeginDate   string
	CloseDate   string
	ToolName    string
	ClientName  string
	ClientPhone string
	Deposit     string
	Address     string
}

type APIError struct {
	StatusCode  int
	Code        string
	Description string
	Temporary   bool
}

func (err *APIError) Error() string {
	if err.Code != "" {
		return fmt.Sprintf("Bitrix API %s: %s", err.Code, err.Description)
	}
	return fmt.Sprintf("Bitrix API HTTP %d: %s", err.StatusCode, err.Description)
}

func IsTemporary(err error) bool {
	if err == nil {
		return false
	}
	var apiError *APIError
	if errors.As(err, &apiError) {
		return apiError.Temporary
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) &&
		(networkError.Timeout() || networkError.Temporary())
}

type OutboxEvent struct {
	ID            string
	EventType     string
	AggregateID   string
	Payload       []byte
	Attempts      int
	NextAttemptAt time.Time
}

type InboundEvent struct {
	ID           string
	EventKey     string
	BitrixDealID string
	RawPayload   []byte
	Attempts     int
}

type RentalSyncData struct {
	RentalID         string
	OrderNumber      string
	ClientID         string
	ClientFullName   string
	ClientPhone      string
	ClientType       string
	OrganizationName string
	INN              string
	ContactFullName  string
	ContactID        string
	DealID           string
	ToolName         string
	StartDate        string
	EndDate          string
	RentalDays       int
	RentalPrice      int64
	DepositAmount    int64
	DeliveryCost     int64
	TotalAmount      int64
	DeliveryMethod   string
	DeliveryAddress  string
	Status           string
}

type ClientSyncData struct {
	ClientID  string
	FullName  string
	Phone     string
	ContactID string
}

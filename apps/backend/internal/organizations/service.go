package organizations

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

var (
	ErrInvalidInput = errors.New("invalid organization input")
	ErrNotFound     = errors.New("organization not found")
)

var digits = regexp.MustCompile(`^[0-9]+$`)

type Repository interface {
	GetByClient(context.Context, string) (Organization, error)
	UpsertByClient(context.Context, string, Input) (Organization, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (service *Service) Get(ctx context.Context, clientID string) (Organization, error) {
	return service.repository.GetByClient(ctx, clientID)
}

func (service *Service) Put(ctx context.Context, clientID string, input Input) (Organization, error) {
	if err := normalizeAndValidate(&input); err != nil {
		return Organization{}, err
	}
	return service.repository.UpsertByClient(ctx, clientID, input)
}

func normalizeAndValidate(input *Input) error {
	input.CompanyName = strings.TrimSpace(input.CompanyName)
	input.INN = strings.TrimSpace(input.INN)
	input.OGRN = strings.TrimSpace(input.OGRN)
	input.LegalAddress = strings.TrimSpace(input.LegalAddress)
	input.Email = strings.TrimSpace(input.Email)
	input.ContactFullName = strings.TrimSpace(input.ContactFullName)
	phone, err := auth.NormalizePhone(input.Phone)
	if err != nil {
		return ErrInvalidInput
	}
	input.Phone = phone
	trimOptional := func(value **string) {
		if *value == nil {
			return
		}
		normalized := strings.TrimSpace(**value)
		if normalized == "" {
			*value = nil
		} else {
			*value = &normalized
		}
	}
	for _, value := range []**string{&input.KPP, &input.ActualAddress, &input.SettlementAccount, &input.BIK, &input.CorrespondentAccount, &input.BankName, &input.ContactPosition} {
		trimOptional(value)
	}
	if input.CompanyName == "" || len([]rune(input.CompanyName)) > 300 ||
		input.LegalAddress == "" || len([]rune(input.LegalAddress)) > 1000 ||
		input.ContactFullName == "" || len([]rune(input.ContactFullName)) > 200 ||
		!validDigits(input.INN, 10, 12) || !validDigits(input.OGRN, 13, 15) {
		return ErrInvalidInput
	}
	if input.KPP != nil && !validDigits(*input.KPP, 9) {
		return ErrInvalidInput
	}
	parsedEmail, err := mail.ParseAddress(input.Email)
	if err != nil || parsedEmail.Address != input.Email || len(input.Email) > 320 {
		return ErrInvalidInput
	}
	if tooLong(input.ActualAddress, 1000) || tooLong(input.BankName, 300) || tooLong(input.ContactPosition, 200) {
		return ErrInvalidInput
	}
	if input.SettlementAccount != nil && !validDigits(*input.SettlementAccount, 20) {
		return ErrInvalidInput
	}
	if input.BIK != nil && !validDigits(*input.BIK, 9) {
		return ErrInvalidInput
	}
	if input.CorrespondentAccount != nil && !validDigits(*input.CorrespondentAccount, 20) {
		return ErrInvalidInput
	}
	bankFields := input.SettlementAccount != nil || input.BIK != nil || input.CorrespondentAccount != nil || input.BankName != nil
	if bankFields && (input.SettlementAccount == nil || input.BIK == nil || input.CorrespondentAccount == nil || input.BankName == nil) {
		return ErrInvalidInput
	}
	return nil
}

func tooLong(value *string, maximum int) bool { return value != nil && len([]rune(*value)) > maximum }

func validDigits(value string, lengths ...int) bool {
	if !digits.MatchString(value) {
		return false
	}
	for _, length := range lengths {
		if len(value) == length {
			return true
		}
	}
	return false
}

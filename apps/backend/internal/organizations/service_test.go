package organizations

import (
	"context"
	"errors"
	"testing"
)

type fakeRepository struct {
	saved Input
	owner string
}

func (repository *fakeRepository) GetByClient(context.Context, string) (Organization, error) {
	return Organization{}, ErrNotFound
}
func (repository *fakeRepository) UpsertByClient(_ context.Context, clientID string, input Input) (Organization, error) {
	repository.owner = clientID
	repository.saved = input
	return Organization{ClientID: clientID, CompanyName: input.CompanyName, INN: input.INN}, nil
}
func validInput() Input {
	return Input{CompanyName: "ООО Тест", INN: "7705432109", KPP: pointer("770501001"), OGRN: "1157746123456", LegalAddress: "Москва", Email: "demo@example.test", Phone: "89990000077", ContactFullName: "Анна Соколова"}
}
func pointer(value string) *string { return &value }

func TestPutNormalizesAndScopesOrganizationToClient(t *testing.T) {
	repository := &fakeRepository{}
	result, err := NewService(repository).Put(context.Background(), "client-1", validInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.ClientID != "client-1" || repository.owner != "client-1" {
		t.Fatalf("wrong owner: %#v", result)
	}
	if repository.saved.Phone != "+79990000077" {
		t.Fatalf("phone not normalized: %q", repository.saved.Phone)
	}
}
func TestPutValidatesLegalIdentifiersAndOptionalBankBlock(t *testing.T) {
	for name, mutate := range map[string]func(*Input){"inn": func(input *Input) { input.INN = "123" }, "kpp": func(input *Input) { input.KPP = pointer("123") }, "ogrn": func(input *Input) { input.OGRN = "abc" }, "partial bank": func(input *Input) { input.BIK = pointer("044525225") }} {
		t.Run(name, func(t *testing.T) {
			input := validInput()
			mutate(&input)
			_, err := NewService(&fakeRepository{}).Put(context.Background(), "client", input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}
func TestPutAllowsBankDetailsToRemainEmpty(t *testing.T) {
	_, err := NewService(&fakeRepository{}).Put(context.Background(), "client", validInput())
	if err != nil {
		t.Fatal(err)
	}
}

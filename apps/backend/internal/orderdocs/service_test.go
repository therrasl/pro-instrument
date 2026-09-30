package orderdocs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeRepository struct {
	data  RentalData
	saved []GeneratedDocument
}

func (repository *fakeRepository) GetRental(context.Context, string) (RentalData, error) {
	return repository.data, nil
}
func (repository *fakeRepository) ListRentalIDs(context.Context, int) ([]string, error) {
	return []string{repository.data.RentalID}, nil
}
func (repository *fakeRepository) ListDocumentTypes(context.Context, string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, document := range repository.saved {
		result[document.Type] = true
	}
	return result, nil
}
func (repository *fakeRepository) SaveDocument(_ context.Context, _ RentalData, document GeneratedDocument) error {
	repository.saved = append(repository.saved, document)
	return nil
}
func demoData() RentalData {
	return RentalData{RentalID: "d1000000-0000-4000-8000-000000000077", OrderNumber: "PI-DEMO-UL-001", Status: "completed", ToolName: "Перфоратор Bosch GBH 2-26", StartDate: "2026-08-01", EndDate: "2026-08-03", RentalPrice: 360000, DepositAmount: 1000000, DeliveryCost: 100000, TotalAmount: 1460000, DeliveryMethod: "courier", DeliveryAddress: "Москва, ул. Дубининская, 57", CompanyName: "ООО «Демо Инструмент»", INN: "7705432109", KPP: "770501001", OGRN: "1157746123456", LegalAddress: "Москва, ул. Дубининская, 57", Email: "demo@example.test", Phone: "+79990000077", ContactFullName: "Анна Викторовна Соколова", ContactPosition: "Руководитель проектов", HasSuccessfulPayment: true, CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
}

func TestDocumentTypesFollowRentalStatus(t *testing.T) {
	cases := []struct {
		status string
		paid   bool
		want   []string
	}{{"pending_manager", false, []string{RentalContract, Invoice}}, {"paid", true, []string{RentalContract, Invoice, PaymentReceipt}}, {"rented", true, []string{RentalContract, Invoice, PaymentReceipt, TransferAct}}, {"completed", true, []string{RentalContract, Invoice, PaymentReceipt, TransferAct, ReturnAct, ClosingDocument}}}
	for _, test := range cases {
		t.Run(test.status, func(t *testing.T) {
			if got := documentTypesFor(test.status, test.paid); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}
func TestEnsureRentalGeneratesReadablePDFs(t *testing.T) {
	root := t.TempDir()
	repository := &fakeRepository{data: demoData()}
	service := NewService(repository, NewGenerator(root), nil)
	if err := service.EnsureRental(context.Background(), repository.data.RentalID); err != nil {
		t.Fatal(err)
	}
	if len(repository.saved) != 6 {
		t.Fatalf("generated %d documents", len(repository.saved))
	}
	if err := service.EnsureRental(context.Background(), repository.data.RentalID); err != nil {
		t.Fatal(err)
	}
	if len(repository.saved) != 6 {
		t.Fatalf("idempotent ensure created duplicates: %d", len(repository.saved))
	}
	for _, document := range repository.saved {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(document.StorageKey)))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 2000 || string(data[:4]) != "%PDF" {
			t.Fatalf("invalid PDF %s (%d bytes)", document.Type, len(data))
		}
	}
}
func TestGeneratePreviewPDFs(t *testing.T) {
	root := os.Getenv("PDF_PREVIEW_DIR")
	if root == "" {
		t.Skip("PDF_PREVIEW_DIR is not set")
	}
	data := demoData()
	generator := NewGenerator(root)
	for _, kind := range []string{RentalContract, Invoice, PaymentReceipt, TransferAct, ReturnAct, ClosingDocument} {
		if _, err := generator.Generate(data, kind); err != nil {
			t.Fatal(err)
		}
	}
}

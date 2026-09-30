package rentals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrderMigrationBackfillsNumbersAndKeepsDocumentsReal(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000013_order_numbers_b2b_documents.up.sql"))
	if err != nil {
		t.Fatalf("read order migration: %v", err)
	}
	migration := string(data)
	for _, fragment := range []string{
		"UPDATE rental_requests",
		"rental_requests_order_number_key UNIQUE",
		"CREATE TABLE rental_customer_snapshots",
		"CREATE TABLE order_documents",
		"file_url TEXT NOT NULL",
		"UNIQUE (rental_request_id, document_type)",
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("order migration does not contain %q", fragment)
		}
	}
}

func TestRentalDocumentsOrganizationMigrationContainsRequiredSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000014_rental_documents_organizations.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	migration := string(data)
	for _, fragment := range []string{"CREATE TABLE client_organizations", "ADD COLUMN storage_key", "rental_contract", "payment_receipt", "closing_document", "settlement_account", "contact_position"} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}

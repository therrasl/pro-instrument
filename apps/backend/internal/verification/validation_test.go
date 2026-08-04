package verification

import (
	"errors"
	"testing"
)

func TestValidateDocumentType(t *testing.T) {
	for _, value := range []string{
		string(DocumentPassportMain),
		string(DocumentPassportRegistration),
		string(DocumentSelfieWithPassport),
	} {
		if _, err := ValidateDocumentType(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}

	if _, err := ValidateDocumentType("other"); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("expected invalid document type, got %v", err)
	}
}

func TestValidateDocumentMIME(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		mimeType string
	}{
		{name: "jpeg", content: []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, mimeType: "image/jpeg"},
		{name: "png", content: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, mimeType: "image/png"},
		{name: "pdf", content: []byte("%PDF-1.7\n"), mimeType: "application/pdf"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mimeType, _, err := validateDocument(test.content, 1024)
			if err != nil {
				t.Fatalf("validate MIME: %v", err)
			}
			if mimeType != test.mimeType {
				t.Fatalf("expected MIME %q, got %q", test.mimeType, mimeType)
			}
		})
	}
}

func TestValidateDocumentRejectsSizeAndMIME(t *testing.T) {
	if _, _, err := validateDocument([]byte("plain text"), 1024); !errors.Is(err, ErrUnsupportedMIME) {
		t.Fatalf("expected unsupported MIME, got %v", err)
	}
	if _, _, err := validateDocument([]byte("%PDF-1.7\n"), 4); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("expected document too large, got %v", err)
	}
}

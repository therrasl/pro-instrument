package verification

import (
	"errors"
	"net/http"
)

var (
	ErrInvalidDocument  = errors.New("invalid document")
	ErrDocumentTooLarge = errors.New("document too large")
	ErrUnsupportedMIME  = errors.New("unsupported document MIME type")
)

func ValidateDocumentType(value string) (DocumentType, error) {
	documentType := DocumentType(value)
	switch documentType {
	case DocumentPassportMain, DocumentPassportRegistration, DocumentSelfieWithPassport:
		return documentType, nil
	default:
		return "", ErrInvalidDocument
	}
}

func validateDocument(data []byte, maximumSize int64) (string, string, error) {
	if len(data) == 0 {
		return "", "", ErrInvalidDocument
	}
	if int64(len(data)) > maximumSize {
		return "", "", ErrDocumentTooLarge
	}

	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg":
		return mimeType, ".jpg", nil
	case "image/png":
		return mimeType, ".png", nil
	case "application/pdf":
		return mimeType, ".pdf", nil
	default:
		return "", "", ErrUnsupportedMIME
	}
}

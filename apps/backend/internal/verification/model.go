package verification

import "time"

type DocumentType string

const (
	DocumentPassportMain         DocumentType = "passport_main"
	DocumentPassportRegistration DocumentType = "passport_registration"
	DocumentSelfieWithPassport   DocumentType = "selfie_with_passport"
)

var requiredDocumentTypes = []DocumentType{
	DocumentPassportMain,
	DocumentPassportRegistration,
	DocumentSelfieWithPassport,
}

type Document struct {
	ID           string       `json:"id"`
	ClientID     string       `json:"-"`
	DocumentType DocumentType `json:"document_type"`
	StorageKey   string       `json:"-"`
	MIMEType     string       `json:"mime_type"`
	SizeBytes    int64        `json:"size_bytes"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type Review struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	ReviewerID string    `json:"reviewer_id"`
	Decision   string    `json:"decision"`
	Reason     *string   `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

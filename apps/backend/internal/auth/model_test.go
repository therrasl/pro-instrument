package auth

import (
	"testing"
	"time"
)

func TestClientProgressComesFromPersistedFields(t *testing.T) {
	now := time.Now()
	name := "Иван Иванов"
	birthDate := "1990-01-01"
	client := Client{
		PhoneVerifiedAt: &now,
		OfferAcceptedAt: &now,
		FullName:        &name,
		BirthDate:       &birthDate,
	}

	client.setProgress()

	if !client.PhoneVerified || !client.OfferAccepted || !client.ProfileCompleted {
		t.Fatalf("unexpected client progress: %#v", client)
	}
}

package auth

import "time"

type Client struct {
	ID                          string     `json:"id"`
	Phone                       string     `json:"phone"`
	PhoneVerified               bool       `json:"phone_verified"`
	PhoneVerifiedAt             *time.Time `json:"phone_verified_at"`
	OfferAccepted               bool       `json:"offer_accepted"`
	OfferAcceptedAt             *time.Time `json:"offer_accepted_at"`
	ProfileCompleted            bool       `json:"profile_completed"`
	FullName                    *string    `json:"full_name"`
	BirthDate                   *string    `json:"birth_date"`
	Email                       *string    `json:"email"`
	Status                      string     `json:"status"`
	VerificationRejectionReason *string    `json:"verification_rejection_reason"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type Session struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type ProfilePatch struct {
	FullName  *string
	BirthDate *string
	Email     *string
}

type ConsentInput struct {
	OfferVersion          string
	PrivacyVersion        string
	OfferAccepted         bool
	PrivacyAccepted       bool
	DataAccuracyConfirmed bool
	RentalRulesAccepted   bool
	AcceptedAt            time.Time
	IPAddress             string
	UserAgent             string
}

type ConsentAcceptance struct {
	ID                    string    `json:"id"`
	OfferVersion          string    `json:"offer_version"`
	PrivacyVersion        string    `json:"privacy_version"`
	OfferAccepted         bool      `json:"offer_accepted"`
	PrivacyAccepted       bool      `json:"privacy_accepted"`
	DataAccuracyConfirmed bool      `json:"data_accuracy_confirmed"`
	RentalRulesAccepted   bool      `json:"rental_rules_accepted"`
	AcceptedAt            time.Time `json:"accepted_at"`
}

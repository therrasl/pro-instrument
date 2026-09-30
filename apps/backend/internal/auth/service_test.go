package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

const testHashSecret = "0123456789abcdef0123456789abcdef"

type fakeRepository struct {
	createCode    func(context.Context, string, []byte, time.Time, time.Time, time.Duration) error
	verifyCode    func(context.Context, string, []byte, []byte, time.Time, time.Time, int) (Client, error)
	authenticate  func(context.Context, []byte, time.Time) (Client, error)
	updateProfile func(context.Context, string, ProfilePatch) (Client, error)
	createConsent func(context.Context, string, ConsentInput) (ConsentAcceptance, error)
}

func (repository fakeRepository) CreateVerificationCode(
	ctx context.Context,
	phone string,
	codeHash []byte,
	expiresAt time.Time,
	createdAt time.Time,
	cooldown time.Duration,
) error {
	return repository.createCode(ctx, phone, codeHash, expiresAt, createdAt, cooldown)
}

func (repository fakeRepository) VerifyCodeAndCreateSession(
	ctx context.Context,
	phone string,
	codeHash []byte,
	tokenHash []byte,
	now time.Time,
	expiresAt time.Time,
	maxAttempts int,
) (Client, error) {
	return repository.verifyCode(ctx, phone, codeHash, tokenHash, now, expiresAt, maxAttempts)
}

func (repository fakeRepository) AuthenticateSession(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (Client, error) {
	return repository.authenticate(ctx, tokenHash, now)
}

func (repository fakeRepository) UpdateProfile(
	ctx context.Context,
	clientID string,
	patch ProfilePatch,
) (Client, error) {
	return repository.updateProfile(ctx, clientID, patch)
}

func (repository fakeRepository) CreateConsent(
	ctx context.Context,
	clientID string,
	input ConsentInput,
) (ConsentAcceptance, error) {
	return repository.createConsent(ctx, clientID, input)
}

type fakeSMSSender struct {
	phone string
	code  string
}

func (sender *fakeSMSSender) SendCode(_ context.Context, phone string, code string) error {
	sender.phone = phone
	sender.code = code
	return nil
}

func TestNormalizePhone(t *testing.T) {
	tests := map[string]string{
		"+7 (999) 123-45-67": "+79991234567",
		"8 999 123 45 67":    "+79991234567",
		"9991234567":         "+79991234567",
		"+4915112345678":     "+4915112345678",
	}

	for input, expected := range tests {
		actual, err := NormalizePhone(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("normalize %q: expected %q, got %q", input, expected, actual)
		}
	}
}

func TestNormalizePhoneRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"", "++79991234567", "7999+1234567", "phone"} {
		if _, err := NormalizePhone(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected %q to be rejected, got %v", input, err)
		}
	}
}

func TestDisabledDemoModePreservesExistingOTPFlow(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	sender := &fakeSMSSender{}
	var storedHash []byte

	repository := fakeRepository{
		createCode: func(
			_ context.Context,
			phone string,
			codeHash []byte,
			expiresAt time.Time,
			createdAt time.Time,
			cooldown time.Duration,
		) error {
			if phone != "+79991234567" {
				t.Fatalf("unexpected phone: %q", phone)
			}
			if !expiresAt.Equal(now.Add(5*time.Minute)) || !createdAt.Equal(now) || cooldown != time.Minute {
				t.Fatal("unexpected verification timing")
			}
			storedHash = append([]byte(nil), codeHash...)
			return nil
		},
	}

	service := NewService(repository, sender, 5*time.Minute, 24*time.Hour, testHashSecret)
	service.now = func() time.Time { return now }
	service.randomCode = func() (string, error) { return "123456", nil }

	if err := service.RequestCode(context.Background(), "8 999 123-45-67"); err != nil {
		t.Fatalf("request code: %v", err)
	}
	if sender.phone != "+79991234567" || sender.code != "123456" {
		t.Fatalf("unexpected SMS: phone=%q code=%q", sender.phone, sender.code)
	}
	if bytes.Equal(storedHash, []byte("123456")) || !bytes.Equal(storedHash, hashOTP([]byte(testHashSecret), sender.phone, sender.code)) {
		t.Fatal("verification code was not stored as the expected HMAC")
	}
}

func TestDemoOTPAllowedPhoneUsesFixedHashedCodeWithoutSMS(t *testing.T) {
	sender := &fakeSMSSender{}
	var storedHash []byte
	repository := fakeRepository{
		createCode: func(
			_ context.Context,
			phone string,
			codeHash []byte,
			_ time.Time,
			_ time.Time,
			_ time.Duration,
		) error {
			if phone != "+79991234567" {
				t.Fatalf("phone was not normalized: %q", phone)
			}
			storedHash = append([]byte(nil), codeHash...)
			return nil
		},
	}
	service := NewService(
		repository,
		sender,
		5*time.Minute,
		24*time.Hour,
		testHashSecret,
		WithDemoOTP("654321", []string{"+79991234567"}),
	)
	service.randomCode = func() (string, error) { return "123456", nil }

	if err := service.RequestCode(context.Background(), "8 (999) 123-45-67"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedHash, hashOTP([]byte(testHashSecret), "+79991234567", "654321")) {
		t.Fatal("allowed demo phone did not receive the fixed hashed code")
	}
	if sender.phone != "" || sender.code != "" {
		t.Fatal("demo mode must not call the SMS sender")
	}
}

func TestDemoOTPForeignPhoneCannotUseFixedCode(t *testing.T) {
	var storedHash []byte
	repository := fakeRepository{
		createCode: func(
			_ context.Context,
			_ string,
			codeHash []byte,
			_ time.Time,
			_ time.Time,
			_ time.Duration,
		) error {
			storedHash = append([]byte(nil), codeHash...)
			return nil
		},
	}
	service := NewService(
		repository,
		&fakeSMSSender{},
		5*time.Minute,
		24*time.Hour,
		testHashSecret,
		WithDemoOTP("654321", []string{"+79991234567"}),
	)
	service.randomCode = func() (string, error) { return "654321", nil }

	if err := service.RequestCode(context.Background(), "+79990000000"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(storedHash, hashOTP([]byte(testHashSecret), "+79990000000", "654321")) {
		t.Fatal("foreign phone received the fixed demo code")
	}
}

func TestDemoOTPRejectsWrongCode(t *testing.T) {
	expectedHash := hashOTP([]byte(testHashSecret), "+79991234567", "654321")
	repository := fakeRepository{
		verifyCode: func(
			_ context.Context,
			_ string,
			codeHash []byte,
			_ []byte,
			_ time.Time,
			_ time.Time,
			_ int,
		) (Client, error) {
			if !bytes.Equal(codeHash, expectedHash) {
				return Client{}, ErrInvalidCode
			}
			return Client{}, nil
		},
	}
	service := NewService(
		repository,
		&fakeSMSSender{},
		5*time.Minute,
		24*time.Hour,
		testHashSecret,
		WithDemoOTP("654321", []string{"+79991234567"}),
	)
	service.randomToken = func() (string, error) { return "session-token", nil }

	if _, err := service.VerifyCode(
		context.Background(),
		"+79991234567",
		"111111",
	); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expected wrong demo code to fail, got %v", err)
	}
}

func TestVerifyCodeReturnsRawTokenAndStoresOnlyHash(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	var storedTokenHash []byte

	repository := fakeRepository{
		verifyCode: func(
			_ context.Context,
			phone string,
			codeHash []byte,
			tokenHash []byte,
			actualNow time.Time,
			expiresAt time.Time,
			maxAttempts int,
		) (Client, error) {
			if phone != "+79991234567" ||
				!bytes.Equal(codeHash, hashOTP([]byte(testHashSecret), phone, "123456")) ||
				!actualNow.Equal(now) ||
				!expiresAt.Equal(now.Add(24*time.Hour)) ||
				maxAttempts != 5 {
				t.Fatal("unexpected verification parameters")
			}
			storedTokenHash = append([]byte(nil), tokenHash...)
			return Client{ID: "client-id"}, nil
		},
	}

	service := NewService(repository, &fakeSMSSender{}, 5*time.Minute, 24*time.Hour, testHashSecret)
	service.now = func() time.Time { return now }
	service.randomToken = func() (string, error) { return "raw-session-token", nil }

	session, err := service.VerifyCode(context.Background(), "+79991234567", "123456")
	if err != nil {
		t.Fatalf("verify code: %v", err)
	}
	if session.AccessToken != "raw-session-token" || session.ExpiresIn != 86400 {
		t.Fatalf("unexpected session: %#v", session)
	}
	if bytes.Equal(storedTokenHash, []byte(session.AccessToken)) ||
		!bytes.Equal(storedTokenHash, hashToken(session.AccessToken)) {
		t.Fatal("session token was not stored as SHA-256")
	}
}

func TestUpdateProfileRejectsInvalidBirthDate(t *testing.T) {
	service := NewService(fakeRepository{}, &fakeSMSSender{}, 5*time.Minute, 24*time.Hour, testHashSecret)
	birthDate := "not-a-date"

	_, err := service.UpdateProfile(context.Background(), "client-id", ProfilePatch{BirthDate: &birthDate})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestUpdateLegalEntityProfileValidatesAndPersistsRequisites(t *testing.T) {
	clientType := "legal_entity"
	fullName := "Иванов Иван"
	email := "office@example.ru"
	companyName := "ООО Про Инструмент"
	inn := "7701234567"
	kpp := "770101001"
	ogrn := "1027700123456"
	address := "Москва, ул. Складская, 10"
	contact := "Иванов Иван, директор"
	repository := fakeRepository{updateProfile: func(_ context.Context, _ string, patch ProfilePatch) (Client, error) {
		if patch.ClientType == nil || *patch.ClientType != clientType ||
			patch.CompanyName == nil || *patch.CompanyName != companyName ||
			patch.INN == nil || *patch.INN != inn || patch.KPP == nil || *patch.KPP != kpp ||
			patch.OGRN == nil || *patch.OGRN != ogrn ||
			patch.LegalAddress == nil || *patch.LegalAddress != address {
			t.Fatalf("unexpected legal profile patch: %#v", patch)
		}
		return Client{ClientType: clientType}, nil
	}}
	service := NewService(repository, &fakeSMSSender{}, 5*time.Minute, 24*time.Hour, testHashSecret)
	_, err := service.UpdateProfile(context.Background(), "client-id", ProfilePatch{
		ClientType: &clientType, FullName: &fullName, Email: &email,
		CompanyName: &companyName, INN: &inn, KPP: &kpp, OGRN: &ogrn,
		LegalAddress: &address, CompanyContact: &contact,
	})
	if err != nil {
		t.Fatalf("update legal profile: %v", err)
	}
}

func TestAcceptConsentsRequiresEveryConfirmation(t *testing.T) {
	service := NewService(fakeRepository{}, &fakeSMSSender{}, 5*time.Minute, 24*time.Hour, testHashSecret)

	_, err := service.AcceptConsents(context.Background(), "client-id", ConsentInput{
		OfferVersion:          "offer-v1",
		PrivacyVersion:        "privacy-v1",
		OfferAccepted:         true,
		PrivacyAccepted:       true,
		DataAccuracyConfirmed: true,
		RentalRulesAccepted:   false,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

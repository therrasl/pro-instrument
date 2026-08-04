package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrRateLimited    = errors.New("verification code rate limited")
	ErrInvalidCode    = errors.New("invalid verification code")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrUnsupportedEnv = errors.New("SMS sender is only configured for development")
)

const (
	verificationCodeDigits   = 6
	verificationMaxAttempts  = 5
	verificationCodeCooldown = time.Minute
	sessionTokenBytes        = 32
)

type AuthRepository interface {
	CreateVerificationCode(context.Context, string, []byte, time.Time, time.Time, time.Duration) error
	VerifyCodeAndCreateSession(context.Context, string, []byte, []byte, time.Time, time.Time, int) (Client, error)
	AuthenticateSession(context.Context, []byte, time.Time) (Client, error)
	UpdateProfile(context.Context, string, ProfilePatch) (Client, error)
	CreateConsent(context.Context, string, ConsentInput) (ConsentAcceptance, error)
}

type Service struct {
	repository    AuthRepository
	smsSender     SMSSender
	otpTTL        time.Duration
	sessionTTL    time.Duration
	otpHashSecret []byte
	now           func() time.Time
	randomCode    func() (string, error)
	randomToken   func() (string, error)
	demoEnabled   bool
	demoCode      string
	demoPhones    map[string]struct{}
}

type ServiceOption func(*Service)

func WithDemoOTP(code string, phones []string) ServiceOption {
	return func(service *Service) {
		service.demoEnabled = true
		service.demoCode = code
		service.demoPhones = make(map[string]struct{}, len(phones))
		for _, phone := range phones {
			service.demoPhones[phone] = struct{}{}
		}
	}
}

func NewService(
	repository AuthRepository,
	smsSender SMSSender,
	otpTTL time.Duration,
	sessionTTL time.Duration,
	otpHashSecret string,
	options ...ServiceOption,
) *Service {
	service := &Service{
		repository:    repository,
		smsSender:     smsSender,
		otpTTL:        otpTTL,
		sessionTTL:    sessionTTL,
		otpHashSecret: []byte(otpHashSecret),
		now:           time.Now,
		randomCode:    generateCode,
		randomToken:   generateToken,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (service *Service) RequestCode(ctx context.Context, rawPhone string) error {
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return ErrInvalidInput
	}

	code, err := service.randomCode()
	if err != nil {
		return fmt.Errorf("generate verification code: %w", err)
	}
	if service.demoEnabled {
		if _, allowed := service.demoPhones[phone]; allowed {
			code = service.demoCode
		} else if code == service.demoCode {
			code = nextVerificationCode(code)
		}
	}

	now := service.now().UTC()
	if err := service.repository.CreateVerificationCode(
		ctx,
		phone,
		hashOTP(service.otpHashSecret, phone, code),
		now.Add(service.otpTTL),
		now,
		verificationCodeCooldown,
	); err != nil {
		return err
	}

	if service.demoEnabled {
		return nil
	}
	if err := service.smsSender.SendCode(ctx, phone, code); err != nil {
		return fmt.Errorf("send verification code: %w", err)
	}

	return nil
}

func nextVerificationCode(code string) string {
	value := 0
	for _, character := range code {
		value = value*10 + int(character-'0')
	}
	return fmt.Sprintf("%06d", (value+1)%1_000_000)
}

func (service *Service) VerifyCode(ctx context.Context, rawPhone string, code string) (Session, error) {
	phone, err := NormalizePhone(rawPhone)
	if err != nil || !validCode(code) {
		return Session{}, ErrInvalidCode
	}

	token, err := service.randomToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}

	now := service.now().UTC()
	expiresAt := now.Add(service.sessionTTL)
	_, err = service.repository.VerifyCodeAndCreateSession(
		ctx,
		phone,
		hashOTP(service.otpHashSecret, phone, code),
		hashToken(token),
		now,
		expiresAt,
		verificationMaxAttempts,
	)
	if err != nil {
		return Session{}, err
	}

	return Session{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(service.sessionTTL.Seconds()),
	}, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (Client, error) {
	if token == "" {
		return Client{}, ErrUnauthorized
	}

	client, err := service.repository.AuthenticateSession(ctx, hashToken(token), service.now().UTC())
	if err != nil {
		return Client{}, err
	}

	return client, nil
}

func (service *Service) UpdateProfile(ctx context.Context, clientID string, patch ProfilePatch) (Client, error) {
	if patch.FullName == nil && patch.BirthDate == nil && patch.Email == nil {
		return Client{}, ErrInvalidInput
	}

	if patch.FullName != nil {
		value := strings.TrimSpace(*patch.FullName)
		if len([]rune(value)) < 2 || len([]rune(value)) > 200 {
			return Client{}, ErrInvalidInput
		}
		patch.FullName = &value
	}
	if patch.BirthDate != nil {
		birthDate, err := time.Parse("2006-01-02", *patch.BirthDate)
		if err != nil || birthDate.After(service.now().UTC()) {
			return Client{}, ErrInvalidInput
		}
		value := birthDate.Format("2006-01-02")
		patch.BirthDate = &value
	}
	if patch.Email != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.Email))
		if value != "" {
			address, err := mail.ParseAddress(value)
			if err != nil || address.Address != value || len(value) > 320 {
				return Client{}, ErrInvalidInput
			}
		}
		patch.Email = &value
	}

	return service.repository.UpdateProfile(ctx, clientID, patch)
}

func (service *Service) AcceptConsents(
	ctx context.Context,
	clientID string,
	input ConsentInput,
) (ConsentAcceptance, error) {
	input.OfferVersion = strings.TrimSpace(input.OfferVersion)
	input.PrivacyVersion = strings.TrimSpace(input.PrivacyVersion)
	if input.OfferVersion == "" ||
		input.PrivacyVersion == "" ||
		len(input.OfferVersion) > 100 ||
		len(input.PrivacyVersion) > 100 ||
		!input.OfferAccepted ||
		!input.PrivacyAccepted ||
		!input.DataAccuracyConfirmed ||
		!input.RentalRulesAccepted {
		return ConsentAcceptance{}, ErrInvalidInput
	}

	input.AcceptedAt = service.now().UTC()
	return service.repository.CreateConsent(ctx, clientID, input)
}

func NormalizePhone(rawPhone string) (string, error) {
	var digits strings.Builder
	sawPlus := false
	for index, character := range strings.TrimSpace(rawPhone) {
		if unicode.IsDigit(character) && character <= unicode.MaxASCII {
			digits.WriteRune(character)
			continue
		}
		if character == '+' {
			if index != 0 || sawPlus {
				return "", ErrInvalidInput
			}
			sawPlus = true
			continue
		}
		if character != ' ' &&
			character != '-' &&
			character != '(' &&
			character != ')' {
			return "", ErrInvalidInput
		}
	}

	value := digits.String()
	switch {
	case len(value) == 10:
		value = "7" + value
	case len(value) == 11 && value[0] == '8':
		value = "7" + value[1:]
	}

	if len(value) < 10 || len(value) > 15 || value[0] == '0' {
		return "", ErrInvalidInput
	}

	return "+" + value, nil
}

func validCode(code string) bool {
	if len(code) != verificationCodeDigits {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func generateCode() (string, error) {
	maximum := big.NewInt(1_000_000)
	value, err := rand.Int(rand.Reader, maximum)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func generateToken() (string, error) {
	value := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashOTP(secret []byte, phone string, code string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(phone))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))
	return mac.Sum(nil)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
)

const (
	defaultPort                       = "8080"
	defaultOTPTTL                     = 5 * time.Minute
	defaultSessionTTL                 = 30 * 24 * time.Hour
	defaultAppEnv                     = "development"
	defaultStoragePath                = "./storage"
	defaultMaxUploadSize        int64 = 10 * 1024 * 1024
	defaultRentalHoldTTL              = 30 * time.Minute
	defaultCourierFee           int64 = 100_000
	defaultBitrixTimeout              = 10 * time.Second
	defaultBitrixPollInterval         = 2 * time.Second
	defaultBitrixRetryBase            = 30 * time.Second
	defaultBitrixMaxAttempts          = 10
	defaultBitrixCategoryID           = 12
	defaultYooKassaTimeout            = 10 * time.Second
	defaultYooKassaPollInterval       = 2 * time.Second
	defaultYooKassaRetryBase          = 30 * time.Second
	defaultYooKassaMaxAttempts        = 10
)

type LookupEnv func(string) string

type Config struct {
	DatabaseURL   string
	Port          string
	OTPTTL        time.Duration
	SessionTTL    time.Duration
	AppEnv        string
	OTPHashSecret string
	StoragePath   string
	MaxUploadSize int64
	RentalHoldTTL time.Duration
	CourierFee    int64
	Bitrix        BitrixConfig
	YooKassa      YooKassaConfig
	Demo          DemoConfig
}

type DemoConfig struct {
	Enabled   bool
	OTPCode   string
	OTPPhones []string
}

type BitrixConfig struct {
	Enabled       bool
	BaseURL       string
	WebhookSecret string
	HTTPTimeout   time.Duration
	PollInterval  time.Duration
	RetryBase     time.Duration
	MaxAttempts   int
	CategoryID    int
	Stages        BitrixStages
	Fields        BitrixFields
}

type BitrixStages struct {
	Application     string
	AwaitingPayment string
	Paid            string
	Preparing       string
	Ready           string
	Courier         string
	Rented          string
	AwaitingReturn  string
	Inspection      string
	Completed       string
	Failed          string
}

type BitrixFields struct {
	RentalID    string
	Tool        string
	StartDate   string
	EndDate     string
	RentalPrice string
	Deposit     string
	Delivery    string
	Address     string
}

type YooKassaConfig struct {
	Enabled         bool
	ShopID          string
	SecretKey       string
	ReturnURL       string
	ReceiptsEnabled bool
	PublicBaseURL   string
	HTTPTimeout     time.Duration
	PollInterval    time.Duration
	RetryBase       time.Duration
	MaxAttempts     int
}

func Load(lookup LookupEnv) (Config, error) {
	databaseURL, err := LoadDatabaseURL(lookup)
	if err != nil {
		return Config{}, err
	}

	port := lookup("PORT")
	if port == "" {
		port = defaultPort
	}

	otpTTL, err := duration(lookup("OTP_TTL"), defaultOTPTTL, "OTP_TTL")
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := duration(lookup("SESSION_TTL"), defaultSessionTTL, "SESSION_TTL")
	if err != nil {
		return Config{}, err
	}

	appEnv := lookup("APP_ENV")
	if appEnv == "" {
		appEnv = defaultAppEnv
	}

	otpHashSecret := lookup("OTP_HASH_SECRET")
	if len(otpHashSecret) < 32 {
		return Config{}, errors.New("OTP_HASH_SECRET must contain at least 32 characters")
	}

	storagePath := lookup("STORAGE_PATH")
	if storagePath == "" {
		storagePath = defaultStoragePath
	}

	maxUploadSize := defaultMaxUploadSize
	if value := lookup("MAX_UPLOAD_SIZE"); value != "" {
		maxUploadSize, err = strconv.ParseInt(value, 10, 64)
		if err != nil || maxUploadSize <= 0 {
			return Config{}, errors.New("MAX_UPLOAD_SIZE must be a positive integer")
		}
	}

	rentalHoldTTL, err := duration(
		lookup("RENTAL_HOLD_TTL"),
		defaultRentalHoldTTL,
		"RENTAL_HOLD_TTL",
	)
	if err != nil {
		return Config{}, err
	}

	courierFee := defaultCourierFee
	if value := lookup("COURIER_DELIVERY_FEE_KOPECKS"); value != "" {
		courierFee, err = strconv.ParseInt(value, 10, 64)
		if err != nil || courierFee < 0 {
			return Config{}, errors.New("COURIER_DELIVERY_FEE_KOPECKS must be a non-negative integer")
		}
	}

	bitrixConfig, err := loadBitrixConfig(lookup)
	if err != nil {
		return Config{}, err
	}
	yooKassaConfig, err := loadYooKassaConfig(lookup)
	if err != nil {
		return Config{}, err
	}
	demoConfig, err := loadDemoConfig(lookup)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:   databaseURL,
		Port:          port,
		OTPTTL:        otpTTL,
		SessionTTL:    sessionTTL,
		AppEnv:        appEnv,
		OTPHashSecret: otpHashSecret,
		StoragePath:   storagePath,
		MaxUploadSize: maxUploadSize,
		RentalHoldTTL: rentalHoldTTL,
		CourierFee:    courierFee,
		Bitrix:        bitrixConfig,
		YooKassa:      yooKassaConfig,
		Demo:          demoConfig,
	}, nil
}

func loadDemoConfig(lookup LookupEnv) (DemoConfig, error) {
	enabled := false
	if value := strings.TrimSpace(lookup("DEMO_MODE_ENABLED")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return DemoConfig{}, errors.New("DEMO_MODE_ENABLED must be a boolean")
		}
		enabled = parsed
	}
	if !enabled {
		return DemoConfig{}, nil
	}

	code := strings.TrimSpace(lookup("DEMO_OTP_CODE"))
	if len(code) != 6 {
		return DemoConfig{}, errors.New("DEMO_OTP_CODE must contain exactly 6 digits when demo mode is enabled")
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return DemoConfig{}, errors.New("DEMO_OTP_CODE must contain exactly 6 digits when demo mode is enabled")
		}
	}

	rawPhones := strings.TrimSpace(lookup("DEMO_OTP_PHONES"))
	if rawPhones == "" {
		return DemoConfig{}, errors.New("DEMO_OTP_PHONES must contain at least one phone when demo mode is enabled")
	}
	phones := make([]string, 0)
	seen := make(map[string]struct{})
	for index, rawPhone := range strings.Split(rawPhones, ",") {
		phone, err := auth.NormalizePhone(strings.TrimSpace(rawPhone))
		if err != nil {
			return DemoConfig{}, fmt.Errorf("DEMO_OTP_PHONES entry %d is invalid", index+1)
		}
		if _, exists := seen[phone]; exists {
			continue
		}
		seen[phone] = struct{}{}
		phones = append(phones, phone)
	}

	return DemoConfig{Enabled: true, OTPCode: code, OTPPhones: phones}, nil
}

func LoadDatabaseURL(lookup LookupEnv) (string, error) {
	databaseURL := lookup("DATABASE_URL")
	if databaseURL == "" {
		return "", errors.New("DATABASE_URL is required")
	}
	return databaseURL, nil
}

func LoadBitrix(lookup LookupEnv) (BitrixConfig, error) {
	return loadBitrixConfig(lookup)
}

func LoadYooKassa(lookup LookupEnv) (YooKassaConfig, error) {
	return loadYooKassaConfig(lookup)
}

func LoadRentalHoldTTL(lookup LookupEnv) (time.Duration, error) {
	return duration(
		lookup("RENTAL_HOLD_TTL"),
		defaultRentalHoldTTL,
		"RENTAL_HOLD_TTL",
	)
}

func duration(value string, fallback time.Duration, name string) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}

	return parsed, nil
}

func loadBitrixConfig(lookup LookupEnv) (BitrixConfig, error) {
	enabled := false
	if value := lookup("BITRIX_ENABLED"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return BitrixConfig{}, errors.New("BITRIX_ENABLED must be a boolean")
		}
		enabled = parsed
	}

	httpTimeout, err := duration(
		lookup("BITRIX_HTTP_TIMEOUT"),
		defaultBitrixTimeout,
		"BITRIX_HTTP_TIMEOUT",
	)
	if err != nil {
		return BitrixConfig{}, err
	}
	pollInterval, err := duration(
		lookup("BITRIX_POLL_INTERVAL"),
		defaultBitrixPollInterval,
		"BITRIX_POLL_INTERVAL",
	)
	if err != nil {
		return BitrixConfig{}, err
	}
	retryBase, err := duration(
		lookup("BITRIX_RETRY_BASE"),
		defaultBitrixRetryBase,
		"BITRIX_RETRY_BASE",
	)
	if err != nil {
		return BitrixConfig{}, err
	}

	maxAttempts := defaultBitrixMaxAttempts
	if value := lookup("BITRIX_MAX_ATTEMPTS"); value != "" {
		maxAttempts, err = strconv.Atoi(value)
		if err != nil || maxAttempts < 1 {
			return BitrixConfig{}, errors.New("BITRIX_MAX_ATTEMPTS must be a positive integer")
		}
	}

	categoryID := defaultBitrixCategoryID
	if value := lookup("BITRIX_DEAL_CATEGORY_ID"); value != "" {
		categoryID, err = strconv.Atoi(value)
		if err != nil || categoryID < 0 {
			return BitrixConfig{}, errors.New("BITRIX_DEAL_CATEGORY_ID must be a non-negative integer")
		}
	}

	baseURL := strings.TrimSpace(lookup("BITRIX_BASE_URL"))
	webhookSecret := lookup("BITRIX_WEBHOOK_SECRET")
	if enabled {
		parsedURL, parseErr := url.Parse(baseURL)
		if parseErr != nil ||
			(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
			parsedURL.Host == "" {
			return BitrixConfig{}, errors.New("BITRIX_BASE_URL must be a valid HTTP(S) URL when Bitrix is enabled")
		}
		if len(webhookSecret) < 32 {
			return BitrixConfig{}, errors.New(
				"BITRIX_WEBHOOK_SECRET must contain at least 32 characters when Bitrix is enabled",
			)
		}
	}

	stage := func(name string, fallback string) string {
		if value := strings.TrimSpace(lookup(name)); value != "" {
			return value
		}
		return fallback
	}

	return BitrixConfig{
		Enabled:       enabled,
		BaseURL:       baseURL,
		WebhookSecret: webhookSecret,
		HTTPTimeout:   httpTimeout,
		PollInterval:  pollInterval,
		RetryBase:     retryBase,
		MaxAttempts:   maxAttempts,
		CategoryID:    categoryID,
		Stages: BitrixStages{
			Application:     stage("BITRIX_STAGE_APPLICATION", "C12:NEW"),
			AwaitingPayment: stage("BITRIX_STAGE_AWAITING_PAYMENT", "C12:UC_RRNJ1L"),
			Paid:            stage("BITRIX_STAGE_PAID", "C12:UC_48E30A"),
			Preparing:       stage("BITRIX_STAGE_PREPARING", "C12:UC_KQXWS6"),
			Ready:           stage("BITRIX_STAGE_READY", "C12:UC_3C6VH5"),
			Courier:         stage("BITRIX_STAGE_COURIER", "C12:UC_NC65FK"),
			Rented:          stage("BITRIX_STAGE_RENTED", "C12:UC_PL3Q5Q"),
			AwaitingReturn:  stage("BITRIX_STAGE_AWAITING_RETURN", "C12:UC_DHV3I2"),
			Inspection:      stage("BITRIX_STAGE_INSPECTION", "C12:UC_6WE1PX"),
			Completed:       stage("BITRIX_STAGE_COMPLETED", "C12:WON"),
			Failed:          stage("BITRIX_STAGE_FAILED", "C12:LOSE"),
		},
		Fields: BitrixFields{
			RentalID:    strings.TrimSpace(lookup("BITRIX_FIELD_RENTAL_ID")),
			Tool:        strings.TrimSpace(lookup("BITRIX_FIELD_TOOL")),
			StartDate:   strings.TrimSpace(lookup("BITRIX_FIELD_START_DATE")),
			EndDate:     strings.TrimSpace(lookup("BITRIX_FIELD_END_DATE")),
			RentalPrice: strings.TrimSpace(lookup("BITRIX_FIELD_RENTAL_PRICE")),
			Deposit:     strings.TrimSpace(lookup("BITRIX_FIELD_DEPOSIT")),
			Delivery:    strings.TrimSpace(lookup("BITRIX_FIELD_DELIVERY")),
			Address:     strings.TrimSpace(lookup("BITRIX_FIELD_ADDRESS")),
		},
	}, nil
}

func loadYooKassaConfig(lookup LookupEnv) (YooKassaConfig, error) {
	enabled, err := boolean(lookup("YOOKASSA_ENABLED"), false, "YOOKASSA_ENABLED")
	if err != nil {
		return YooKassaConfig{}, err
	}
	receiptsEnabled, err := boolean(
		lookup("YOOKASSA_RECEIPTS_ENABLED"),
		false,
		"YOOKASSA_RECEIPTS_ENABLED",
	)
	if err != nil {
		return YooKassaConfig{}, err
	}
	httpTimeout, err := duration(
		lookup("YOOKASSA_HTTP_TIMEOUT"),
		defaultYooKassaTimeout,
		"YOOKASSA_HTTP_TIMEOUT",
	)
	if err != nil {
		return YooKassaConfig{}, err
	}
	pollInterval, err := duration(
		lookup("YOOKASSA_POLL_INTERVAL"),
		defaultYooKassaPollInterval,
		"YOOKASSA_POLL_INTERVAL",
	)
	if err != nil {
		return YooKassaConfig{}, err
	}
	retryBase, err := duration(
		lookup("YOOKASSA_RETRY_BASE"),
		defaultYooKassaRetryBase,
		"YOOKASSA_RETRY_BASE",
	)
	if err != nil {
		return YooKassaConfig{}, err
	}

	maxAttempts := defaultYooKassaMaxAttempts
	if value := lookup("YOOKASSA_MAX_ATTEMPTS"); value != "" {
		maxAttempts, err = strconv.Atoi(value)
		if err != nil || maxAttempts < 1 {
			return YooKassaConfig{}, errors.New("YOOKASSA_MAX_ATTEMPTS must be a positive integer")
		}
	}

	settings := YooKassaConfig{
		Enabled:         enabled,
		ShopID:          strings.TrimSpace(lookup("YOOKASSA_SHOP_ID")),
		SecretKey:       lookup("YOOKASSA_SECRET_KEY"),
		ReturnURL:       strings.TrimSpace(lookup("YOOKASSA_RETURN_URL")),
		ReceiptsEnabled: receiptsEnabled,
		PublicBaseURL:   strings.TrimRight(strings.TrimSpace(lookup("PUBLIC_BASE_URL")), "/"),
		HTTPTimeout:     httpTimeout,
		PollInterval:    pollInterval,
		RetryBase:       retryBase,
		MaxAttempts:     maxAttempts,
	}
	if !enabled {
		return settings, nil
	}
	if settings.ShopID == "" {
		return YooKassaConfig{}, errors.New("YOOKASSA_SHOP_ID is required when YooKassa is enabled")
	}
	if settings.SecretKey == "" {
		return YooKassaConfig{}, errors.New("YOOKASSA_SECRET_KEY is required when YooKassa is enabled")
	}
	if err := validateAbsoluteURL(settings.ReturnURL); err != nil {
		return YooKassaConfig{}, errors.New(
			"YOOKASSA_RETURN_URL must be a valid absolute URL when YooKassa is enabled",
		)
	}
	if err := validateHTTPURL(settings.PublicBaseURL); err != nil {
		return YooKassaConfig{}, errors.New(
			"PUBLIC_BASE_URL must be a valid HTTP(S) URL when YooKassa is enabled",
		)
	}
	return settings, nil
}

func boolean(value string, fallback bool, name string) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return parsed, nil
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" {
		return errors.New("invalid HTTP URL")
	}
	return nil
}

func validateAbsoluteURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return errors.New("invalid absolute URL")
	}
	return nil
}

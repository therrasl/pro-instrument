package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
)

func TestLoad(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":    "postgres://postgres:postgres@localhost:5432/pro_instrument?sslmode=disable",
		"OTP_HASH_SECRET": "0123456789abcdef0123456789abcdef",
		"PICKUP_ADDRESS":  "Москва, тестовый адрес",
	}

	settings, err := config.Load(func(key string) string {
		return values[key]
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if settings.DatabaseURL != values["DATABASE_URL"] {
		t.Fatalf("unexpected database URL: %q", settings.DatabaseURL)
	}
	if settings.Port != "8080" {
		t.Fatalf("expected default port 8080, got %q", settings.Port)
	}
	if settings.OTPTTL != 5*time.Minute {
		t.Fatalf("expected OTP TTL 5m, got %s", settings.OTPTTL)
	}
	if settings.SessionTTL != 30*24*time.Hour {
		t.Fatalf("expected session TTL 720h, got %s", settings.SessionTTL)
	}
	if settings.AppEnv != "development" {
		t.Fatalf("expected development environment, got %q", settings.AppEnv)
	}
	if settings.StoragePath != "./storage" || settings.MaxUploadSize != 10*1024*1024 {
		t.Fatalf("unexpected storage defaults: path=%q size=%d", settings.StoragePath, settings.MaxUploadSize)
	}
	if settings.RentalHoldTTL != 30*time.Minute || settings.CourierFee != 100_000 {
		t.Fatalf(
			"unexpected rental defaults: ttl=%s courier_fee=%d",
			settings.RentalHoldTTL,
			settings.CourierFee,
		)
	}
	if settings.Bitrix.Enabled ||
		settings.Bitrix.CategoryID != 12 ||
		settings.Bitrix.Stages.Application != "C12:NEW" ||
		settings.Bitrix.Stages.Failed != "C12:LOSE" {
		t.Fatalf("unexpected Bitrix defaults: %#v", settings.Bitrix)
	}
	if settings.YooKassa.Enabled ||
		settings.YooKassa.ReceiptsEnabled ||
		settings.YooKassa.HTTPTimeout != 10*time.Second ||
		settings.YooKassa.MaxAttempts != 10 {
		t.Fatalf("unexpected YooKassa defaults: %#v", settings.YooKassa)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := config.Load(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestLoadAuthOverrides(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                 "postgres://localhost/test",
		"OTP_TTL":                      "10m",
		"SESSION_TTL":                  "24h",
		"APP_ENV":                      "test",
		"OTP_HASH_SECRET":              "0123456789abcdef0123456789abcdef",
		"STORAGE_PATH":                 "/tmp/uploads",
		"MAX_UPLOAD_SIZE":              "2048",
		"RENTAL_HOLD_TTL":              "45m",
		"COURIER_DELIVERY_FEE_KOPECKS": "125000",
		"PICKUP_ADDRESS":               "Москва, пункт выдачи",
	}

	settings, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if settings.OTPTTL != 10*time.Minute ||
		settings.SessionTTL != 24*time.Hour ||
		settings.AppEnv != "test" ||
		settings.StoragePath != "/tmp/uploads" ||
		settings.MaxUploadSize != 2048 ||
		settings.RentalHoldTTL != 45*time.Minute ||
		settings.CourierFee != 125_000 {
		t.Fatalf("unexpected auth settings: %#v", settings)
	}
}

func TestLoadDemoConfigurationNormalizesPhones(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":      "postgres://localhost/test",
		"OTP_HASH_SECRET":   "0123456789abcdef0123456789abcdef",
		"DEMO_MODE_ENABLED": "true",
		"DEMO_OTP_CODE":     "654321",
		"DEMO_OTP_PHONES":   "8 (999) 123-45-67, +4915112345678, +7 999 123 45 67",
		"PICKUP_ADDRESS":    "Москва, пункт выдачи",
	}
	settings, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load demo config: %v", err)
	}
	if !settings.Demo.Enabled || settings.Demo.OTPCode != "654321" ||
		len(settings.Demo.OTPPhones) != 2 ||
		settings.Demo.OTPPhones[0] != "+79991234567" ||
		settings.Demo.OTPPhones[1] != "+4915112345678" {
		t.Fatalf("unexpected demo config: %#v", settings.Demo)
	}
}

func TestLoadRejectsInvalidDemoConfiguration(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":      "postgres://localhost/test",
		"OTP_HASH_SECRET":   "0123456789abcdef0123456789abcdef",
		"DEMO_MODE_ENABLED": "true",
		"DEMO_OTP_CODE":     "654321",
		"DEMO_OTP_PHONES":   "+79991234567",
		"PICKUP_ADDRESS":    "Москва, пункт выдачи",
	}
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "invalid flag", key: "DEMO_MODE_ENABLED", value: "sometimes", want: "DEMO_MODE_ENABLED"},
		{name: "missing code", key: "DEMO_OTP_CODE", value: "", want: "DEMO_OTP_CODE"},
		{name: "non numeric code", key: "DEMO_OTP_CODE", value: "12AB56", want: "DEMO_OTP_CODE"},
		{name: "missing phones", key: "DEMO_OTP_PHONES", value: "", want: "DEMO_OTP_PHONES"},
		{name: "invalid phone", key: "DEMO_OTP_PHONES", value: "not-a-phone", want: "DEMO_OTP_PHONES"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			_, err := config.Load(func(key string) string { return values[key] })
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s error, got %v", test.want, err)
			}
		})
	}
}

func TestLoadDisabledDemoModeIgnoresDemoValues(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":      "postgres://localhost/test",
		"OTP_HASH_SECRET":   "0123456789abcdef0123456789abcdef",
		"DEMO_MODE_ENABLED": "false",
		"DEMO_OTP_CODE":     "not-used",
		"DEMO_OTP_PHONES":   "not-used",
		"PICKUP_ADDRESS":    "Москва, пункт выдачи",
	}
	settings, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("disabled demo mode changed existing config behavior: %v", err)
	}
	if settings.Demo.Enabled || settings.Demo.OTPCode != "" || len(settings.Demo.OTPPhones) != 0 {
		t.Fatalf("unexpected disabled demo config: %#v", settings.Demo)
	}
}

func TestLoadRejectsInvalidTTL(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":    "postgres://localhost/test",
		"OTP_TTL":         "invalid",
		"OTP_HASH_SECRET": "0123456789abcdef0123456789abcdef",
	}

	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "OTP_TTL") {
		t.Fatalf("expected OTP_TTL error, got %v", err)
	}
}

func TestLoadRequiresOTPHashSecret(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "postgres://localhost/test",
	}

	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "OTP_HASH_SECRET") {
		t.Fatalf("expected OTP_HASH_SECRET error, got %v", err)
	}
}

func TestLoadRejectsInvalidMaxUploadSize(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":    "postgres://localhost/test",
		"OTP_HASH_SECRET": "0123456789abcdef0123456789abcdef",
		"MAX_UPLOAD_SIZE": "zero",
	}

	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "MAX_UPLOAD_SIZE") {
		t.Fatalf("expected MAX_UPLOAD_SIZE error, got %v", err)
	}
}

func TestLoadRejectsInvalidRentalConfiguration(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":    "postgres://localhost/test",
		"OTP_HASH_SECRET": "0123456789abcdef0123456789abcdef",
		"RENTAL_HOLD_TTL": "0s",
	}

	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "RENTAL_HOLD_TTL") {
		t.Fatalf("expected RENTAL_HOLD_TTL error, got %v", err)
	}

	values["RENTAL_HOLD_TTL"] = "30m"
	values["COURIER_DELIVERY_FEE_KOPECKS"] = "-1"
	_, err = config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "COURIER_DELIVERY_FEE_KOPECKS") {
		t.Fatalf("expected courier fee error, got %v", err)
	}
}

func TestLoadBitrixEnabledConfiguration(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":           "postgres://localhost/test",
		"OTP_HASH_SECRET":        "0123456789abcdef0123456789abcdef",
		"BITRIX_ENABLED":         "true",
		"BITRIX_BASE_URL":        "https://example.test/rest/user/token",
		"BITRIX_WEBHOOK_SECRET":  "abcdef0123456789abcdef0123456789",
		"BITRIX_HTTP_TIMEOUT":    "5s",
		"BITRIX_MAX_ATTEMPTS":    "7",
		"BITRIX_FIELD_RENTAL_ID": "UF_CRM_RENTAL_ID",
		"PICKUP_ADDRESS":         "Москва, пункт выдачи",
	}

	settings, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load enabled Bitrix config: %v", err)
	}
	if !settings.Bitrix.Enabled ||
		settings.Bitrix.HTTPTimeout != 5*time.Second ||
		settings.Bitrix.MaxAttempts != 7 ||
		settings.Bitrix.Fields.RentalID != "UF_CRM_RENTAL_ID" {
		t.Fatalf("unexpected enabled Bitrix settings: %#v", settings.Bitrix)
	}
}

func TestLoadBitrixEnabledRequiresURLAndSecret(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":    "postgres://localhost/test",
		"OTP_HASH_SECRET": "0123456789abcdef0123456789abcdef",
		"BITRIX_ENABLED":  "true",
		"PICKUP_ADDRESS":  "Москва, пункт выдачи",
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "BITRIX_BASE_URL") {
		t.Fatalf("expected Bitrix base URL error, got %v", err)
	}

	values["BITRIX_BASE_URL"] = "https://example.test/rest/user/token"
	_, err = config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "BITRIX_WEBHOOK_SECRET") {
		t.Fatalf("expected Bitrix webhook secret error, got %v", err)
	}
}

func TestLoadYooKassaEnabledConfiguration(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":              "postgres://localhost/test",
		"OTP_HASH_SECRET":           "0123456789abcdef0123456789abcdef",
		"YOOKASSA_ENABLED":          "true",
		"YOOKASSA_SHOP_ID":          "test-shop",
		"YOOKASSA_SECRET_KEY":       "test-secret",
		"YOOKASSA_RETURN_URL":       "https://app.example.test/payment-return",
		"YOOKASSA_RECEIPTS_ENABLED": "true",
		"PUBLIC_BASE_URL":           "https://api.example.test/",
		"YOOKASSA_HTTP_TIMEOUT":     "4s",
		"YOOKASSA_MAX_ATTEMPTS":     "6",
		"PICKUP_ADDRESS":            "Москва, пункт выдачи",
	}
	settings, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load YooKassa config: %v", err)
	}
	if !settings.YooKassa.Enabled ||
		!settings.YooKassa.ReceiptsEnabled ||
		settings.YooKassa.ShopID != "test-shop" ||
		settings.YooKassa.PublicBaseURL != "https://api.example.test" ||
		settings.YooKassa.HTTPTimeout != 4*time.Second ||
		settings.YooKassa.MaxAttempts != 6 {
		t.Fatalf("unexpected YooKassa settings: %#v", settings.YooKassa)
	}
}

func TestLoadYooKassaEnabledRequiresCredentialsAndURLs(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":     "postgres://localhost/test",
		"OTP_HASH_SECRET":  "0123456789abcdef0123456789abcdef",
		"YOOKASSA_ENABLED": "true",
		"PICKUP_ADDRESS":   "Москва, пункт выдачи",
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "YOOKASSA_SHOP_ID") {
		t.Fatalf("expected YooKassa shop error, got %v", err)
	}

	values["YOOKASSA_SHOP_ID"] = "shop"
	_, err = config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "YOOKASSA_SECRET_KEY") {
		t.Fatalf("expected YooKassa secret error, got %v", err)
	}

	values["YOOKASSA_SECRET_KEY"] = "secret"
	_, err = config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "YOOKASSA_RETURN_URL") {
		t.Fatalf("expected YooKassa return URL error, got %v", err)
	}

	values["YOOKASSA_RETURN_URL"] = "https://app.example.test/return"
	_, err = config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Fatalf("expected public base URL error, got %v", err)
	}
}

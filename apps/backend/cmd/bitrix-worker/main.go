package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/bitrix"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/payments"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/push"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	bitrixSettings, err := config.LoadBitrix(os.Getenv)
	if err != nil {
		return fmt.Errorf("load Bitrix configuration: %w", err)
	}
	if !bitrixSettings.Enabled {
		logger.Print("Bitrix worker is disabled")
		return nil
	}
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	rentalHoldTTL, err := config.LoadRentalHoldTTL(os.Getenv)
	if err != nil {
		return fmt.Errorf("load rental hold configuration: %w", err)
	}

	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDatabase()
	pool, err := database.Open(databaseContext, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	repository := bitrix.NewPostgresRepository(pool)
	statusService := rentals.NewStatusService(
		rentals.NewPostgresRepository(pool),
		rentalHoldTTL,
	)
	client := bitrix.NewHTTPClient(bitrixSettings.BaseURL, bitrixSettings.HTTPTimeout)
	worker := bitrix.NewWorker(repository, client, statusService, bitrixSettings, logger)

	pushRepo := push.NewPostgresRepository(pool)
	pushClient := push.NewExpoClient(push.DefaultExpoPushURL, &http.Client{Timeout: 10 * time.Second})
	worker.SetVerificationPushNotifier(push.NewVerificationPushNotifier(pushRepo, pushClient))

	storagePath := os.Getenv("STORAGE_PATH")
	if storagePath == "" {
		storagePath = "./storage"
	}
	fileStorage, _ := verification.NewLocalFileStorage(storagePath)
	verificationService := verification.NewService(verification.NewPostgresRepository(pool), fileStorage, 10*1024*1024)
	worker.SetVerificationReviewer(verificationService)

	yooSettings, err := config.LoadYooKassa(os.Getenv)
	if err == nil && yooSettings.Enabled {
		yooClient := yookassa.NewHTTPClient(
			yookassa.DefaultBaseURL,
			yooSettings.ShopID,
			yooSettings.SecretKey,
			yooSettings.HTTPTimeout,
		)
		paymentsRepo := payments.NewPostgresRepository(pool)
		paymentsService := payments.NewService(
			paymentsRepo,
			yooClient,
			yooSettings.Enabled,
			yooSettings.ReturnURL,
			yooSettings.ReceiptsEnabled,
			yooSettings.RetryBase,
			yooSettings.MaxAttempts,
			logger,
		).SetFiscalParameters(yooSettings.TaxSystemCode, yooSettings.VATCode)
		worker.SetDepositRefunder(paymentsService)
		logger.Print("YooKassa automatic deposit refund enabled in Bitrix worker")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return worker.Run(ctx)
}

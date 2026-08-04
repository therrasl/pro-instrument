package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/app"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/appconfig"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/auth"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/catalog"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/bitrix"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/middleware"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/payments"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/push"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("backend stopped: %v", err)
		os.Exit(1)
	}
}

func run(logger *log.Logger) error {
	settings, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDatabase()

	pool, err := database.Open(databaseContext, settings.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	catalogRepository := catalog.NewRepository(pool)
	catalogService := catalog.NewService(catalogRepository)
	catalogHandler := catalog.NewHandler(catalogService, logger)

	if settings.AppEnv != "development" {
		return fmt.Errorf("APP_ENV %q: %w", settings.AppEnv, auth.ErrUnsupportedEnv)
	}
	authRepository := auth.NewPostgresRepository(pool)
	smsSender := auth.NewDevelopmentSMSSender(logger)
	authServiceOptions := make([]auth.ServiceOption, 0, 1)
	if settings.Demo.Enabled {
		authServiceOptions = append(
			authServiceOptions,
			auth.WithDemoOTP(settings.Demo.OTPCode, settings.Demo.OTPPhones),
		)
	}
	authService := auth.NewService(
		authRepository,
		smsSender,
		settings.OTPTTL,
		settings.SessionTTL,
		settings.OTPHashSecret,
		authServiceOptions...,
	)
	authHandler := auth.NewHandler(authService, logger)
	appConfigHandler := appconfig.NewHandler(settings.Demo.Enabled, settings.Demo.OTPCode)

	fileStorage, err := verification.NewLocalFileStorage(settings.StoragePath)
	if err != nil {
		return fmt.Errorf("open document storage: %w", err)
	}
	verificationRepository := verification.NewPostgresRepository(pool)
	verificationService := verification.NewService(
		verificationRepository,
		fileStorage,
		settings.MaxUploadSize,
	)
	verificationHandler := verification.NewHandler(
		verificationService,
		logger,
		settings.MaxUploadSize,
	)

	rentalsRepository := rentals.NewPostgresRepository(pool)
	rentalsService := rentals.NewService(
		rentalsRepository,
		settings.RentalHoldTTL,
		settings.CourierFee,
	)
	rentalsHandler := rentals.NewHandler(rentalsService, logger)

	bitrixRepository := bitrix.NewPostgresRepository(pool)
	bitrixEventsHandler := bitrix.NewEventsHandler(
		bitrixRepository,
		settings.Bitrix.Enabled,
		settings.Bitrix.WebhookSecret,
		logger,
	)

	paymentsRepository := payments.NewPostgresRepository(pool)
	yooKassaClient := yookassa.NewHTTPClient(
		yookassa.DefaultBaseURL,
		settings.YooKassa.ShopID,
		settings.YooKassa.SecretKey,
		settings.YooKassa.HTTPTimeout,
	)
	paymentsService := payments.NewService(
		paymentsRepository,
		yooKassaClient,
		settings.YooKassa.Enabled,
		settings.YooKassa.ReturnURL,
		settings.YooKassa.ReceiptsEnabled,
		settings.YooKassa.RetryBase,
		settings.YooKassa.MaxAttempts,
		logger,
	)
	paymentsHandler := payments.NewHandler(paymentsService, logger)
	yooKassaWebhookHandler := payments.NewWebhookHandler(paymentsService, logger)
	pushRepository := push.NewPostgresRepository(pool)
	pushTokenService := push.NewTokenService(pushRepository)
	pushHandler := push.NewHandler(pushTokenService, logger)
	pushClient := push.NewExpoClient(push.DefaultExpoPushURL, &http.Client{
		Timeout: 10 * time.Second,
	})
	pushDispatcher := push.NewDispatcher(
		pushRepository,
		pushClient,
		2*time.Second,
		5*time.Second,
		5,
		logger,
	)
	if settings.YooKassa.Enabled {
		logger.Printf(
			"YooKassa webhook endpoint: %s/api/v1/integrations/yookassa/webhook",
			settings.YooKassa.PublicBaseURL,
		)
	}

	handler := middleware.RequestLogger(logger)(middleware.Recovery(logger)(app.NewHandler(
		catalogHandler,
		authHandler,
		verificationHandler,
		rentalsHandler,
		bitrixEventsHandler,
		paymentsHandler,
		yooKassaWebhookHandler,
		pushHandler,
		appConfigHandler,
	)))
	server := &http.Server{
		Addr:              ":" + settings.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go pushDispatcher.Run(shutdownSignal)

	serverError := make(chan error, 1)
	go func() {
		logger.Printf("backend listening on %s", server.Addr)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-shutdownSignal.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Printf("graceful shutdown failed: %v", err)
			if closeErr := server.Close(); closeErr != nil {
				return fmt.Errorf("force HTTP shutdown: %w", closeErr)
			}
		}
	}

	return nil
}

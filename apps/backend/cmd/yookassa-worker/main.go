package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/yookassa"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/payments"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("YooKassa worker stopped: %v", err)
		os.Exit(1)
	}
}

func run(logger *log.Logger) error {
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return err
	}
	settings, err := config.LoadYooKassa(os.Getenv)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		logger.Print("YooKassa worker is disabled")
		return nil
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	databaseContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := database.Open(databaseContext, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	repository := payments.NewPostgresRepository(pool)
	client := yookassa.NewHTTPClient(
		yookassa.DefaultBaseURL,
		settings.ShopID,
		settings.SecretKey,
		settings.HTTPTimeout,
	)
	service := payments.NewService(
		repository,
		client,
		settings.Enabled,
		settings.ReturnURL,
		settings.ReceiptsEnabled,
		settings.RetryBase,
		settings.MaxAttempts,
		logger,
	).SetFiscalParameters(settings.TaxSystemCode, settings.VATCode)

	for {
		processed, err := service.RunOnce(ctx)
		if err != nil {
			logger.Printf("YooKassa worker iteration failed: %v", err)
		}
		if processed {
			continue
		}
		timer := time.NewTimer(settings.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

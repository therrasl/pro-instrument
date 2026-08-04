package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/integrations/bitrix"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	bitrixSettings, err := config.LoadBitrix(os.Getenv)
	if err != nil {
		return fmt.Errorf("load Bitrix configuration: %w", err)
	}
	if !bitrixSettings.Enabled {
		log.Print("Bitrix reconciliation is disabled")
		return nil
	}
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	count, err := bitrix.NewReconciliationService(
		bitrix.NewPostgresRepository(pool),
	).Run(ctx)
	if err != nil {
		return err
	}
	log.Printf("Bitrix reconciliation events queued: %d", count)
	return nil
}

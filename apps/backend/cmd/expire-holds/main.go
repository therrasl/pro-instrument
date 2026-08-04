package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	count, err := rentals.NewPostgresRepository(pool).ExpireHolds(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	log.Printf("expired tool holds: %d", count)
	return nil
}

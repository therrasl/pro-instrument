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
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/orderdocs"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}
func run(logger *log.Logger) error {
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	storage := os.Getenv("STORAGE_PATH")
	if storage == "" {
		storage = "./storage"
	}
	service := orderdocs.NewService(orderdocs.NewPostgresRepository(pool), orderdocs.NewGenerator(storage), logger)
	runContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return service.Run(runContext, 5*time.Second)
}

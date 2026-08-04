package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return errors.New("usage: go run ./cmd/migrate <up|down>")
	}

	databaseURLValue, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	databaseURL, err := migrationURL(databaseURLValue)
	if err != nil {
		return err
	}

	migration, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		_, _ = migration.Close()
	}()

	switch os.Args[1] {
	case "up":
		err = migration.Up()
	case "down":
		err = migration.Steps(-1)
	default:
		return fmt.Errorf("unknown migration direction %q", os.Args[1])
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %s: %w", os.Args[1], err)
	}

	return nil
}

func migrationURL(databaseURL string) (string, error) {
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	switch parsedURL.Scheme {
	case "postgres", "postgresql":
		parsedURL.Scheme = "pgx5"
	case "pgx5":
	default:
		return "", fmt.Errorf("unsupported DATABASE_URL scheme %q", parsedURL.Scheme)
	}

	return parsedURL.String(), nil
}

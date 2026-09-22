package main

import (
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	m, err := migrate.New(
		"file://migrations",
		databaseURL,
	)
	if err != nil {
		log.Fatalf("failed to initialize migration: %v", err)
	}

	defer m.Close()

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			fmt.Println("Database is already up to date")
			return
		}

		log.Fatalf("migration failed: %v", err)
	}

	fmt.Println("Database migration completed successfully")
}

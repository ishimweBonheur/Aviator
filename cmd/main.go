package main

import (
	"aviator/backend/internal/config"
	"aviator/backend/internal/database"
	"log"
	"net/http"
)

func main() {
	// Load application configuration.
	cfg := config.Load()

	// Connect to PostgreSQL.
	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Connected to PostgreSQL")

	// HTTP routes.
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusOK)

		w.Write([]byte(`{"status":"ok"}`))
	})

	// Start HTTP server.
	addr := ":" + cfg.AppPort

	log.Printf("Aviator backend running on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

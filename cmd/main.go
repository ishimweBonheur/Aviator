package main

import (
	_ "aviator/backend/docs"
	"aviator/backend/internal/auth"
	"aviator/backend/internal/config"
	"aviator/backend/internal/database"
	"aviator/backend/internal/game"
	"aviator/backend/internal/wallet"
	"context"
	"log"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

// @title Aviator Backend API
// @description HTTP API for the Aviator real-money game backend.
// @version 1.0
// @host localhost:7000
// @BasePath /
// @schemes http
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter your JWT token. Swagger UI will send it as "Bearer <token>".

// @Summary Health check
// @Description Returns the current health status of the backend.
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)

	w.Write([]byte(`{"status":"ok"}`))
}

func main() {
	// Configuration.
	cfg := config.Load()

	// PostgreSQL.
	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	defer db.Close()

	log.Println("Connected to PostgreSQL")

	// Authentication.
	authService := auth.NewService(
		db,
		cfg.JWTSecret,
	)

	authHandler := auth.NewHandler(authService)

	// Wallet.
	walletRepository := wallet.NewRepository(db)
	walletHandler := wallet.NewHandler(walletRepository)

	// Game.
	gameRepository := game.NewRepository(db)
	gameService := game.NewService(gameRepository)
	gameHandler := game.NewHandler(gameService)

	// Game engine.
	gameEngine := game.NewEngine(gameService)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gameEngine.Run(ctx)

	// Router.
	mux := http.NewServeMux()

	// Health.
	mux.HandleFunc("/health", healthHandler)

	// Public authentication routes.
	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)

	// Protected wallet routes.
	mux.Handle(
		"/api/wallet/balance",
		authService.Middleware(
			http.HandlerFunc(walletHandler.GetBalance),
		),
	)

	// Game routes.
	mux.HandleFunc(
		"/api/game/rounds",
		gameHandler.CreateRound,
	)

	mux.HandleFunc(
		"/api/game/rounds/current",
		gameHandler.GetCurrentRound,
	)

	mux.HandleFunc(
		"/api/game/rounds/",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				gameHandler.GetRound(w, r)

			case http.MethodPost:
				switch {
				case hasSuffix(r.URL.Path, "/open"):
					gameHandler.OpenBetting(w, r)

				case hasSuffix(r.URL.Path, "/close"):
					gameHandler.CloseBetting(w, r)

				case hasSuffix(r.URL.Path, "/start"):
					gameHandler.StartRound(w, r)

				case hasSuffix(r.URL.Path, "/crash"):
					gameHandler.CrashRound(w, r)

				case hasSuffix(r.URL.Path, "/settle"):
					gameHandler.SettleRound(w, r)

				default:
					http.NotFound(w, r)
				}

			default:
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
			}
		},
	)

	// Swagger UI.
	mux.Handle(
		"/swagger/",
		httpSwagger.WrapHandler,
	)

	// Start server.
	addr := ":" + cfg.AppPort

	log.Printf(
		"Aviator backend running on %s",
		addr,
	)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func hasSuffix(path, suffix string) bool {
	if len(path) < len(suffix) {
		return false
	}

	return path[len(path)-len(suffix):] == suffix
}

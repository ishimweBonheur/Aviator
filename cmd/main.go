package main

import (
	_ "aviator/backend/docs"
	"aviator/backend/internal/auth"
	"aviator/backend/internal/betting"
	"aviator/backend/internal/cashout"
	"aviator/backend/internal/config"
	"aviator/backend/internal/database"
	"aviator/backend/internal/deposit"
	"aviator/backend/internal/fairness"
	"aviator/backend/internal/game"
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/settlement"
	"aviator/backend/internal/wallet"
	withdrawal "aviator/backend/internal/withdraw"
	"context"
	"log"
	"net/http"

	"github.com/shopspring/decimal"
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
// @description Enter "Bearer" followed by a space and your JWT token.

// @Summary Health check
// @Description Returns the current health status of the backend.
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func healthHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(
		[]byte(`{"status":"ok"}`),
	)
}

func main() {
	// ============================================================
	// Configuration
	// ============================================================

	cfg := config.Load()

	// ============================================================
	// PostgreSQL
	// ============================================================

	db, err := database.NewPostgres(
		cfg.DatabaseURL,
	)
	if err != nil {
		log.Fatalf(
			"failed to connect to database: %v",
			err,
		)
	}

	defer db.Close()

	log.Println("Connected to PostgreSQL")

	// ============================================================
	// Fairness
	// ============================================================

	houseEdge, err := decimal.NewFromString(
		cfg.HouseEdge,
	)
	if err != nil {
		log.Fatalf(
			"failed to parse HOUSE_EDGE: %v",
			err,
		)
	}

	fairnessService := fairness.NewService(
		fairness.Config{
			HouseEdge: houseEdge,
		},
	)

	log.Printf(
		"Fairness service initialized with house edge: %s",
		houseEdge.String(),
	)

	// ============================================================
	// Authentication
	// ============================================================

	authService := auth.NewService(
		db,
		cfg.JWTSecret,
	)

	authHandler := auth.NewHandler(
		authService,
	)

	// ============================================================
	// Wallet
	// ============================================================

	walletRepository := wallet.NewRepository(
		db,
	)

	walletService := wallet.NewService(
		db,
	)

	walletHandler := wallet.NewHandler(
		walletRepository,
	)

	// ============================================================
	// Deposits
	// ============================================================

	depositRepository := deposit.NewRepository(
		db,
	)

	depositService := deposit.NewService(
		depositRepository,
	)

	depositHandler := deposit.NewHandler(
		depositService,
	)

	// ============================================================
	// Withdrawals
	// ============================================================

	withdrawalRepository := withdrawal.NewRepository(
		db,
	)

	withdrawalService := withdrawal.NewService(
		withdrawalRepository,
	)

	withdrawalHandler := withdrawal.NewHandler(
		withdrawalService,
	)

	// ============================================================
	// Betting
	// ============================================================

	bettingRepository := betting.NewRepository(
		db,
	)

	bettingService := betting.NewService(
		db,
		bettingRepository,
		walletService,
	)

	bettingHandler := betting.NewHandler(
		bettingService,
	)

	// ============================================================
	// Multiplier
	// ============================================================

	multiplierService := multiplier.NewService()

	// ============================================================
	// Cashout
	// ============================================================

	cashoutRepository := cashout.NewRepository(
		db,
	)

	cashoutService := cashout.NewService(
		db,
		cashoutRepository,
		walletService,
		multiplierService,
	)

	cashoutHandler := cashout.NewHandler(
		cashoutService,
	)

	// ============================================================
	// Game
	// ============================================================

	gameRepository := game.NewRepository(
		db,
	)

	gameService := game.NewService(
		gameRepository,
		fairnessService,
	)

	gameHandler := game.NewHandler(
		gameService,
	)

	// ============================================================
	// Settlement
	// ============================================================

	settlementRepository := settlement.NewRepository(
		db,
	)

	settlementService := settlement.NewService(
		db,
		settlementRepository,
	)

	settlementHandler := settlement.NewHandler(
		settlementService,
	)

	// ============================================================
	// Realtime / WebSocket
	// ============================================================

	realtimeHub := realtime.NewHub()

	defer realtimeHub.Close()

	realtimeHandler := realtime.NewHandler(
		realtimeHub,
	)

	// ============================================================
	// Game Engine
	// ============================================================

	gameEngine := game.NewEngine(
		gameService,
		multiplierService,
		settlementService,
		realtimeHub,
	)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	defer cancel()

	go gameEngine.Run(ctx)

	// ============================================================
	// Router
	// ============================================================

	mux := http.NewServeMux()

	// ============================================================
	// WebSocket
	// ============================================================

	mux.Handle(
		"GET /ws",
		realtimeHandler,
	)

	// ============================================================
	// Health
	// ============================================================

	mux.HandleFunc(
		"/health",
		healthHandler,
	)

	// ============================================================
	// Authentication
	// ============================================================

	mux.HandleFunc(
		"/api/auth/register",
		authHandler.Register,
	)

	mux.HandleFunc(
		"/api/auth/login",
		authHandler.Login,
	)

	// ============================================================
	// Protected Wallet Routes
	// ============================================================

	mux.Handle(
		"/api/wallet/balance",
		authService.Middleware(
			http.HandlerFunc(
				walletHandler.GetBalance,
			),
		),
	)

	// ============================================================
	// Protected Deposit Routes
	//
	// POST /api/deposits
	// GET  /api/deposits
	// ============================================================

	mux.Handle(
		"/api/deposits",
		authService.Middleware(
			http.HandlerFunc(
				depositHandler.Handle,
			),
		),
	)

	// ============================================================
	// Protected Withdrawal Routes
	//
	// POST /api/withdrawals
	// GET  /api/withdrawals
	// ============================================================

	mux.Handle(
		"/api/withdrawals",
		authService.Middleware(
			http.HandlerFunc(
				withdrawalHandler.Handle,
			),
		),
	)

	// ============================================================
	// Protected Betting Routes
	// ============================================================

	mux.Handle(
		"/api/bets",
		authService.Middleware(
			http.HandlerFunc(
				bettingHandler.PlaceBet,
			),
		),
	)

	// ============================================================
	// Protected Cashout Route
	//
	// Example:
	// POST /api/bets/123
	// ============================================================

	mux.Handle(
		"/api/bets/",
		authService.Middleware(
			http.HandlerFunc(
				cashoutHandler.CashOut,
			),
		),
	)

	// ============================================================
	// Settlement Routes
	// ============================================================

	mux.HandleFunc(
		"/api/settlement/rounds/",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			switch r.Method {

			case http.MethodPost:
				switch {

				case hasSuffix(
					r.URL.Path,
					"/settle",
				):
					settlementHandler.SettleRound(
						w,
						r,
					)

				default:
					http.NotFound(
						w,
						r,
					)
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

	// ============================================================
	// Game Routes
	// ============================================================

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
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			switch r.Method {

			case http.MethodGet:
				gameHandler.GetRound(
					w,
					r,
				)

			case http.MethodPost:
				switch {

				case hasSuffix(
					r.URL.Path,
					"/open",
				):
					gameHandler.OpenBetting(
						w,
						r,
					)

				case hasSuffix(
					r.URL.Path,
					"/close",
				):
					gameHandler.CloseBetting(
						w,
						r,
					)

				case hasSuffix(
					r.URL.Path,
					"/start",
				):
					gameHandler.StartRound(
						w,
						r,
					)

				case hasSuffix(
					r.URL.Path,
					"/crash",
				):
					gameHandler.CrashRound(
						w,
						r,
					)

				case hasSuffix(
					r.URL.Path,
					"/settle",
				):
					gameHandler.SettleRound(
						w,
						r,
					)

				default:
					http.NotFound(
						w,
						r,
					)
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

	// ============================================================
	// Swagger
	// ============================================================

	mux.Handle(
		"/swagger/",
		httpSwagger.WrapHandler,
	)

	// ============================================================
	// Start Server
	// ============================================================

	addr := ":" + cfg.AppPort

	log.Printf(
		"Aviator backend running on %s",
		addr,
	)

	if err := http.ListenAndServe(
		addr,
		mux,
	); err != nil {
		log.Fatal(err)
	}
}

func hasSuffix(
	path string,
	suffix string,
) bool {
	if len(path) < len(suffix) {
		return false
	}

	return path[len(path)-len(suffix):] == suffix
}

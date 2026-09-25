package main

import (
	_ "aviator/backend/docs"
	"aviator/backend/internal/auth"
	"aviator/backend/internal/betting"
	"aviator/backend/internal/cache"
	"aviator/backend/internal/cashout"
	"aviator/backend/internal/config"
	"aviator/backend/internal/database"
	"aviator/backend/internal/deposit"
	"aviator/backend/internal/fairness"
	"aviator/backend/internal/game"
	"aviator/backend/internal/history"
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/settlement"
	"aviator/backend/internal/wallet"
	withdrawal "aviator/backend/internal/withdraw"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	store := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	defer store.Close()

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

	depositService.SetLimits(cfg.Limits)
	withdrawalService.SetLimits(cfg.Limits)
	bettingService.SetLimits(cfg.Limits)
	cashoutService.SetLimits(cfg.Limits)
	bettingService.SetPublisher(store)
	cashoutService.SetPublisher(store)
	gameEngine := game.NewEngine(
		gameService,
		multiplierService,
		settlementService,
		store, store, cfg.BettingWindow, cfg.GrowthRate,
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)

	defer cancel()

	go store.Subscribe(ctx, realtimeHub)
	engineDone := make(chan struct{})
	go func() { defer close(engineDone); store.Lead(ctx, gameEngine.Run) }()
	defer func() { cancel(); <-engineDone }()

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
		"POST /api/auth/register",
		authHandler.Register,
	)

	mux.HandleFunc(
		"POST /api/auth/login",
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

	mux.Handle(
		"/api/deposits/{$}",
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

	mux.Handle(
		"/api/withdrawals/{$}",
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
		"POST /api/bets",
		authService.Middleware(
			http.HandlerFunc(
				bettingHandler.PlaceBet,
			),
		),
	)

	// Public reads and authenticated player commands. Lifecycle mutations are
	// deliberately unregistered; only the fenced engine invokes their services.
	mux.Handle("POST /api/bets/{id}/cashout", authService.Middleware(store.SuppressDuplicates(http.HandlerFunc(cashoutHandler.CashOut))))
	mux.Handle("POST /api/bets/{id}/cancel", authService.Middleware(store.SuppressDuplicates(http.HandlerFunc(bettingHandler.Cancel))))
	mux.HandleFunc("GET /api/game/rounds/current", gameHandler.GetCurrentRound)
	mux.HandleFunc("GET /api/game/rounds/{id}", gameHandler.GetRound)
	mux.HandleFunc("GET /api/game/rounds/{id}/fairness", gameHandler.Fairness)
	histories := history.NewHandler(db)
	mux.Handle("GET /api/bets", authService.Middleware(http.HandlerFunc(histories.Bets)))
	mux.Handle("GET /api/wallet/transactions", authService.Middleware(http.HandlerFunc(histories.Transactions)))
	mux.HandleFunc("GET /api/game/rounds", gameHandler.Recent)
	mux.Handle("GET /api/limits", cfg.Limits)
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

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server: %v", err)
	}
}

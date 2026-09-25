package config

import (
	"aviator/backend/internal/risk"
	"fmt"
	"github.com/shopspring/decimal"
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	RedisAddr, RedisPassword string
	RedisDB                  int
	BettingWindow            time.Duration
	GrowthRate               float64
	Limits                   risk.Limits
	AppPort                  string
	DatabaseURL              string
	JWTSecret                string
	HouseEdge                string
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	HouseEdge := os.Getenv("HOUSE_EDGE")
	cfg := Config{
		AppPort:     os.Getenv("APP_PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		HouseEdge:   HouseEdge,
	}
	if cfg.HouseEdge == "" {
		cfg.HouseEdge = "0"
	}
	if cfg.AppPort == "" {
		cfg.AppPort = "7000"
	}

	if cfg.DatabaseURL == "" {
		panic(fmt.Sprintf("DATABASE_URL is required"))
	}

	if cfg.JWTSecret == "" {
		panic(fmt.Sprintf("JWT_SECRET is required"))
	}

	cfg.RedisAddr = env("REDIS_ADDR", "localhost:6380")
	cfg.RedisPassword = os.Getenv("REDIS_PASSWORD")
	var err error
	cfg.RedisDB, err = strconv.Atoi(env("REDIS_DB", "0"))
	if err != nil || cfg.RedisDB < 0 {
		panic("invalid REDIS_DB")
	}
	seconds, err := strconv.Atoi(env("BETTING_WINDOW_SECONDS", "5"))
	if err != nil || seconds < 1 || seconds > 3600 {
		panic("invalid BETTING_WINDOW_SECONDS")
	}
	cfg.BettingWindow = time.Duration(seconds) * time.Second
	cfg.GrowthRate, err = strconv.ParseFloat(env("MULTIPLIER_GROWTH_RATE", "0.08"), 64)
	if err != nil || cfg.GrowthRate <= 0 || cfg.GrowthRate >= 100 || math.IsNaN(cfg.GrowthRate) {
		panic("invalid MULTIPLIER_GROWTH_RATE")
	}
	cfg.Limits = risk.Default()
	for name, ptr := range map[string]*decimal.Decimal{
		"MIN_BET_AMOUNT": &cfg.Limits.MinBet, "MAX_BET_AMOUNT": &cfg.Limits.MaxBet, "MAX_PAYOUT": &cfg.Limits.MaxPayout,
		"MIN_DEPOSIT_AMOUNT": &cfg.Limits.MinDeposit, "MAX_DEPOSIT_AMOUNT": &cfg.Limits.MaxDeposit,
		"MIN_WITHDRAWAL_AMOUNT": &cfg.Limits.MinWithdrawal, "MAX_WITHDRAWAL_AMOUNT": &cfg.Limits.MaxWithdrawal} {
		value, err := decimal.NewFromString(env(name, ptr.String()))
		if err != nil || !value.IsPositive() || !value.Equal(value.Round(2)) {
			panic("invalid " + name)
		}
		*ptr = value
	}
	if cfg.Limits.MinBet.LessThan(decimal.NewFromInt(50)) || cfg.Limits.MinBet.GreaterThan(cfg.Limits.MaxBet) || cfg.Limits.MinDeposit.GreaterThan(cfg.Limits.MaxDeposit) || cfg.Limits.MinWithdrawal.GreaterThan(cfg.Limits.MaxWithdrawal) || cfg.Limits.MaxPayout.LessThan(cfg.Limits.MaxBet) {
		panic("inconsistent risk limits (database minimum bet is 50)")
	}
	edge, err := decimal.NewFromString(cfg.HouseEdge)
	if err != nil || edge.IsNegative() || edge.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		panic("invalid HOUSE_EDGE")
	}
	return cfg
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

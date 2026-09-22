package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort     string
	DatabaseURL string
	JWTSecret   string
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := Config{
		AppPort:     os.Getenv("APP_PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
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

	return cfg
}

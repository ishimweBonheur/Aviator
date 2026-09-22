package game

import (
	"time"

	"github.com/shopspring/decimal"
)

type RoundStatus string

const (
	RoundCreated       RoundStatus = "CREATED"
	RoundBettingOpen   RoundStatus = "BETTING_OPEN"
	RoundBettingClosed RoundStatus = "BETTING_CLOSED"
	RoundRunning       RoundStatus = "RUNNING"
	RoundCrashed       RoundStatus = "CRASHED"
	RoundSettled       RoundStatus = "SETTLED"
)

type GameRound struct {
	ID             int64            `json:"id"`
	RoundNumber    int64            `json:"round_number"`
	ServerSeedHash string           `json:"server_seed_hash"`
	ServerSeed     *string          `json:"server_seed,omitempty"`
	ClientSeed     string           `json:"client_seed"`
	Nonce          int64            `json:"nonce"`
	CrashPoint     *decimal.Decimal `json:"crash_point,omitempty"`
	Status         RoundStatus      `json:"status"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	EndedAt        *time.Time       `json:"ended_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
}

package game

import (
	"encoding/json"
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
	HouseEdge       *decimal.Decimal `json:"house_edge,omitempty"`
	BettingOpenedAt *time.Time       `json:"betting_opened_at,omitempty"`
	BettingClosesAt *time.Time       `json:"betting_closes_at,omitempty"`
	GrowthRate      float64          `json:"-"`
	ID              int64            `json:"id"`
	RoundNumber     int64            `json:"round_number"`
	ServerSeedHash  string           `json:"server_seed_hash"`
	ServerSeed      *string          `json:"server_seed,omitempty"`
	ClientSeed      string           `json:"client_seed"`
	Nonce           int64            `json:"nonce"`
	CrashPoint      *decimal.Decimal `json:"crash_point,omitempty"`
	Status          RoundStatus      `json:"status"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	EndedAt         *time.Time       `json:"ended_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
}

// Redact at the serialization boundary, including future callers of this model.
func (r GameRound) MarshalJSON() ([]byte, error) {
	type public GameRound
	safe := public(r)
	if r.Status != RoundSettled {
		safe.ServerSeed = nil
	}
	if r.Status != RoundSettled && r.Status != RoundCrashed {
		safe.CrashPoint = nil
	}
	return json.Marshal(safe)
}

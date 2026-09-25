package realtime

import (
	"context"
	"time"
)

type Publisher interface{ Publish(context.Context, Event) }

const (
	EventCountdown        = "COUNTDOWN"
	EventBetPlaced        = "BET_PLACED"
	EventBetCancelled     = "BET_CANCELLED"
	EventBetCashedOut     = "BET_CASHED_OUT"
	EventRoundOpened      = "ROUND_OPENED"
	EventRoundStarted     = "ROUND_STARTED"
	EventMultiplierUpdate = "MULTIPLIER_UPDATE"
	EventRoundCrashed     = "ROUND_CRASHED"
	EventRoundSettled     = "ROUND_SETTLED"
)

// Event contains public game state only. Decimal values are formatted by the
// publisher with exactly two decimal places; crash points are revealed at crash.
type Event struct {
	Timestamp        time.Time  `json:"timestamp"`
	SecondsRemaining *int       `json:"seconds_remaining,omitempty"`
	BettingClosesAt  *time.Time `json:"betting_closes_at,omitempty"`
	BetID            int64      `json:"bet_id,omitempty"`
	Amount           string     `json:"amount,omitempty"`
	Payout           string     `json:"payout,omitempty"`
	Type             string     `json:"type"`
	RoundID          int64      `json:"round_id"`
	RoundNumber      int64      `json:"round_number"`
	Multiplier       string     `json:"multiplier,omitempty"`
	CrashPoint       string     `json:"crash_point,omitempty"`
}

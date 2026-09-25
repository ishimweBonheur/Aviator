package game

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/settlement"
	"context"
	"time"
)

type LiveState interface {
	State(context.Context, string, any)
}
type Engine struct {
	service           *Service
	multiplierService *multiplier.Service
	settlementService *settlement.Service
	publisher         realtime.Publisher
	liveState         LiveState
	bettingWindow     time.Duration
	growthRate        float64
	countdownRound    int64
	countdownSeconds  int
}

func NewEngine(service *Service, m *multiplier.Service, s *settlement.Service, p realtime.Publisher, state LiveState, window time.Duration, rate float64) *Engine {
	return &Engine{service: service, multiplierService: m, settlementService: s, publisher: p, liveState: state, bettingWindow: window, growthRate: rate}
}
func (e *Engine) Run(ctx context.Context) { e.RunLeader(ctx) }

package multiplier

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type Service struct {
	mu     sync.RWMutex
	states map[int64]State
}

func NewService() *Service {
	return &Service{
		states: make(map[int64]State),
	}
}

func (s *Service) Start(roundID int64, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[roundID] = State{
		RoundID:    roundID,
		Multiplier: decimal.NewFromFloat(1.0),
		Running:    true,
		StartedAt:  startedAt,
	}
}

func (s *Service) Current(roundID int64) (decimal.Decimal, error) {
	s.mu.RLock()
	state, exists := s.states[roundID]
	s.mu.RUnlock()

	if !exists {
		return decimal.Zero, fmt.Errorf("multiplier state not found for round %d", roundID)
	}

	if !state.Running {
		return decimal.Zero, fmt.Errorf("round %d is not running", roundID)
	}

	return Calculate(state.StartedAt, time.Now()), nil
}

func (s *Service) Set(roundID int64, value decimal.Decimal) error {
	if value.LessThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("multiplier cannot be below 1.00")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[roundID]
	if !exists {
		return fmt.Errorf("multiplier state not found for round %d", roundID)
	}

	state.Multiplier = value
	s.states[roundID] = state

	return nil
}

func (s *Service) Stop(roundID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[roundID]
	if !exists {
		return
	}

	state.Running = false
	s.states[roundID] = state
}

func (s *Service) Remove(roundID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, roundID)
}

// Calculate returns the multiplier for a specific point in time.
//
// This is intentionally deterministic:
// the same start time and current time produce the same multiplier.
//
// The growth curve is:
//
//	multiplier = e^(growthRate * elapsedSeconds)
//
// The exact growth rate is a gameplay parameter and can be tuned later.
func Calculate(startedAt, now time.Time) decimal.Decimal {
	elapsed := now.Sub(startedAt).Seconds()

	if elapsed <= 0 {
		return decimal.NewFromFloat(1.0)
	}

	const growthRate = 0.08

	multiplier := math.Exp(growthRate * elapsed)

	return decimal.NewFromFloat(multiplier).Round(2)
}

package multiplier

import (
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const (
	growthRate = 0.08

	// Defensive ceiling for multiplier calculations.
	//
	// The game engine should normally stop at the round's crash point
	// long before reaching this value. This exists to prevent stale
	// RUNNING rounds or corrupted timestamps from causing math.Exp
	// to overflow to +Inf.
	maxMultiplier = 1_000_000_000
)

var (
	oneMultiplier        = decimal.NewFromInt(1)
	maxMultiplierDecimal = decimal.NewFromInt(maxMultiplier)
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
		Multiplier: oneMultiplier,
		Running:    true,
		StartedAt:  startedAt,
	}
}

func (s *Service) Current(roundID int64) (decimal.Decimal, error) {
	s.mu.RLock()
	state, exists := s.states[roundID]
	s.mu.RUnlock()

	if !exists {
		return decimal.Zero, fmt.Errorf(
			"multiplier state not found for round %d",
			roundID,
		)
	}

	if !state.Running {
		return decimal.Zero, fmt.Errorf(
			"round %d is not running",
			roundID,
		)
	}

	return Calculate(state.StartedAt, time.Now()), nil
}

func (s *Service) Set(
	roundID int64,
	value decimal.Decimal,
) error {
	if value.LessThan(oneMultiplier) {
		return fmt.Errorf(
			"multiplier cannot be below 1.00",
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[roundID]
	if !exists {
		return fmt.Errorf(
			"multiplier state not found for round %d",
			roundID,
		)
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
// The calculation is deterministic:
//
//	multiplier = e^(growthRate * elapsedSeconds)
//
// The result is rounded to two decimal places for the game multiplier.
//
// The function also protects against invalid timestamps and floating-point
// overflow so a stale RUNNING round cannot crash the entire application.
func Calculate(startedAt, now time.Time) decimal.Decimal {
	return (Clock{Rate: growthRate}).Calculate(startedAt, now)
}

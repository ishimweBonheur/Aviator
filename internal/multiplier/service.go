package multiplier

import (
	"fmt"
	"sync"

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

func (s *Service) Start(roundID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[roundID] = State{
		RoundID:    roundID,
		Multiplier: decimal.NewFromInt(1),
		Running:    true,
	}
}

func (s *Service) Set(roundID int64, value decimal.Decimal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[roundID]
	if !exists {
		return fmt.Errorf("multiplier state not found for round %d", roundID)
	}

	if !value.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return fmt.Errorf("multiplier must be at least 1.00")
	}

	state.Multiplier = value
	s.states[roundID] = state

	return nil
}

func (s *Service) Current(roundID int64) (decimal.Decimal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[roundID]
	if !exists || !state.Running {
		return decimal.Zero, false
	}

	return state.Multiplier, true
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

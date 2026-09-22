package game

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type Engine struct {
	service *Service

	mu      sync.Mutex
	running bool
}

func NewEngine(service *Service) *Engine {
	return &Engine{
		service: service,
	}
}

// Run starts the game engine.
// The engine continuously creates and runs game rounds.
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()

	if e.running {
		e.mu.Unlock()
		log.Println("game engine is already running")
		return
	}

	e.running = true

	e.mu.Unlock()

	log.Println("game engine started")

	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()

		log.Println("game engine stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := e.runRound(ctx); err != nil {
				log.Printf("game round error: %v", err)

				// Prevent a tight retry loop if something goes wrong.
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
}

func (e *Engine) runRound(ctx context.Context) error {
	// 1. Create round
	round, err := e.service.CreateRound(ctx)
	if err != nil {
		return fmt.Errorf("create round: %w", err)
	}

	log.Printf(
		"created round #%d",
		round.RoundNumber,
	)

	// 2. Open betting
	round, err = e.service.OpenBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf("open betting: %w", err)
	}

	log.Printf(
		"round #%d: betting opened",
		round.RoundNumber,
	)

	// Temporary betting period.
	//
	// We will later move this duration into configuration.
	if err := waitFor(ctx, 5*time.Second); err != nil {
		return err
	}

	// 3. Close betting
	round, err = e.service.CloseBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf("close betting: %w", err)
	}

	log.Printf(
		"round #%d: betting closed",
		round.RoundNumber,
	)

	// 4. Start round
	round, err = e.service.StartRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf("start round: %w", err)
	}

	log.Printf(
		"round #%d: running",
		round.RoundNumber,
	)

	// The real multiplier loop will be implemented later.
	//
	// For now we simulate a running period.
	if err := waitFor(ctx, 5*time.Second); err != nil {
		return err
	}

	// 5. Crash round
	round, err = e.service.CrashRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf("crash round: %w", err)
	}

	log.Printf(
		"round #%d: crashed",
		round.RoundNumber,
	)

	// 6. Settle round
	round, err = e.service.SettleRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf("settle round: %w", err)
	}

	log.Printf(
		"round #%d: settled",
		round.RoundNumber,
	)

	return nil
}

func waitFor(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-timer.C:
		return nil
	}
}

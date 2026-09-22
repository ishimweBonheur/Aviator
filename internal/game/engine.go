package game

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
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
//
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

// runRound executes the complete lifecycle of one game round.
func (e *Engine) runRound(ctx context.Context) error {

	// --------------------------------------------------
	// 1. Create round
	// --------------------------------------------------

	round, err := e.service.CreateRound(ctx)
	if err != nil {
		return fmt.Errorf("create round: %w", err)
	}

	log.Printf(
		"created round #%d",
		round.RoundNumber,
	)

	// Open betting

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

	// Betting period

	// Temporary betting period.
	//
	// Later this will move into configuration.
	if err := waitFor(ctx, 5*time.Second); err != nil {
		return err
	}

	// Close betting

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

	// Generate crash point

	round, err = e.service.GenerateCrashPoint(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"generate crash point: %w",
			err,
		)
	}

	if round.CrashPoint == nil {
		return fmt.Errorf(
			"round #%d has no crash point",
			round.RoundNumber,
		)
	}

	log.Printf(
		"round #%d: crash point = %sx",
		round.RoundNumber,
		round.CrashPoint.StringFixed(4),
	)

	// Start round

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

	//  Run multiplier

	if err := e.runMultiplier(
		ctx,
		round,
	); err != nil {
		return fmt.Errorf(
			"multiplier loop: %w",
			err,
		)
	}

	// Crash round

	round, err = e.service.CrashRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"crash round: %w",
			err,
		)
	}

	log.Printf(
		"round #%d: crashed at %sx",
		round.RoundNumber,
		round.CrashPoint.StringFixed(4),
	)

	// Settle round

	round, err = e.service.SettleRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"settle round: %w",
			err,
		)
	}

	log.Printf(
		"round #%d: settled",
		round.RoundNumber,
	)

	return nil
}

// runMultiplier runs the multiplier until it reaches
// the predetermined crash point.
func (e *Engine) runMultiplier(
	ctx context.Context,
	round *GameRound,
) error {

	if round.CrashPoint == nil {
		return fmt.Errorf(
			"round #%d has no crash point",
			round.RoundNumber,
		)
	}

	// Start at 1.00x.
	multiplier := decimal.NewFromInt(1)

	// Temporary simulation tick.
	//
	// Later we will replace this with a time-based
	// multiplier calculation.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:

			// Check whether the multiplier has reached
			// the predetermined crash point.
			if multiplier.GreaterThanOrEqual(
				*round.CrashPoint,
			) {
				log.Printf(
					"round #%d: 💥 multiplier reached crash point %sx",
					round.RoundNumber,
					multiplier.StringFixed(2),
				)

				return nil
			}

			log.Printf(
				"round #%d: multiplier = %sx",
				round.RoundNumber,
				multiplier.StringFixed(2),
			)

			// Temporary multiplier increase.
			//
			// This is NOT the final production
			// multiplier formula.
			multiplier = multiplier.Add(
				decimal.NewFromFloat(0.01),
			)
		}
	}
}

// waitFor waits for a duration while still respecting
// context cancellation.
func waitFor(
	ctx context.Context,
	duration time.Duration,
) error {

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {

	case <-ctx.Done():
		return ctx.Err()

	case <-timer.C:
		return nil
	}
}

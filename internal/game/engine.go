package game

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/settlement"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type Engine struct {
	service           *Service
	multiplierService *multiplier.Service
	settlementService *settlement.Service

	mu      sync.Mutex
	running bool
}

func NewEngine(
	service *Service,
	multiplierService *multiplier.Service,
	settlementService *settlement.Service,
) *Engine {
	return &Engine{
		service:           service,
		multiplierService: multiplierService,
		settlementService: settlementService,
	}
}

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
				log.Printf(
					"game round error: %v",
					err,
				)

				select {
				case <-ctx.Done():
					return

				case <-time.After(2 * time.Second):
				}
			}
		}
	}
}

func (e *Engine) runRound(
	ctx context.Context,
) error {

	// ============================================================
	// CREATE
	// ============================================================

	round, err := e.service.CreateRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"create round: %w",
			err,
		)
	}

	log.Printf(
		"created round #%d",
		round.RoundNumber,
	)

	// ============================================================
	// OPEN BETTING
	// ============================================================

	round, err = e.service.OpenBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"open betting: %w",
			err,
		)
	}

	log.Printf(
		"round #%d: betting opened",
		round.RoundNumber,
	)

	// Give players time to place bets.
	if err := waitFor(
		ctx,
		5*time.Second,
	); err != nil {
		return err
	}

	// ============================================================
	// CLOSE BETTING
	// ============================================================

	round, err = e.service.CloseBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"close betting: %w",
			err,
		)
	}

	log.Printf(
		"round #%d: betting closed",
		round.RoundNumber,
	)

	// ============================================================
	// GENERATE CRASH POINT
	// ============================================================

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

	// ============================================================
	// START ROUND
	// ============================================================

	round, err = e.service.StartRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"start round: %w",
			err,
		)
	}

	log.Printf(
		"round #%d: running",
		round.RoundNumber,
	)

	// ============================================================
	// RUN MULTIPLIER
	// ============================================================

	if err := e.runMultiplier(
		ctx,
		round,
	); err != nil {

		// Make sure the in-memory multiplier state
		// does not remain running if the engine stops
		// unexpectedly.
		e.multiplierService.Stop(round.ID)

		return fmt.Errorf(
			"multiplier loop: %w",
			err,
		)
	}

	// The multiplier has reached the crash point.
	e.multiplierService.Stop(round.ID)

	// ============================================================
	// CRASH ROUND
	// ============================================================

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

	// ============================================================
	// SETTLEMENT
	// ============================================================

	result, err := e.settlementService.SettleRound(
		ctx,
		round.ID,
	)
	if err != nil {
		return fmt.Errorf(
			"settle round: %w",
			err,
		)
	}

	// Remove the multiplier state only after
	// the round has been successfully settled.
	e.multiplierService.Remove(round.ID)

	log.Printf(
		"round #%d: settled, %d bets lost",
		round.RoundNumber,
		result.LostBets,
	)

	return nil
}

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

	// Start the live multiplier state.
	e.multiplierService.Start(round.ID)

	multiplierValue := decimal.NewFromInt(1)

	ticker := time.NewTicker(
		100 * time.Millisecond,
	)

	defer ticker.Stop()

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:

			// Check whether we have reached
			// the predetermined crash point.
			if multiplierValue.GreaterThanOrEqual(
				*round.CrashPoint,
			) {

				log.Printf(
					"round #%d: crash reached at %sx",
					round.RoundNumber,
					multiplierValue.StringFixed(4),
				)

				return nil
			}

			// Publish the current multiplier
			// to the in-memory multiplier service.
			if err := e.multiplierService.Set(
				round.ID,
				multiplierValue,
			); err != nil {
				return fmt.Errorf(
					"set multiplier: %w",
					err,
				)
			}

			log.Printf(
				"round #%d: multiplier = %sx",
				round.RoundNumber,
				multiplierValue.StringFixed(4),
			)

			multiplierValue = multiplierValue.Add(
				decimal.NewFromFloat(0.01),
			)
		}
	}
}

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

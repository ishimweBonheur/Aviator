package game

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/settlement"
	"context"
	"fmt"
	"log"
	"sync"
	"time"
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
		}

		if err := e.runContinuous(ctx); err != nil {
			log.Printf(
				"game engine error: %v",
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

func (e *Engine) runContinuous(
	ctx context.Context,
) error {
	// ============================================================
	// ENSURE CURRENT RUNNING ROUND
	// ============================================================

	runningRound, err := e.service.repository.GetRunningRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get running round: %w",
			err,
		)
	}

	// ============================================================
	// ENSURE UPCOMING BETTING ROUND
	// ============================================================

	bettingRound, err := e.service.repository.GetBettingRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get betting round: %w",
			err,
		)
	}

	// ------------------------------------------------------------
	// No running round yet.
	// ------------------------------------------------------------

	if runningRound == nil {
		if bettingRound == nil {
			bettingRound, err = e.createAndOpenRound(ctx)
			if err != nil {
				return err
			}

			log.Printf(
				"round #%d: initial betting opened",
				bettingRound.RoundNumber,
			)

			if err := waitFor(
				ctx,
				5*time.Second,
			); err != nil {
				return err
			}
		}

		if bettingRound.Status == RoundBettingOpen {
			bettingRound, err = e.prepareRoundForRunning(
				ctx,
				bettingRound,
			)
			if err != nil {
				return err
			}
		}

		runningRound = bettingRound

		// Create the next betting round immediately.
		if _, err := e.ensureBettingRound(ctx); err != nil {
			return err
		}
	}

	// ------------------------------------------------------------
	// A running round already exists.
	// Make sure another round is accepting bets.
	// ------------------------------------------------------------

	if _, err := e.ensureBettingRound(ctx); err != nil {
		return err
	}

	// ============================================================
	// RUN CURRENT ROUND
	// ============================================================

	if err := e.runMultiplier(
		ctx,
		runningRound,
	); err != nil {
		e.multiplierService.Stop(runningRound.ID)

		return fmt.Errorf(
			"multiplier loop: %w",
			err,
		)
	}

	e.multiplierService.Stop(runningRound.ID)

	// ============================================================
	// CRASH CURRENT ROUND
	// ============================================================

	round, err := e.service.CrashRound(
		ctx,
		runningRound.ID,
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
	// SETTLE CURRENT ROUND
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

	e.multiplierService.Remove(round.ID)

	log.Printf(
		"round #%d: settled, %d bets lost",
		round.RoundNumber,
		result.LostBets,
	)

	// ============================================================
	// PROMOTE UPCOMING ROUND
	// ============================================================

	nextRound, err := e.service.repository.GetBettingRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get next betting round: %w",
			err,
		)
	}

	if nextRound == nil {
		return fmt.Errorf(
			"no upcoming betting round exists after round #%d",
			round.RoundNumber,
		)
	}

	nextRound, err = e.prepareRoundForRunning(
		ctx,
		nextRound,
	)
	if err != nil {
		return err
	}

	log.Printf(
		"round #%d: now running",
		nextRound.RoundNumber,
	)

	// ============================================================
	// CREATE NEXT BETTING ROUND
	// ============================================================

	if _, err := e.ensureBettingRound(ctx); err != nil {
		return err
	}

	// The next invocation of runContinuous
	// will run the newly promoted round.
	return nil
}

func (e *Engine) createAndOpenRound(
	ctx context.Context,
) (*GameRound, error) {
	round, err := e.service.CreateRound(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"create round: %w",
			err,
		)
	}

	log.Printf(
		"created round #%d",
		round.RoundNumber,
	)

	round, err = e.service.OpenBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open betting: %w",
			err,
		)
	}

	return round, nil
}

func (e *Engine) ensureBettingRound(
	ctx context.Context,
) (*GameRound, error) {
	round, err := e.service.repository.GetBettingRound(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"get betting round: %w",
			err,
		)
	}

	if round != nil {
		return round, nil
	}

	round, err = e.createAndOpenRound(ctx)
	if err != nil {
		return nil, err
	}

	log.Printf(
		"round #%d: betting opened",
		round.RoundNumber,
	)

	return round, nil
}

func (e *Engine) prepareRoundForRunning(
	ctx context.Context,
	round *GameRound,
) (*GameRound, error) {
	if round.Status != RoundBettingOpen {
		return nil, fmt.Errorf(
			"round #%d cannot be promoted from status %s",
			round.RoundNumber,
			round.Status,
		)
	}

	// ============================================================
	// CLOSE BETTING
	// ============================================================

	round, err := e.service.CloseBetting(
		ctx,
		round.ID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"close betting for round #%d: %w",
			round.RoundNumber,
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
		return nil, fmt.Errorf(
			"generate crash point for round #%d: %w",
			round.RoundNumber,
			err,
		)
	}

	if round.CrashPoint == nil {
		return nil, fmt.Errorf(
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
		return nil, fmt.Errorf(
			"start round #%d: %w",
			round.RoundNumber,
			err,
		)
	}

	return round, nil
}

// ================================================================
// TIME-BASED MULTIPLIER
// ================================================================
//
// The multiplier is now determined by:
//
//     elapsed time since StartedAt
//                    ↓
//             growth function
//
// The ticker only controls how frequently we check the value.
// It no longer controls the multiplier itself.
//
// This means:
//
//     1.00x -> 1.01x -> 1.02x
//
// is NOT caused by:
//
//     ticker -> +0.01
//
// Instead:
//
//     StartedAt + current time -> multiplier
//
// ================================================================

func (e *Engine) runMultiplier(
	ctx context.Context,
	round *GameRound,
) error {
	if round.StartedAt == nil {
		return fmt.Errorf(
			"round #%d has no started_at",
			round.RoundNumber,
		)
	}

	if round.CrashPoint == nil {
		return fmt.Errorf(
			"round #%d has no crash point",
			round.RoundNumber,
		)
	}

	e.multiplierService.Start(
		round.ID,
		*round.StartedAt,
	)

	defer e.multiplierService.Stop(round.ID)

	ticker := time.NewTicker(
		100 * time.Millisecond,
	)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case now := <-ticker.C:

			current := multiplier.Calculate(
				*round.StartedAt,
				now,
			)

			// Never expose a multiplier above the
			// authoritative crash point.
			if current.GreaterThan(*round.CrashPoint) {
				current = *round.CrashPoint
			}

			if err := e.multiplierService.Set(
				round.ID,
				current,
			); err != nil {
				return fmt.Errorf(
					"set multiplier: %w",
					err,
				)
			}

			log.Printf(
				"round #%d: multiplier = %sx",
				round.RoundNumber,
				current.StringFixed(2),
			)

			if current.GreaterThanOrEqual(
				*round.CrashPoint,
			) {
				log.Printf(
					"round #%d: crash reached at %sx",
					round.RoundNumber,
					current.StringFixed(2),
				)

				return nil
			}
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

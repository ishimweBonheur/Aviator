package game

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/settlement"
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const preRoundCountdown = 5 * time.Second

type Engine struct {
	service           *Service
	multiplierService *multiplier.Service
	settlementService *settlement.Service
	realtimeHub       *realtime.Hub

	mu      sync.Mutex
	running bool
}

func NewEngine(
	service *Service,
	multiplierService *multiplier.Service,
	settlementService *settlement.Service,
	realtimeHub *realtime.Hub,
) *Engine {
	return &Engine{
		service:           service,
		multiplierService: multiplierService,
		settlementService: settlementService,
		realtimeHub:       realtimeHub,
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
	// GET CURRENT RUNNING ROUND
	// ============================================================

	runningRound, err := e.service.repository.GetRunningRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get running round: %w",
			err,
		)
	}

	// ============================================================
	// GET UPCOMING BETTING ROUND
	// ============================================================

	bettingRound, err := e.service.repository.GetBettingRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get betting round: %w",
			err,
		)
	}

	// ============================================================
	// NO RUNNING ROUND
	//
	// This happens:
	// - on first startup
	// - after a clean restart
	// - if no round is currently flying
	// ============================================================

	if runningRound == nil {
		// If there is no betting round either,
		// create one and open betting.
		if bettingRound == nil {
			bettingRound, err = e.createAndOpenRound(ctx)
			if err != nil {
				return err
			}

			log.Printf(
				"round #%d: initial betting opened",
				bettingRound.RoundNumber,
			)
		}

		if bettingRound == nil {
			return fmt.Errorf(
				"no betting round available to start",
			)
		}

		// Every round gets a visible countdown before
		// betting is closed and the plane starts.
		if bettingRound.Status == RoundBettingOpen {
			if err := e.waitForNextRound(
				ctx,
				bettingRound,
			); err != nil {
				return err
			}

			bettingRound, err = e.prepareRoundForRunning(
				ctx,
				bettingRound,
			)
			if err != nil {
				return err
			}
		}

		runningRound = bettingRound

		if runningRound == nil {
			return fmt.Errorf(
				"failed to establish running round",
			)
		}

		log.Printf(
			"round #%d: now running",
			runningRound.RoundNumber,
		)

		// As soon as this round starts flying,
		// create/open the next betting round.
		if _, err := e.ensureBettingRound(ctx); err != nil {
			return err
		}
	}

	// ============================================================
	// ENSURE UPCOMING BETTING ROUND EXISTS
	//
	// While the current plane is flying, players can already
	// place bets on the next round.
	// ============================================================

	if _, err := e.ensureBettingRound(ctx); err != nil {
		return err
	}

	// ============================================================
	// RUN CURRENT ROUND MULTIPLIER
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

	if round.CrashPoint == nil {
		return fmt.Errorf(
			"round #%d crashed without crash point",
			round.RoundNumber,
		)
	}

	e.realtimeHub.Broadcast(realtime.Event{
		Type:        realtime.EventRoundCrashed,
		RoundID:     round.ID,
		RoundNumber: round.RoundNumber,
		CrashPoint:  round.CrashPoint.StringFixed(2),
	})

	log.Printf(
		"round #%d: crashed at %sx",
		round.RoundNumber,
		round.CrashPoint.StringFixed(2),
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

	e.realtimeHub.Broadcast(realtime.Event{
		Type:        realtime.EventRoundSettled,
		RoundID:     round.ID,
		RoundNumber: round.RoundNumber,
	})

	log.Printf(
		"round #%d: settled, %d bets lost",
		round.RoundNumber,
		result.LostBets,
	)

	// ============================================================
	// GET / CREATE NEXT BETTING ROUND
	// ============================================================

	nextRound, err := e.service.repository.GetBettingRound(ctx)
	if err != nil {
		return fmt.Errorf(
			"get next betting round: %w",
			err,
		)
	}

	// Normally this already exists because it was created while
	// the previous round was flying.
	//
	// But if it does not exist for some reason, recover by
	// creating one now.
	if nextRound == nil {
		nextRound, err = e.ensureBettingRound(ctx)
		if err != nil {
			return fmt.Errorf(
				"ensure next betting round: %w",
				err,
			)
		}
	}

	if nextRound == nil {
		return fmt.Errorf(
			"next betting round is nil",
		)
	}

	// ============================================================
	// PRE-ROUND COUNTDOWN
	//
	// Even though this round may already have been accepting bets
	// while the previous plane was flying, we keep a final
	// countdown before starting it.
	// ============================================================

	if nextRound.Status == RoundBettingOpen {
		if err := e.waitForNextRound(
			ctx,
			nextRound,
		); err != nil {
			return err
		}

		nextRound, err = e.prepareRoundForRunning(
			ctx,
			nextRound,
		)
		if err != nil {
			return err
		}
	}

	log.Printf(
		"round #%d: now running",
		nextRound.RoundNumber,
	)

	// ============================================================
	// CREATE THE FOLLOWING BETTING ROUND
	//
	// Now that nextRound is RUNNING, immediately create another
	// BETTING_OPEN round so players can bet ahead.
	// ============================================================

	if _, err := e.ensureBettingRound(ctx); err != nil {
		return err
	}

	// The next runContinuous() invocation finds nextRound
	// as the current RUNNING round.
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

	log.Printf(
		"round #%d: betting opened",
		round.RoundNumber,
	)

	e.realtimeHub.Broadcast(realtime.Event{
		Type:        realtime.EventRoundOpened,
		RoundID:     round.ID,
		RoundNumber: round.RoundNumber,
	})

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

	return round, nil
}

func (e *Engine) prepareRoundForRunning(
	ctx context.Context,
	round *GameRound,
) (*GameRound, error) {
	if round == nil {
		return nil, fmt.Errorf(
			"cannot prepare nil round",
		)
	}

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
		round.CrashPoint.StringFixed(2),
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

	log.Printf(
		"round #%d: started",
		round.RoundNumber,
	)

	e.realtimeHub.Broadcast(realtime.Event{
		Type:        realtime.EventRoundStarted,
		RoundID:     round.ID,
		RoundNumber: round.RoundNumber,
	})

	return round, nil
}

// ================================================================
// TIME-BASED MULTIPLIER
// ================================================================
//
// The multiplier is determined from elapsed time:
//
//	multiplier = e^(growthRate * elapsedSeconds)
//
// The ticker only determines how often the current value is
// published. It does not increment the multiplier itself.
//
// ================================================================

func (e *Engine) runMultiplier(
	ctx context.Context,
	round *GameRound,
) error {
	if round == nil {
		return fmt.Errorf(
			"cannot run multiplier for nil round",
		)
	}

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
			if current.GreaterThan(
				*round.CrashPoint,
			) {
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

			e.realtimeHub.Broadcast(realtime.Event{
				Type:        realtime.EventMultiplierUpdate,
				RoundID:     round.ID,
				RoundNumber: round.RoundNumber,
				Multiplier:  current.StringFixed(2),
			})

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

// ================================================================
// PRE-ROUND COUNTDOWN
// ================================================================

func (e *Engine) waitForNextRound(
	ctx context.Context,
	round *GameRound,
) error {
	if round == nil {
		return fmt.Errorf(
			"cannot start countdown for nil round",
		)
	}

	remaining := int(
		preRoundCountdown / time.Second,
	)

	log.Printf(
		"round #%d: starts in %d seconds",
		round.RoundNumber,
		remaining,
	)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for remaining > 0 {
		log.Printf(
			"round #%d: starting in %d...",
			round.RoundNumber,
			remaining,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			remaining--
		}
	}

	log.Printf(
		"round #%d: countdown finished",
		round.RoundNumber,
	)

	return nil
}

package game

import (
	"aviator/backend/internal/database"
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// RunLeader holds a PostgreSQL session lock as a second fence. All lifecycle SQL
// uses this same connection: a lost DB session cannot mutate via another pool
// connection. Release only after the loop stops, including on Redis lease loss.
func (e *Engine) RunLeader(ctx context.Context) {
	conn, err := e.service.repository.db.Acquire(ctx)
	if err != nil {
		log.Printf("engine DB guard: %v", err)
		return
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(728194620)").Scan(&locked); err != nil || !locked {
		return
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := conn.Exec(c, "SELECT pg_advisory_unlock(728194620)"); err != nil {
			_ = conn.Conn().Close(c)
		}
	}()
	ctx = database.WithLeaderConnection(ctx, conn.Conn())
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := e.recoverStep(ctx); err != nil {
			log.Printf("engine recovery: %v", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) emit(ctx context.Context, kind string, r *GameRound) {
	event := realtime.Event{Type: kind, RoundID: r.ID, RoundNumber: r.RoundNumber, BettingClosesAt: r.BettingClosesAt}
	if kind == realtime.EventRoundCrashed && r.CrashPoint != nil {
		event.CrashPoint = r.CrashPoint.StringFixed(2)
	}
	e.publisher.Publish(ctx, event)
}

// Each pass resumes from durable state. The next round opens immediately while
// the current round runs; it can close on its deadline and wait for promotion.
func (e *Engine) recoverStep(ctx context.Context) error {
	repo := e.service.repository
	// Complete interrupted settlements first.
	for {
		crashed, err := repo.getRoundByStatus(ctx, RoundCrashed)
		if err != nil {
			return err
		}
		if crashed == nil {
			break
		}
		if _, err := e.settlementService.SettleRound(ctx, crashed.ID); err != nil {
			return err
		}
		e.multiplierService.Remove(crashed.ID)
		e.emit(ctx, realtime.EventRoundSettled, crashed)
	}
	running, err := repo.GetRunningRound(ctx)
	if err != nil {
		return err
	}
	if running != nil {
		if running.StartedAt == nil || running.CrashPoint == nil {
			return fmt.Errorf("running round %d has invalid timing", running.ID)
		}
		clock := multiplier.Clock{Rate: running.GrowthRate}
		crashAt, err := clock.CrashAt(*running.StartedAt, *running.CrashPoint)
		if err != nil {
			return err
		}
		now := time.Now()
		if !now.Before(crashAt) {
			crashed, err := e.service.CrashRound(ctx, running.ID)
			if err != nil {
				return err
			}
			e.emit(ctx, realtime.EventRoundCrashed, crashed)
			if _, err := e.settlementService.SettleRound(ctx, running.ID); err != nil {
				return err
			}
			e.multiplierService.Remove(running.ID)
			e.emit(ctx, realtime.EventRoundSettled, crashed)
			running = nil
		} else {
			current := clock.Calculate(*running.StartedAt, now)
			if current.GreaterThan(*running.CrashPoint) {
				current = *running.CrashPoint
			}
			e.multiplierService.Start(running.ID, *running.StartedAt)
			if err := e.multiplierService.Set(running.ID, current); err != nil {
				return err
			}
			e.publisher.Publish(ctx, realtime.Event{Type: realtime.EventMultiplierUpdate, RoundID: running.ID, RoundNumber: running.RoundNumber, Multiplier: current.StringFixed(2)})
			e.liveState.State(ctx, "current", map[string]any{"round_id": running.ID, "round_number": running.RoundNumber, "status": running.Status, "started_at": running.StartedAt, "current_multiplier": current.StringFixed(2)})
		}
	}
	upcoming, err := repo.GetUpcomingRound(ctx)
	if err != nil {
		return err
	}
	if upcoming == nil {
		upcoming, err = e.service.CreateRound(ctx)
		if err != nil {
			return err
		}
	}
	if upcoming.Status == RoundCreated {
		if _, err := database.Query(ctx, repo.db).Exec(ctx, `UPDATE game_rounds SET status='BETTING_OPEN', betting_opened_at=clock_timestamp(), betting_closes_at=clock_timestamp()+$2*interval '1 second', growth_rate=$3 WHERE id=$1 AND status='CREATED'`, upcoming.ID, e.bettingWindow.Seconds(), e.growthRate); err != nil {
			return err
		}
		upcoming, err = repo.GetRoundByID(ctx, upcoming.ID)
		if err != nil {
			return err
		}
		e.emit(ctx, realtime.EventRoundOpened, upcoming)
	}
	if upcoming.Status == RoundBettingOpen {
		if upcoming.BettingClosesAt == nil {
			// Upgrade recovery for a legacy process that opened a round during
			// migration. Anchor to its original creation time, never restart now.
			if _, err := database.Query(ctx, repo.db).Exec(ctx, `UPDATE game_rounds SET betting_opened_at=COALESCE(betting_opened_at,created_at), betting_closes_at=created_at+$2*interval '1 second' WHERE id=$1 AND betting_closes_at IS NULL`, upcoming.ID, e.bettingWindow.Seconds()); err != nil {
				return err
			}
			upcoming, err = repo.GetRoundByID(ctx, upcoming.ID)
			if err != nil {
				return err
			}
		}
		seconds := int(math.Max(0, math.Ceil(time.Until(*upcoming.BettingClosesAt).Seconds())))
		if e.countdownRound != upcoming.ID || e.countdownSeconds != seconds {
			e.publisher.Publish(ctx, realtime.Event{Type: realtime.EventCountdown, RoundID: upcoming.ID, RoundNumber: upcoming.RoundNumber, SecondsRemaining: &seconds, BettingClosesAt: upcoming.BettingClosesAt})
			e.countdownRound = upcoming.ID
			e.countdownSeconds = seconds
		}
		if seconds == 0 {
			upcoming, err = e.service.CloseBetting(ctx, upcoming.ID)
			if err != nil {
				return err
			}
		}
	}
	e.liveState.State(ctx, "upcoming", upcoming)
	if running == nil && upcoming.Status == RoundBettingClosed {
		if upcoming.CrashPoint == nil {
			upcoming, err = e.service.GenerateCrashPoint(ctx, upcoming.ID)
			if err != nil {
				return err
			}
		}
		upcoming, err = e.service.StartRound(ctx, upcoming.ID)
		if err != nil {
			return err
		}
		e.emit(ctx, realtime.EventRoundStarted, upcoming)
	}
	if running == nil {
		e.liveState.State(ctx, "current", nil)
	}
	return nil
}

func (r *Repository) GetUpcomingRound(ctx context.Context) (*GameRound, error) {
	for _, status := range []RoundStatus{RoundBettingClosed, RoundBettingOpen, RoundCreated} {
		round, err := r.getRoundByStatus(ctx, status)
		if err != nil || round != nil {
			return round, err
		}
	}
	return nil, nil
}

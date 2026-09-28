package cashout

import (
	"context"
	"log"
	"time"
)

// RunAutomatic performs persisted automatic cashouts without browser timers.
// Each transaction rechecks the ACTIVE bet, RUNNING round and strict crash deadline.
func (s *Service) RunAutomatic(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick, cancel := context.WithTimeout(ctx, time.Second)
			if err := s.ProcessAutomatic(tick); err != nil && ctx.Err() == nil {
				log.Printf("automatic cashouts: %v", err)
			}
			cancel()
		}
	}
}
func (s *Service) ProcessAutomatic(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT b.id,b.user_id FROM bets b JOIN game_rounds g ON g.id=b.round_id WHERE b.status='ACTIVE' AND b.auto_cashout_multiplier IS NOT NULL AND g.status='RUNNING' AND g.growth_rate>0 AND g.started_at + (ln(b.auto_cashout_multiplier)::double precision/g.growth_rate)*interval '1 second' <= clock_timestamp() ORDER BY b.id LIMIT 1000`)
	if err != nil {
		return err
	}
	type candidate struct{ id, user int64 }
	items := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.user); err != nil {
			rows.Close()
			return err
		}
		items = append(items, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A different instance or manual request may already have resolved the bet.
		// A target below the current tick can still lose if processing reaches crash_at.
		_, _ = s.cashOut(ctx, c.user, c.id, true)
	}
	return nil
}

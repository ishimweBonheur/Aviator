package settlement

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db         *pgxpool.Pool
	repository *Repository
}

func NewService(
	db *pgxpool.Pool,
	repository *Repository,
) *Service {
	return &Service{
		db:         db,
		repository: repository,
	}
}

func (s *Service) SettleRound(
	ctx context.Context,
	roundID int64,
) (*Result, error) {

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin settlement transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	status, err := s.repository.GetRoundStatus(
		ctx,
		tx,
		roundID,
	)
	if err != nil {
		return nil, err
	}

	// Settlement is idempotent.
	if status == "SETTLED" {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf(
				"commit already-settled transaction: %w",
				err,
			)
		}

		return &Result{
			RoundID:        roundID,
			AlreadySettled: true,
		}, nil
	}

	if status != "CRASHED" {
		return nil, fmt.Errorf(
			"round %d cannot be settled from status %s",
			roundID,
			status,
		)
	}

	// Any bet that did not cash out before
	// the crash becomes a loss.
	lostBets, err := s.repository.MarkActiveBetsLost(
		ctx,
		tx,
		roundID,
	)
	if err != nil {
		return nil, err
	}

	// Only after all active bets have been
	// processed do we mark the round settled.
	if err := s.repository.MarkRoundSettled(
		ctx,
		tx,
		roundID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit settlement transaction: %w",
			err,
		)
	}

	return &Result{
		RoundID:  roundID,
		LostBets: lostBets,
	}, nil
}

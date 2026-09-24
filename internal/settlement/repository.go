package settlement

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) GetRoundStatus(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
) (string, error) {

	var status string

	err := tx.QueryRow(
		ctx,
		`
		SELECT status
		FROM game_rounds
		WHERE id = $1
		FOR UPDATE
		`,
		roundID,
	).Scan(&status)

	if err != nil {
		return "", fmt.Errorf(
			"get round status: %w",
			err,
		)
	}

	return status, nil
}

func (r *Repository) MarkActiveBetsLost(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
) (int, error) {

	result, err := tx.Exec(
		ctx,
		`
		UPDATE bets
		SET
			status = 'LOST',
			payout = 0.00
		WHERE
			round_id = $1
			AND status = 'ACTIVE'
		`,
		roundID,
	)

	if err != nil {
		return 0, fmt.Errorf(
			"mark active bets lost: %w",
			err,
		)
	}

	return int(result.RowsAffected()), nil
}

func (r *Repository) MarkRoundSettled(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
) error {

	_, err := tx.Exec(
		ctx,
		`
		UPDATE game_rounds
		SET status = 'SETTLED',
		    ended_at = COALESCE(ended_at, $2)
		WHERE id = $1
		  AND status = 'CRASHED'
		`,
		roundID,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf(
			"mark round settled: %w",
			err,
		)
	}

	return nil
}

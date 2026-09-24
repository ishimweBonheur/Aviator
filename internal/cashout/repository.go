package cashout

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

type Bet struct {
	ID      int64
	RoundID int64
	UserID  int64
	Amount  decimal.Decimal
	Status  string
}

func (r *Repository) GetActiveBetForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	betID int64,
	userID int64,
) (*Bet, error) {
	var (
		bet          Bet
		amountString string
	)

	err := tx.QueryRow(ctx, `
		SELECT
			id,
			round_id,
			user_id,
			amount::text,
			status
		FROM bets
		WHERE id = $1
		  AND user_id = $2
		FOR UPDATE
	`,
		betID,
		userID,
	).Scan(
		&bet.ID,
		&bet.RoundID,
		&bet.UserID,
		&amountString,
		&bet.Status,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("bet not found")
		}

		return nil, fmt.Errorf(
			"failed to get bet: %w",
			err,
		)
	}

	bet.Amount, err = decimal.NewFromString(amountString)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid bet amount: %w",
			err,
		)
	}

	return &bet, nil
}

func (r *Repository) GetRoundStatus(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
) (string, error) {
	var status string

	err := tx.QueryRow(ctx, `
		SELECT status
		FROM game_rounds
		WHERE id = $1
		FOR SHARE
	`, roundID).Scan(&status)

	if err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("round not found")
		}

		return "", fmt.Errorf(
			"failed to get round status: %w",
			err,
		)
	}

	return status, nil
}

func (r *Repository) CashOut(
	ctx context.Context,
	tx pgx.Tx,
	betID int64,
	multiplier decimal.Decimal,
	payout decimal.Decimal,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE bets
		SET
			status = 'CASHED_OUT',
			cashout_multiplier = $1,
			payout = $2,
			cashed_out_at = NOW()
		WHERE id = $3
		  AND status = 'ACTIVE'
	`,
		multiplier.StringFixed(4),
		payout.StringFixed(2),
		betID,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to cash out bet: %w",
			err,
		)
	}

	return nil
}

package betting

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

func (r *Repository) CountUserBetsForRound(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
	userID int64,
) (int, error) {
	var count int

	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM bets
		WHERE round_id = $1
		  AND user_id = $2
		  AND status IN ('ACTIVE', 'CASHED_OUT')
	`, roundID, userID).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("failed to count user bets: %w", err)
	}

	return count, nil
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

		return "", fmt.Errorf("failed to get round status: %w", err)
	}

	return status, nil
}

func (r *Repository) CreateBet(
	ctx context.Context,
	tx pgx.Tx,
	roundID int64,
	userID int64,
	betNumber int16,
	amount decimal.Decimal,
) (*Bet, error) {
	var (
		bet          Bet
		amountString string
		payoutString string
		placedAt     string
		createdAt    string
	)

	err := tx.QueryRow(ctx, `
		INSERT INTO bets (
			round_id,
			user_id,
			bet_number,
			amount,
			status,
			payout
		)
		VALUES ($1, $2, $3, $4, 'ACTIVE', 0.00)
		RETURNING
			id,
			round_id,
			user_id,
			bet_number,
			amount::text,
			status,
			payout::text,
			placed_at::text,
			created_at::text
	`,
		roundID,
		userID,
		betNumber,
		amount.StringFixed(2),
	).Scan(
		&bet.ID,
		&bet.RoundID,
		&bet.UserID,
		&bet.BetNumber,
		&amountString,
		&bet.Status,
		&payoutString,
		&placedAt,
		&createdAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create bet: %w", err)
	}

	bet.Amount, err = decimal.NewFromString(amountString)
	if err != nil {
		return nil, fmt.Errorf("invalid bet amount returned by database: %w", err)
	}

	bet.Payout, err = decimal.NewFromString(payoutString)
	if err != nil {
		return nil, fmt.Errorf("invalid payout returned by database: %w", err)
	}

	bet.PlacedAt = placedAt
	bet.CreatedAt = createdAt

	return &bet, nil
}

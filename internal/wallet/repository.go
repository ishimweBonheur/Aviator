package wallet

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
	return &Repository{
		db: db,
	}
}

// GetBalance returns the current balance for a user.
func (r *Repository) GetBalance(
	ctx context.Context,
	userID int64,
) (decimal.Decimal, error) {
	var balanceString string

	err := r.db.QueryRow(
		ctx,
		`
		SELECT balance::text
		FROM users
		WHERE id = $1
		`,
		userID,
	).Scan(&balanceString)

	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, fmt.Errorf("user %d not found", userID)
		}

		return decimal.Zero, fmt.Errorf("failed to get balance: %w", err)
	}

	balance, err := decimal.NewFromString(balanceString)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid balance in database: %w", err)
	}

	return balance, nil
}

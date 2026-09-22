package wallet

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		db: db,
	}
}

// Debit removes money from a user's wallet.
//
// This is used for things such as:
// - placing a bet
// - processing a withdrawal
func (s *Service) Debit(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be greater than zero")
	}

	if transactionType == "" {
		return fmt.Errorf("transaction type is required")
	}

	if reference == "" {
		return fmt.Errorf("transaction reference is required")
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin wallet transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the user's row.
	//
	// This is extremely important for a real-money wallet.
	// It prevents two simultaneous operations from modifying
	// the same balance incorrectly.
	var balanceString string

	err = tx.QueryRow(
		ctx,
		`
		SELECT balance::text
		FROM users
		WHERE id = $1
		FOR UPDATE
		`,
		userID,
	).Scan(&balanceString)

	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("user %d not found", userID)
		}

		return fmt.Errorf("failed to lock user wallet: %w", err)
	}

	balance, err := decimal.NewFromString(balanceString)
	if err != nil {
		return fmt.Errorf("invalid wallet balance: %w", err)
	}

	if balance.LessThan(amount) {
		return fmt.Errorf("insufficient balance")
	}

	newBalance := balance.Sub(amount)

	// Update balance.
	_, err = tx.Exec(
		ctx,
		`
		UPDATE users
		SET
			balance = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		newBalance.String(),
		userID,
	)

	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}

	// Record the transaction.
	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO wallet_transactions (
			user_id,
			type,
			amount,
			reference,
			balance_before,
			balance_after
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		`,
		userID,
		transactionType,
		amount.String(),
		reference,
		balance.String(),
		newBalance.String(),
	)

	if err != nil {
		return fmt.Errorf("failed to record wallet transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit wallet transaction: %w", err)
	}

	return nil
}

// Credit adds money to a user's wallet.
//
// This will eventually be used for:
// - deposits
// - winnings
// - refunds
func (s *Service) Credit(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be greater than zero")
	}

	if transactionType == "" {
		return fmt.Errorf("transaction type is required")
	}

	if reference == "" {
		return fmt.Errorf("transaction reference is required")
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin wallet transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock wallet.
	var balanceString string

	err = tx.QueryRow(
		ctx,
		`
		SELECT balance::text
		FROM users
		WHERE id = $1
		FOR UPDATE
		`,
		userID,
	).Scan(&balanceString)

	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("user %d not found", userID)
		}

		return fmt.Errorf("failed to lock user wallet: %w", err)
	}

	balance, err := decimal.NewFromString(balanceString)
	if err != nil {
		return fmt.Errorf("invalid wallet balance: %w", err)
	}

	newBalance := balance.Add(amount)

	// Update balance.
	_, err = tx.Exec(
		ctx,
		`
		UPDATE users
		SET
			balance = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		newBalance.String(),
		userID,
	)

	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}

	// Record transaction.
	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO wallet_transactions (
			user_id,
			type,
			amount,
			reference,
			balance_before,
			balance_after
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		`,
		userID,
		transactionType,
		amount.String(),
		reference,
		balance.String(),
		newBalance.String(),
	)

	if err != nil {
		return fmt.Errorf("failed to record wallet transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit wallet transaction: %w", err)
	}

	return nil
}

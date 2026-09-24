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
	return &Service{db: db}
}

func (s *Service) Debit(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if err := validateTransaction(amount, transactionType, reference); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin wallet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.DebitTx(ctx, tx, userID, amount, transactionType, reference); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit wallet transaction: %w", err)
	}

	return nil
}

func (s *Service) DebitTx(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if err := validateTransaction(amount, transactionType, reference); err != nil {
		return err
	}

	var balanceString string
	err := tx.QueryRow(ctx, `
		SELECT balance::text
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&balanceString)
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
	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET balance = $1, updated_at = NOW()
		WHERE id = $2
	`, newBalance.String(), userID); err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_transactions (
			user_id, type, amount, reference, balance_before, balance_after
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, transactionType, amount.String(), reference, balance.String(), newBalance.String()); err != nil {
		return fmt.Errorf("failed to record wallet transaction: %w", err)
	}

	return nil
}

func (s *Service) Credit(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if err := validateTransaction(amount, transactionType, reference); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin wallet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.CreditTx(ctx, tx, userID, amount, transactionType, reference); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit wallet transaction: %w", err)
	}
	return nil
}

func (s *Service) CreditTx(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount decimal.Decimal,
	transactionType string,
	reference string,
) error {
	if err := validateTransaction(amount, transactionType, reference); err != nil {
		return err
	}

	var balanceString string
	err := tx.QueryRow(ctx, `
		SELECT balance::text
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&balanceString)
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

	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET balance = $1, updated_at = NOW()
		WHERE id = $2
	`, newBalance.String(), userID); err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_transactions (
			user_id, type, amount, reference, balance_before, balance_after
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, transactionType, amount.String(), reference, balance.String(), newBalance.String()); err != nil {
		return fmt.Errorf("failed to record wallet transaction: %w", err)
	}

	return nil
}

func validateTransaction(amount decimal.Decimal, transactionType, reference string) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be greater than zero")
	}
	if transactionType == "" {
		return fmt.Errorf("transaction type is required")
	}
	if reference == "" {
		return fmt.Errorf("transaction reference is required")
	}
	return nil
}
func (s *Service) GetBalance(
	ctx context.Context,
	userID int64,
) (decimal.Decimal, error) {
	var balanceString string

	err := s.db.QueryRow(ctx, `
		SELECT balance::text
		FROM users
		WHERE id = $1
	`, userID).Scan(&balanceString)

	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, fmt.Errorf(
				"user %d not found",
				userID,
			)
		}

		return decimal.Zero, fmt.Errorf(
			"failed to get balance: %w",
			err,
		)
	}

	balance, err := decimal.NewFromString(balanceString)
	if err != nil {
		return decimal.Zero, fmt.Errorf(
			"invalid balance: %w",
			err,
		)
	}

	return balance, nil
}

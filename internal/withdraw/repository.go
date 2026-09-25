package withdrawal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

var (
	ErrInsufficientBalance    = errors.New("insufficient balance")
	ErrWithdrawalNotFound     = errors.New("withdrawal not found")
	ErrInvalidWithdrawalState = errors.New("invalid withdrawal state")
	ErrUserNotActive          = errors.New("user is not active")
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(
	db *pgxpool.Pool,
) *Repository {
	return &Repository{
		db: db,
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWithdrawal(
	row rowScanner,
) (*Withdrawal, error) {
	var (
		withdrawal Withdrawal
		amountRaw  string
		statusRaw  string
	)

	err := row.Scan(
		&withdrawal.ID,
		&withdrawal.UserID,
		&amountRaw,
		&withdrawal.Provider,
		&withdrawal.ProviderReference,
		&statusRaw,
		&withdrawal.CreatedAt,
		&withdrawal.CompletedAt,
	)
	if err != nil {
		return nil, err
	}

	amount, err := decimal.NewFromString(
		amountRaw,
	)
	if err != nil {
		return nil, err
	}

	withdrawal.Amount = amount
	withdrawal.Status = Status(statusRaw)

	return &withdrawal, nil
}

func (r *Repository) CreateAndReserve(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	provider string,
) (*CreateResult, error) {
	tx, err := r.db.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var (
		balanceRaw string
		userStatus string
	)

	err = tx.QueryRow(
		ctx,
		`
		SELECT balance::text, status
		FROM users
		WHERE id = $1
		FOR UPDATE
		`,
		userID,
	).Scan(
		&balanceRaw,
		&userStatus,
	)
	if err != nil {
		return nil, err
	}

	if userStatus != "ACTIVE" {
		return nil, ErrUserNotActive
	}

	balanceBefore, err := decimal.NewFromString(
		balanceRaw,
	)
	if err != nil {
		return nil, err
	}

	if balanceBefore.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	balanceAfter := balanceBefore.Sub(amount)

	row := tx.QueryRow(
		ctx,
		`
		INSERT INTO withdrawals (
			user_id,
			amount,
			provider,
			status
		)
		VALUES ($1, $2, $3, 'PENDING')
		RETURNING
			id,
			user_id,
			amount::text,
			provider,
			provider_reference,
			status,
			created_at,
			completed_at
		`,
		userID,
		amount.StringFixed(2),
		provider,
	)

	withdrawal, err := scanWithdrawal(row)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`
		UPDATE users
		SET balance = $1,
		    updated_at = NOW()
		WHERE id = $2
		`,
		balanceAfter.StringFixed(2),
		userID,
	)
	if err != nil {
		return nil, err
	}

	reference := fmt.Sprintf(
		"withdrawal:%d:reserve",
		withdrawal.ID,
	)

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
		VALUES ($1, 'WITHDRAWAL', $2, $3, $4, $5)
		`,
		userID,
		amount.StringFixed(2),
		reference,
		balanceBefore.StringFixed(2),
		balanceAfter.StringFixed(2),
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &CreateResult{
		Withdrawal:       withdrawal,
		RemainingBalance: balanceAfter,
	}, nil
}

func (r *Repository) ListByUser(
	ctx context.Context,
	userID int64,
	limit int,
) ([]Withdrawal, error) {
	rows, err := r.db.Query(
		ctx,
		`
		SELECT
			id,
			user_id,
			amount::text,
			provider,
			provider_reference,
			status,
			created_at,
			completed_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
		`,
		userID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	withdrawals := make([]Withdrawal, 0)

	for rows.Next() {
		withdrawal, err := scanWithdrawal(
			rows,
		)
		if err != nil {
			return nil, err
		}

		withdrawals = append(
			withdrawals,
			*withdrawal,
		)
	}

	return withdrawals, rows.Err()
}

func (r *Repository) Complete(
	ctx context.Context,
	withdrawalID int64,
	providerReference string,
) (*Withdrawal, error) {
	tx, err := r.db.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	withdrawal, err := scanWithdrawal(
		tx.QueryRow(
			ctx,
			`
			SELECT
				id,
				user_id,
				amount::text,
				provider,
				provider_reference,
				status,
				created_at,
				completed_at
			FROM withdrawals
			WHERE id = $1
			FOR UPDATE
			`,
			withdrawalID,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}

	if withdrawal.Status == StatusCompleted {
		return withdrawal, nil
	}

	if withdrawal.Status != StatusPending &&
		withdrawal.Status != StatusProcessing {
		return nil, ErrInvalidWithdrawalState
	}

	var completedAt time.Time

	err = tx.QueryRow(
		ctx,
		`
		UPDATE withdrawals
		SET status = 'COMPLETED',
		    provider_reference = $1,
		    completed_at = NOW()
		WHERE id = $2
		RETURNING completed_at
		`,
		providerReference,
		withdrawal.ID,
	).Scan(&completedAt)
	if err != nil {
		return nil, err
	}

	withdrawal.Status = StatusCompleted
	withdrawal.ProviderReference = &providerReference
	withdrawal.CompletedAt = &completedAt

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return withdrawal, nil
}

// FailAndRefund atomically changes the withdrawal to FAILED
// and returns the reserved money to the user's wallet.
func (r *Repository) FailAndRefund(
	ctx context.Context,
	withdrawalID int64,
) (*Withdrawal, error) {
	tx, err := r.db.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	withdrawal, err := scanWithdrawal(
		tx.QueryRow(
			ctx,
			`
			SELECT
				id,
				user_id,
				amount::text,
				provider,
				provider_reference,
				status,
				created_at,
				completed_at
			FROM withdrawals
			WHERE id = $1
			FOR UPDATE
			`,
			withdrawalID,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}

	if withdrawal.Status == StatusFailed ||
		withdrawal.Status == StatusCancelled {
		return withdrawal, nil
	}

	if withdrawal.Status == StatusCompleted {
		return nil, ErrInvalidWithdrawalState
	}

	var balanceRaw string

	err = tx.QueryRow(
		ctx,
		`
		SELECT balance::text
		FROM users
		WHERE id = $1
		FOR UPDATE
		`,
		withdrawal.UserID,
	).Scan(&balanceRaw)
	if err != nil {
		return nil, err
	}

	balanceBefore, err := decimal.NewFromString(
		balanceRaw,
	)
	if err != nil {
		return nil, err
	}

	balanceAfter := balanceBefore.Add(
		withdrawal.Amount,
	)

	_, err = tx.Exec(
		ctx,
		`
		UPDATE users
		SET balance = $1,
		    updated_at = NOW()
		WHERE id = $2
		`,
		balanceAfter.StringFixed(2),
		withdrawal.UserID,
	)
	if err != nil {
		return nil, err
	}

	reference := fmt.Sprintf(
		"withdrawal:%d:refund",
		withdrawal.ID,
	)

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
		VALUES ($1, 'REFUND', $2, $3, $4, $5)
		`,
		withdrawal.UserID,
		withdrawal.Amount.StringFixed(2),
		reference,
		balanceBefore.StringFixed(2),
		balanceAfter.StringFixed(2),
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`
		UPDATE withdrawals
		SET status = 'FAILED'
		WHERE id = $1
		`,
		withdrawal.ID,
	)
	if err != nil {
		return nil, err
	}

	withdrawal.Status = StatusFailed

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return withdrawal, nil
}

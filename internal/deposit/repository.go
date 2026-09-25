package deposit

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
	ErrDepositNotFound     = errors.New("deposit not found")
	ErrInvalidDepositState = errors.New("invalid deposit state")
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		db: db,
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDeposit(row rowScanner) (*Deposit, error) {
	var (
		deposit   Deposit
		amountRaw string
		statusRaw string
	)

	err := row.Scan(
		&deposit.ID,
		&deposit.UserID,
		&amountRaw,
		&deposit.Provider,
		&deposit.ProviderReference,
		&statusRaw,
		&deposit.CreatedAt,
		&deposit.CompletedAt,
	)
	if err != nil {
		return nil, err
	}

	amount, err := decimal.NewFromString(amountRaw)
	if err != nil {
		return nil, fmt.Errorf(
			"parse deposit amount: %w",
			err,
		)
	}

	deposit.Amount = amount
	deposit.Status = Status(statusRaw)

	return &deposit, nil
}

func (r *Repository) Create(
	ctx context.Context,
	userID int64,
	amount decimal.Decimal,
	provider string,
	providerReference string,
) (*Deposit, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(
		ctx,
		`
		INSERT INTO deposits (
			user_id,
			amount,
			provider,
			provider_reference,
			status
		)
		VALUES ($1, $2, $3, $4, 'PENDING')
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
		providerReference,
	)

	deposit, err := scanDeposit(row)
	if err != nil {
		return nil, fmt.Errorf(
			"create deposit: %w",
			err,
		)
	}

	if provider == string(ProviderSandbox) {
		deposit, err = r.completeTx(ctx, tx, provider, providerReference)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return deposit, nil
}

func (r *Repository) ListByUser(
	ctx context.Context,
	userID int64,
	limit int,
) ([]Deposit, error) {
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
		FROM deposits
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
		`,
		userID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list deposits: %w",
			err,
		)
	}
	defer rows.Close()

	deposits := make([]Deposit, 0)

	for rows.Next() {
		deposit, err := scanDeposit(rows)
		if err != nil {
			return nil, err
		}

		deposits = append(
			deposits,
			*deposit,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return deposits, nil
}

// CompleteByProviderReference is intended to be called by the future
// verified payment-provider webhook.
//
// This operation is idempotent:
// a COMPLETED deposit will not credit the wallet twice.
func (r *Repository) CompleteByProviderReference(
	ctx context.Context,
	provider string,
	providerReference string,
) (*Deposit, error) {
	tx, err := r.db.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	deposit, err := r.completeTx(ctx, tx, provider, providerReference)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return deposit, nil
}

func (r *Repository) completeTx(ctx context.Context, tx pgx.Tx, provider, providerReference string) (*Deposit, error) {
	row := tx.QueryRow(
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
		FROM deposits
		WHERE provider = $1
		  AND provider_reference = $2
		FOR UPDATE
		`,
		provider,
		providerReference,
	)

	deposit, err := scanDeposit(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDepositNotFound
	}
	if err != nil {
		return nil, err
	}

	if deposit.Status == StatusCompleted {
		return deposit, nil
	}

	if deposit.Status != StatusPending &&
		deposit.Status != StatusProcessing {
		return nil, ErrInvalidDepositState
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
		deposit.UserID,
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
		deposit.Amount,
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
		deposit.UserID,
	)
	if err != nil {
		return nil, err
	}

	reference := fmt.Sprintf(
		"deposit:%d",
		deposit.ID,
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
		VALUES ($1, 'DEPOSIT', $2, $3, $4, $5)
		`,
		deposit.UserID,
		deposit.Amount.StringFixed(2),
		reference,
		balanceBefore.StringFixed(2),
		balanceAfter.StringFixed(2),
	)
	if err != nil {
		return nil, err
	}

	var completedAt time.Time

	err = tx.QueryRow(
		ctx,
		`
		UPDATE deposits
		SET status = 'COMPLETED',
		    completed_at = NOW()
		WHERE id = $1
		RETURNING completed_at
		`,
		deposit.ID,
	).Scan(&completedAt)
	if err != nil {
		return nil, err
	}

	deposit.Status = StatusCompleted
	deposit.CompletedAt = &completedAt

	return deposit, nil
}

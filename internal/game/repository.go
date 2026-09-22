package game

import (
	"context"
	"fmt"

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

func (r *Repository) GetNextRoundNumber(ctx context.Context) (int64, error) {
	var roundNumber int64

	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(round_number), 0) + 1
		FROM game_rounds
	`).Scan(&roundNumber)

	if err != nil {
		return 0, fmt.Errorf("failed to get next round number: %w", err)
	}

	return roundNumber, nil
}

func (r *Repository) CreateRound(
	ctx context.Context,
	roundNumber int64,
	serverSeedHash string,
	serverSeed string,
	clientSeed string,
	nonce int64,
) (*GameRound, error) {
	var round GameRound

	err := r.db.QueryRow(ctx, `
		INSERT INTO game_rounds (
			round_number,
			server_seed_hash,
			server_seed,
			client_seed,
			nonce,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			round_number,
			server_seed_hash,
			server_seed,
			client_seed,
			nonce,
			crash_point,
			status,
			started_at,
			ended_at,
			created_at
	`,
		roundNumber,
		serverSeedHash,
		serverSeed,
		clientSeed,
		nonce,
		RoundCreated,
	).Scan(
		&round.ID,
		&round.RoundNumber,
		&round.ServerSeedHash,
		&round.ServerSeed,
		&round.ClientSeed,
		&round.Nonce,
		&round.CrashPoint,
		&round.Status,
		&round.StartedAt,
		&round.EndedAt,
		&round.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create game round: %w", err)
	}

	return &round, nil
}

func (r *Repository) GetRoundByID(
	ctx context.Context,
	id int64,
) (*GameRound, error) {
	var round GameRound

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			round_number,
			server_seed_hash,
			server_seed,
			client_seed,
			nonce,
			crash_point,
			status,
			started_at,
			ended_at,
			created_at
		FROM game_rounds
		WHERE id = $1
	`, id).Scan(
		&round.ID,
		&round.RoundNumber,
		&round.ServerSeedHash,
		&round.ServerSeed,
		&round.ClientSeed,
		&round.Nonce,
		&round.CrashPoint,
		&round.Status,
		&round.StartedAt,
		&round.EndedAt,
		&round.CreatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("round %d not found", id)
		}

		return nil, fmt.Errorf("failed to get round: %w", err)
	}

	return &round, nil
}

func (r *Repository) GetCurrentRound(
	ctx context.Context,
) (*GameRound, error) {
	var round GameRound

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			round_number,
			server_seed_hash,
			server_seed,
			client_seed,
			nonce,
			crash_point,
			status,
			started_at,
			ended_at,
			created_at
		FROM game_rounds
		WHERE status IN (
			'CREATED',
			'BETTING_OPEN',
			'BETTING_CLOSED',
			'RUNNING'
		)
		ORDER BY round_number DESC
		LIMIT 1
	`).Scan(
		&round.ID,
		&round.RoundNumber,
		&round.ServerSeedHash,
		&round.ServerSeed,
		&round.ClientSeed,
		&round.Nonce,
		&round.CrashPoint,
		&round.Status,
		&round.StartedAt,
		&round.EndedAt,
		&round.CreatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get current round: %w", err)
	}

	return &round, nil
}

func (r *Repository) UpdateStatus(
	ctx context.Context,
	id int64,
	status RoundStatus,
) error {
	commandTag, err := r.db.Exec(ctx, `
		UPDATE game_rounds
		SET status = $1
		WHERE id = $2
	`,
		status,
		id,
	)

	if err != nil {
		return fmt.Errorf("failed to update round status: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("round %d not found", id)
	}

	return nil
}

func (r *Repository) StartRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := r.db.Exec(ctx, `
		UPDATE game_rounds
		SET
			status = $1,
			started_at = NOW()
		WHERE id = $2
	`,
		RoundRunning,
		id,
	)

	if err != nil {
		return fmt.Errorf("failed to start round: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("round %d not found", id)
	}

	return nil
}

func (r *Repository) CrashRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := r.db.Exec(ctx, `
		UPDATE game_rounds
		SET
			status = $1,
			ended_at = NOW()
		WHERE id = $2
	`,
		RoundCrashed,
		id,
	)

	if err != nil {
		return fmt.Errorf("failed to crash round: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("round %d not found", id)
	}

	return nil
}

func (r *Repository) SettleRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := r.db.Exec(ctx, `
		UPDATE game_rounds
		SET status = $1
		WHERE id = $2
	`,
		RoundSettled,
		id,
	)

	if err != nil {
		return fmt.Errorf("failed to settle round: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("round %d not found", id)
	}

	return nil
}

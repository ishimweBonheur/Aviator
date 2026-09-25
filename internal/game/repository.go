package game

import (
	"aviator/backend/internal/database"
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

func (r *Repository) GetNextRoundNumber(
	ctx context.Context,
) (int64, error) {
	var roundNumber int64

	err := database.Query(ctx, r.db).QueryRow(ctx, `
		SELECT COALESCE(MAX(round_number), 0) + 1
		FROM game_rounds
	`).Scan(&roundNumber)

	if err != nil {
		return 0, fmt.Errorf(
			"failed to get next round number: %w",
			err,
		)
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
	houseEdge decimal.Decimal,
) (*GameRound, error) {
	var round GameRound

	err := database.Query(ctx, r.db).QueryRow(ctx, `
		INSERT INTO game_rounds (
			round_number,
			server_seed_hash,
			server_seed,
			client_seed,
			nonce,
			status,house_edge
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
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
			created_at, betting_opened_at, betting_closes_at, growth_rate, house_edge
	`,
		roundNumber,
		serverSeedHash,
		serverSeed,
		clientSeed,
		nonce,
		RoundCreated, houseEdge.String(),
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
		&round.CreatedAt, &round.BettingOpenedAt, &round.BettingClosesAt, &round.GrowthRate, &round.HouseEdge,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to create game round: %w",
			err,
		)
	}

	return &round, nil
}

func (r *Repository) GetRoundByID(
	ctx context.Context,
	id int64,
) (*GameRound, error) {
	var round GameRound

	err := database.Query(ctx, r.db).QueryRow(ctx, `
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
			created_at, betting_opened_at, betting_closes_at, growth_rate, house_edge
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
		&round.CreatedAt, &round.BettingOpenedAt, &round.BettingClosesAt, &round.GrowthRate, &round.HouseEdge,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf(
				"round %d not found",
				id,
			)
		}

		return nil, fmt.Errorf(
			"failed to get round: %w",
			err,
		)
	}

	return &round, nil
}

func (r *Repository) GetRunningRound(
	ctx context.Context,
) (*GameRound, error) {
	return r.getRoundByStatus(
		ctx,
		RoundRunning,
	)
}

func (r *Repository) GetBettingRound(
	ctx context.Context,
) (*GameRound, error) {
	return r.getRoundByStatus(
		ctx,
		RoundBettingOpen,
	)
}

func (r *Repository) getRoundByStatus(
	ctx context.Context,
	status RoundStatus,
) (*GameRound, error) {
	var round GameRound

	err := database.Query(ctx, r.db).QueryRow(ctx, `
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
			created_at, betting_opened_at, betting_closes_at, growth_rate, house_edge
		FROM game_rounds
		WHERE status = $1
		ORDER BY round_number DESC
		LIMIT 1
	`, status).Scan(
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
		&round.CreatedAt, &round.BettingOpenedAt, &round.BettingClosesAt, &round.GrowthRate, &round.HouseEdge,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"failed to get round with status %s: %w",
			status,
			err,
		)
	}

	return &round, nil
}

func (r *Repository) GetCurrentRound(
	ctx context.Context,
) (*GameRound, error) {
	var round GameRound

	err := database.Query(ctx, r.db).QueryRow(ctx, `
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
			created_at, betting_opened_at, betting_closes_at, growth_rate, house_edge
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
		&round.CreatedAt, &round.BettingOpenedAt, &round.BettingClosesAt, &round.GrowthRate, &round.HouseEdge,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"failed to get current round: %w",
			err,
		)
	}

	return &round, nil
}

func (r *Repository) UpdateStatus(
	ctx context.Context,
	id int64,
	status RoundStatus,
) error {
	commandTag, err := database.Query(ctx, r.db).Exec(ctx, `
		UPDATE game_rounds
		SET status = $1
		WHERE id = $2
	`,
		status,
		id,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to update round status: %w",
			err,
		)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"round %d not found",
			id,
		)
	}

	return nil
}

func (r *Repository) StartRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := database.Query(ctx, r.db).Exec(ctx, `
		UPDATE game_rounds
		SET
			status = $1,
			started_at = NOW()
		WHERE id = $2 AND status='BETTING_CLOSED'
	`,
		RoundRunning,
		id,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to start round: %w",
			err,
		)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"round %d not found",
			id,
		)
	}

	return nil
}

func (r *Repository) CrashRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := database.Query(ctx, r.db).Exec(ctx, `
		UPDATE game_rounds
		SET
			status = $1,
			ended_at = NOW()
		WHERE id = $2 AND status='RUNNING'
	`,
		RoundCrashed,
		id,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to crash round: %w",
			err,
		)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"round %d not found",
			id,
		)
	}

	return nil
}

func (r *Repository) SettleRound(
	ctx context.Context,
	id int64,
) error {
	commandTag, err := database.Query(ctx, r.db).Exec(ctx, `
		UPDATE game_rounds
		SET status = $1
		WHERE id = $2
	`,
		RoundSettled,
		id,
	)

	if err != nil {
		return fmt.Errorf(
			"failed to settle round: %w",
			err,
		)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf(
			"round %d not found",
			id,
		)
	}

	return nil
}

func (r *Repository) SetCrashPoint(
	ctx context.Context,
	id int64,
	crashPoint decimal.Decimal,
) (*GameRound, error) {
	var round GameRound
	var crashPointString string

	err := database.Query(ctx, r.db).QueryRow(
		ctx,
		`
		UPDATE game_rounds
		SET crash_point = $2
		WHERE id = $1
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
			created_at, betting_opened_at, betting_closes_at, growth_rate, house_edge
		`,
		id,
		crashPoint.String(),
	).Scan(
		&round.ID,
		&round.RoundNumber,
		&round.ServerSeedHash,
		&round.ServerSeed,
		&round.ClientSeed,
		&round.Nonce,
		&crashPointString,
		&round.Status,
		&round.StartedAt,
		&round.EndedAt,
		&round.CreatedAt, &round.BettingOpenedAt, &round.BettingClosesAt, &round.GrowthRate, &round.HouseEdge,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to set crash point: %w",
			err,
		)
	}

	parsedCrashPoint, err := decimal.NewFromString(
		crashPointString,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid crash point in database: %w",
			err,
		)
	}

	round.CrashPoint = &parsedCrashPoint

	return &round, nil
}

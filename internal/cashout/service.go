package cashout

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/risk"
	"aviator/backend/internal/wallet"
	"context"
	"fmt"
	"github.com/shopspring/decimal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	publisher         realtime.Publisher
	limits            risk.Limits
	db                *pgxpool.Pool
	repository        *Repository
	walletService     *wallet.Service
	multiplierService *multiplier.Service
}

func NewService(
	db *pgxpool.Pool,
	repository *Repository,
	walletService *wallet.Service,
	multiplierService *multiplier.Service,
) *Service {
	return &Service{
		limits:            risk.Default(),
		db:                db,
		repository:        repository,
		walletService:     walletService,
		multiplierService: multiplierService,
	}
}

func (s *Service) CashOut(
	ctx context.Context,
	userID int64,
	betID int64,
) (*CashoutResponse, error) {

	if userID <= 0 {
		return nil, fmt.Errorf("invalid user ID")
	}

	if betID <= 0 {
		return nil, fmt.Errorf("invalid bet ID")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to begin cashout transaction: %w",
			err,
		)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var roundID int64
	if err := tx.QueryRow(ctx, "SELECT round_id FROM bets WHERE id=$1 AND user_id=$2", betID, userID).Scan(&roundID); err != nil {
		return nil, fmt.Errorf("bet not found")
	}
	var roundStatus string
	var started *time.Time
	var point *decimal.Decimal
	var rate float64
	if err := tx.QueryRow(ctx, "SELECT status, started_at, crash_point, growth_rate FROM game_rounds WHERE id=$1 FOR SHARE", roundID).Scan(&roundStatus, &started, &point, &rate); err != nil {
		return nil, fmt.Errorf("failed to load round: %w", err)
	}
	bet, err := s.repository.GetActiveBetForUpdate(
		ctx,
		tx,
		betID,
		userID,
	)
	if err != nil {
		return nil, err
	}

	if bet.Status != "ACTIVE" {
		return nil, fmt.Errorf(
			"bet is no longer active",
		)
	}

	if roundStatus != "RUNNING" {
		return nil, fmt.Errorf(
			"cashout is only allowed while the round is running",
		)
	}

	if started == nil || point == nil {
		return nil, fmt.Errorf("invalid round timing")
	}
	clock := multiplier.Clock{Rate: rate}
	// Evaluate at processing time after acquiring every contended money lock.
	var lockedUser int64
	if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&lockedUser); err != nil {
		return nil, fmt.Errorf("failed to lock wallet: %w", err)
	}
	now := time.Now()
	currentMultiplier, err := clock.CashoutMultiplier(*started, now, *point)
	if err != nil {
		return nil, err
	}
	payout := bet.Amount.Mul(currentMultiplier).Round(2)

	if payout.GreaterThan(s.limits.MaxPayout) {
		payout = s.limits.MaxPayout
	}
	reference := fmt.Sprintf(
		"WIN-BET-%d",
		bet.ID,
	)

	err = s.walletService.CreditTx(
		ctx,
		tx,
		userID,
		payout,
		"WIN",
		reference,
	)
	if err != nil {
		return nil, err
	}

	err = s.repository.CashOut(
		ctx,
		tx,
		bet.ID,
		currentMultiplier,
		payout,
	)
	if err != nil {
		return nil, err
	}

	var balance decimal.Decimal
	if err := tx.QueryRow(ctx, "SELECT balance FROM users WHERE id=$1", userID).Scan(&balance); err != nil {
		return nil, fmt.Errorf("failed to read balance: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"failed to commit cashout: %w",
			err,
		)
	}

	if s.publisher != nil {
		var number int64
		if err := s.db.QueryRow(ctx, "SELECT round_number FROM game_rounds WHERE id=$1", bet.RoundID).Scan(&number); err == nil {
			s.publisher.Publish(ctx, realtime.Event{Type: realtime.EventBetCashedOut, RoundID: bet.RoundID, RoundNumber: number, BetID: bet.ID, Multiplier: currentMultiplier.StringFixed(2), Payout: payout.StringFixed(2)})
		}
	}
	return &CashoutResponse{
		BetID:            bet.ID,
		Multiplier:       currentMultiplier,
		BetAmount:        bet.Amount,
		Payout:           payout,
		RemainingBalance: balance,
	}, nil
}

func (s *Service) SetLimits(limits risk.Limits) { s.limits = limits }

func (s *Service) SetPublisher(p realtime.Publisher) { s.publisher = p }

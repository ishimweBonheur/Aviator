package betting

import (
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/risk"
	"aviator/backend/internal/wallet"
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const MinimumBet = 50

type Service struct {
	publisher     realtime.Publisher
	limits        risk.Limits
	db            *pgxpool.Pool
	repository    *Repository
	walletService *wallet.Service
}

func NewService(
	db *pgxpool.Pool,
	repository *Repository,
	walletService *wallet.Service,
) *Service {
	return &Service{
		limits:        risk.Default(),
		db:            db,
		repository:    repository,
		walletService: walletService,
	}
}

func (s *Service) PlaceBet(
	ctx context.Context,
	userID int64,
	roundID int64,
	betNumber int16,
	amount decimal.Decimal,
	targets ...decimal.Decimal,
) (*Bet, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	bet, err := s.placeBetTx(ctx, tx, userID, roundID, betNumber, amount, targets...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.publish(ctx, realtime.EventBetPlaced, roundID, bet.ID, amount.StringFixed(2))
	return bet, nil
}

func (s *Service) placeBetTx(ctx context.Context, tx pgx.Tx, userID, roundID int64, betNumber int16, amount decimal.Decimal, targets ...decimal.Decimal) (*Bet, error) {
	if len(targets) > 1 {
		return nil, fmt.Errorf("invalid auto cashout target")
	}
	if len(targets) == 1 {
		if err := ValidateTarget(targets[0]); err != nil {
			return nil, err
		}
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user ID")
	}

	if roundID <= 0 {
		return nil, fmt.Errorf("invalid round ID")
	}

	if betNumber != 1 && betNumber != 2 {
		return nil, fmt.Errorf("bet number must be 1 or 2")
	}

	if err := risk.Amount("bet", amount, s.limits.MinBet, s.limits.MaxBet); err != nil {
		return nil, err
	}
	minimum := decimal.NewFromInt(MinimumBet)

	if amount.LessThan(minimum) {
		return nil, fmt.Errorf(
			"minimum bet is %s RWF",
			minimum.StringFixed(2),
		)
	}

	if !amount.IsPositive() {
		return nil, fmt.Errorf("bet amount must be greater than zero")
	}

	roundStatus, err := s.repository.GetRoundStatus(
		ctx,
		tx,
		roundID,
	)
	if err != nil {
		return nil, err
	}

	if roundStatus != "BETTING_OPEN" {
		return nil, fmt.Errorf(
			"betting is not open for this round",
		)
	}

	var accountStatus string
	if err := tx.QueryRow(ctx, "SELECT status FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&accountStatus); err != nil {
		return nil, err
	}
	if accountStatus != "ACTIVE" {
		return nil, fmt.Errorf("account is not active")
	}
	count, err := s.repository.CountUserBetsForRound(
		ctx,
		tx,
		roundID,
		userID,
	)
	if err != nil {
		return nil, err
	}

	if count >= 2 {
		return nil, fmt.Errorf(
			"user already has the maximum of 2 bets for this round",
		)
	}

	reference := fmt.Sprintf(
		"BET-%d-%d-%d-%s",
		roundID,
		userID,
		betNumber, rand.Text(),
	)

	err = s.walletService.DebitTx(
		ctx,
		tx,
		userID,
		amount,
		"BET",
		reference,
	)
	if err != nil {
		return nil, err
	}

	roundStatus, err = s.repository.GetRoundStatus(ctx, tx, roundID)
	if err != nil {
		return nil, err
	}
	if roundStatus != "BETTING_OPEN" {
		return nil, fmt.Errorf("betting is closed")
	}
	bet, err := s.repository.CreateBet(
		ctx,
		tx,
		roundID,
		userID,
		betNumber,
		amount,
	)
	if err != nil {
		return nil, err
	}

	if len(targets) == 1 {
		if _, err := tx.Exec(ctx, "UPDATE bets SET auto_cashout_multiplier=$2 WHERE id=$1", bet.ID, targets[0].StringFixed(2)); err != nil {
			return nil, err
		}
	}
	return bet, nil
}

func (s *Service) SetLimits(limits risk.Limits) { s.limits = limits }

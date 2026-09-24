package betting

import (
	"aviator/backend/internal/wallet"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const MinimumBet = 50

type Service struct {
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
) (*Bet, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user ID")
	}

	if roundID <= 0 {
		return nil, fmt.Errorf("invalid round ID")
	}

	if betNumber != 1 && betNumber != 2 {
		return nil, fmt.Errorf("bet number must be 1 or 2")
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to begin betting transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

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
		"BET-%d-%d-%d",
		roundID,
		userID,
		betNumber,
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

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"failed to commit bet: %w",
			err,
		)
	}

	return bet, nil
}

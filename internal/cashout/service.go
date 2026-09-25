package cashout

import (
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/wallet"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
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

	roundStatus, err := s.repository.GetRoundStatus(
		ctx,
		tx,
		bet.RoundID,
	)
	if err != nil {
		return nil, err
	}

	if roundStatus != "RUNNING" {
		return nil, fmt.Errorf(
			"cashout is only allowed while the round is running",
		)
	}

	currentMultiplier, err := s.multiplierService.Current(
		bet.RoundID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"multiplier is not currently running: %w",
			err,
		)
	}

	payout := bet.Amount.Mul(currentMultiplier).Round(2)

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

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"failed to commit cashout: %w",
			err,
		)
	}

	balance, err := s.walletService.GetBalance(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf(
			"cashout completed but failed to read balance: %w",
			err,
		)
	}

	return &CashoutResponse{
		BetID:            bet.ID,
		Multiplier:       currentMultiplier,
		BetAmount:        bet.Amount,
		Payout:           payout,
		RemainingBalance: balance,
	}, nil
}

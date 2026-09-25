package withdrawal

import (
	"aviator/backend/internal/risk"
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

type Service struct {
	limits     risk.Limits
	repository *Repository
}

func NewService(
	repository *Repository,
) *Service {
	return &Service{
		limits:     risk.Default(),
		repository: repository,
	}
}

func (s *Service) Create(
	ctx context.Context,
	userID int64,
	req CreateRequest,
) (*CreateResult, error) {
	amount, err := decimal.NewFromString(
		strings.TrimSpace(req.Amount),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid withdrawal amount",
		)
	}

	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf(
			"withdrawal amount must be greater than zero",
		)
	}

	if !amount.Equal(amount.Round(2)) {
		return nil, fmt.Errorf(
			"withdrawal amount cannot have more than 2 decimal places",
		)
	}

	if err := risk.Amount("withdrawal", amount, s.limits.MinWithdrawal, s.limits.MaxWithdrawal); err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(
		req.Provider,
	)

	if provider == "" {
		return nil, fmt.Errorf(
			"provider is required",
		)
	}

	return s.repository.CreateAndReserve(
		ctx,
		userID,
		amount,
		provider,
	)
}

func (s *Service) List(
	ctx context.Context,
	userID int64,
) ([]Withdrawal, error) {
	return s.repository.ListByUser(
		ctx,
		userID,
		50,
	)
}

func (s *Service) Complete(
	ctx context.Context,
	withdrawalID int64,
	providerReference string,
) (*Withdrawal, error) {
	return s.repository.Complete(
		ctx,
		withdrawalID,
		providerReference,
	)
}

func (s *Service) FailAndRefund(
	ctx context.Context,
	withdrawalID int64,
) (*Withdrawal, error) {
	return s.repository.FailAndRefund(
		ctx,
		withdrawalID,
	)
}

func (s *Service) SetLimits(limits risk.Limits) { s.limits = limits }

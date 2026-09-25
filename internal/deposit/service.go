package deposit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

type Service struct {
	repository *Repository
}

func NewService(
	repository *Repository,
) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) Create(
	ctx context.Context,
	userID int64,
	req CreateRequest,
) (*Deposit, error) {
	amount, err := decimal.NewFromString(
		strings.TrimSpace(req.Amount),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid deposit amount",
		)
	}

	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf(
			"deposit amount must be greater than zero",
		)
	}

	if !amount.Equal(amount.Round(2)) {
		return nil, fmt.Errorf(
			"deposit amount cannot have more than 2 decimal places",
		)
	}

	provider := strings.ToUpper(
		strings.TrimSpace(req.Provider),
	)

	switch Provider(provider) {
	case ProviderSandbox:
	case ProviderMTNMomo:
	default:
		return nil, fmt.Errorf(
			"unsupported provider: use SANDBOX or MTN_MOMO",
		)
	}

	providerReference, err := generateProviderReference(
		provider,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"generate provider reference: %w",
			err,
		)
	}

	deposit, err := s.repository.Create(
		ctx,
		userID,
		amount,
		provider,
		providerReference,
	)
	if err != nil {
		return nil, err
	}

	// Development/testing provider.
	//
	// SANDBOX deposits are automatically confirmed so that
	// the complete wallet-credit flow can be tested without
	// connecting to an external payment provider.
	if Provider(provider) == ProviderSandbox {
		completedDeposit, err :=
			s.repository.CompleteByProviderReference(
				ctx,
				provider,
				providerReference,
			)

		if err != nil {
			return nil, fmt.Errorf(
				"complete sandbox deposit: %w",
				err,
			)
		}

		return completedDeposit, nil
	}

	// MTN_MOMO remains pending until the real payment provider
	// confirms payment through its callback/webhook.
	return deposit, nil
}

func (s *Service) List(
	ctx context.Context,
	userID int64,
) ([]Deposit, error) {
	return s.repository.ListByUser(
		ctx,
		userID,
		50,
	)
}

func (s *Service) CompleteByProviderReference(
	ctx context.Context,
	provider string,
	providerReference string,
) (*Deposit, error) {
	return s.repository.CompleteByProviderReference(
		ctx,
		provider,
		providerReference,
	)
}

func generateProviderReference(
	provider string,
) (string, error) {
	bytes := make([]byte, 12)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"%s-%s",
		provider,
		hex.EncodeToString(bytes),
	), nil
}

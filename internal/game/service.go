package game

import (
	"aviator/backend/internal/fairness"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

type Service struct {
	repository      *Repository
	fairnessService *fairness.Service
}

func NewService(
	repository *Repository,
	fairnessService *fairness.Service,
) *Service {
	return &Service{
		repository:      repository,
		fairnessService: fairnessService,
	}
}

func (s *Service) CreateRound(ctx context.Context) (*GameRound, error) {
	// There can be only one upcoming betting round.
	bettingRound, err := s.repository.GetBettingRound(ctx)
	if err != nil {
		return nil, err
	}

	if bettingRound != nil {
		return nil, fmt.Errorf(
			"cannot create a new round while round %d is already open for betting",
			bettingRound.RoundNumber,
		)
	}

	roundNumber, err := s.repository.GetNextRoundNumber(ctx)
	if err != nil {
		return nil, err
	}

	serverSeed, err := generateSeed()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to generate server seed: %w",
			err,
		)
	}

	serverSeedHash := hashSeed(serverSeed)

	clientSeed := "aviator-default"

	round, err := s.repository.CreateRound(
		ctx,
		roundNumber,
		serverSeedHash,
		serverSeed,
		clientSeed,
		roundNumber, s.fairnessService.HouseEdge(),
	)

	if err != nil {
		return nil, err
	}

	return round, nil
}

func (s *Service) OpenBetting(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundCreated {
		return nil, errors.New(
			"round must be in CREATED state before betting can open",
		)
	}

	if err := s.repository.UpdateStatus(
		ctx,
		roundID,
		RoundBettingOpen,
	); err != nil {
		return nil, err
	}

	return s.repository.GetRoundByID(ctx, roundID)
}

func (s *Service) CloseBetting(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundBettingOpen {
		return nil, errors.New(
			"betting can only be closed when round is BETTING_OPEN",
		)
	}

	if err := s.repository.UpdateStatus(
		ctx,
		roundID,
		RoundBettingClosed,
	); err != nil {
		return nil, err
	}

	return s.repository.GetRoundByID(ctx, roundID)
}

func (s *Service) StartRound(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundBettingClosed {
		return nil, errors.New(
			"round can only start when betting is closed",
		)
	}

	if err := s.repository.StartRound(ctx, roundID); err != nil {
		return nil, err
	}

	return s.repository.GetRoundByID(ctx, roundID)
}

func (s *Service) CrashRound(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundRunning {
		return nil, errors.New(
			"round can only crash when it is RUNNING",
		)
	}

	if err := s.repository.CrashRound(ctx, roundID); err != nil {
		return nil, err
	}

	return s.repository.GetRoundByID(ctx, roundID)
}

func (s *Service) SettleRound(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundCrashed {
		return nil, errors.New(
			"round can only be settled after it has crashed",
		)
	}

	if err := s.repository.SettleRound(ctx, roundID); err != nil {
		return nil, err
	}

	return s.repository.GetRoundByID(ctx, roundID)
}

func (s *Service) GenerateCrashPoint(
	ctx context.Context,
	roundID int64,
) (*GameRound, error) {
	round, err := s.repository.GetRoundByID(ctx, roundID)
	if err != nil {
		return nil, err
	}

	if round.Status != RoundBettingClosed {
		return nil, fmt.Errorf(
			"cannot generate crash point for round in status %s",
			round.Status,
		)
	}

	if round.ServerSeed == nil {
		return nil, fmt.Errorf("server seed is missing")
	}

	fairnessService := s.fairnessService
	if round.HouseEdge != nil {
		fairnessService = fairness.NewService(fairness.Config{HouseEdge: *round.HouseEdge})
	}
	result, err := fairnessService.GenerateCrashPoint(
		*round.ServerSeed,
		round.ClientSeed,
		round.Nonce,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to generate crash point: %w",
			err,
		)
	}

	round, err = s.repository.SetCrashPoint(
		ctx,
		round.ID,
		result.CrashPoint,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to save crash point: %w",
			err,
		)
	}

	return round, nil
}

func generateSeed() (string, error) {
	bytes := make([]byte, 32)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

func hashSeed(seed string) string {
	hash := sha256.Sum256([]byte(seed))

	return hex.EncodeToString(hash[:])
}

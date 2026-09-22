package fairness

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

const (
	// 52 bits gives us a large deterministic random space
	// while keeping the calculation easy to verify.
	randomBits = 52
)

type Service struct {
	config Config
}

func NewService(config Config) *Service {
	return &Service{
		config: config,
	}
}

// GenerateCrashPoint calculates the crash multiplier from:
//
//	serverSeed
//	clientSeed
//	nonce
//
// The result is deterministic. The same inputs will always
// produce the same crash point.
func (s *Service) GenerateCrashPoint(
	serverSeed string,
	clientSeed string,
	nonce int64,
) (*CrashResult, error) {

	if serverSeed == "" {
		return nil, fmt.Errorf("server seed is required")
	}

	if clientSeed == "" {
		return nil, fmt.Errorf("client seed is required")
	}

	if nonce < 0 {
		return nil, fmt.Errorf("nonce cannot be negative")
	}

	if s.config.HouseEdge.IsNegative() ||
		s.config.HouseEdge.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return nil, fmt.Errorf("house edge must be between 0 and 1")
	}

	// Canonical message used for verification.
	message := fmt.Sprintf("%s:%d", clientSeed, nonce)

	// HMAC-SHA256 using the server seed as the secret.
	mac := hmac.New(sha256.New, []byte(serverSeed))
	_, _ = mac.Write([]byte(message))

	hashBytes := mac.Sum(nil)
	hashString := hex.EncodeToString(hashBytes)

	// Extract the first 52 bits from the HMAC.
	randomValue := extract52Bits(hashBytes)

	// 2^52.
	maxValue := new(big.Int).Lsh(big.NewInt(1), randomBits)

	// If the random value is exactly 2^52 - 1,
	// denominator becomes 1 and the result can become
	// extremely large. That is valid and deterministic.
	numerator := new(big.Int).Set(maxValue)

	denominator := new(big.Int).Sub(
		maxValue,
		randomValue,
	)

	// Apply the house-edge factor.
	//
	// crash = (1 - houseEdge) * 2^52 / (2^52 - random)
	//
	// We perform the main calculation with Decimal so
	// there is no float64 arithmetic involved.
	numeratorDecimal := decimal.NewFromBigInt(numerator, 0)
	denominatorDecimal := decimal.NewFromBigInt(denominator, 0)

	randomMultiplier := numeratorDecimal.Div(denominatorDecimal)

	edgeMultiplier := decimal.NewFromInt(1).Sub(s.config.HouseEdge)

	crashPoint := randomMultiplier.Mul(edgeMultiplier)

	// A crash game cannot crash below 1.00x.
	minimum := decimal.NewFromInt(1)

	if crashPoint.LessThan(minimum) {
		crashPoint = minimum
	}

	// The game uses four decimal places internally.
	crashPoint = crashPoint.Round(4)

	return &CrashResult{
		CrashPoint: crashPoint,
		Hash:       hashString,
	}, nil
}

// extract52Bits takes the first 52 bits from the SHA-256 output.
func extract52Bits(hash []byte) *big.Int {
	if len(hash) < 7 {
		return big.NewInt(0)
	}

	// First 7 bytes = 56 bits.
	//
	// We keep only the lower 52 bits.
	value := new(big.Int).SetBytes(hash[:7])

	mask := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), randomBits),
		big.NewInt(1),
	)

	return new(big.Int).And(value, mask)
}

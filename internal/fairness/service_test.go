package fairness

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/shopspring/decimal"
)

func newTestService() *Service {
	return NewService(Config{
		HouseEdge: decimal.Zero,
	})
}

func TestGenerateCrashPointIsDeterministic(t *testing.T) {
	service := newTestService()

	serverSeed := "test-server-seed"
	clientSeed := "aviator-test"
	nonce := int64(1)

	first, err := service.GenerateCrashPoint(
		serverSeed,
		clientSeed,
		nonce,
	)
	if err != nil {
		t.Fatalf("first calculation failed: %v", err)
	}

	second, err := service.GenerateCrashPoint(
		serverSeed,
		clientSeed,
		nonce,
	)
	if err != nil {
		t.Fatalf("second calculation failed: %v", err)
	}

	if !first.CrashPoint.Equal(second.CrashPoint) {
		t.Fatalf(
			"crash points are not deterministic: first=%s second=%s",
			first.CrashPoint,
			second.CrashPoint,
		)
	}

	if first.Hash != second.Hash {
		t.Fatalf(
			"hashes are not deterministic: first=%s second=%s",
			first.Hash,
			second.Hash,
		)
	}
}

func TestDifferentNonceProducesDifferentResult(t *testing.T) {
	service := newTestService()

	first, err := service.GenerateCrashPoint(
		"test-server-seed",
		"aviator-test",
		1,
	)
	if err != nil {
		t.Fatalf("first calculation failed: %v", err)
	}

	second, err := service.GenerateCrashPoint(
		"test-server-seed",
		"aviator-test",
		2,
	)
	if err != nil {
		t.Fatalf("second calculation failed: %v", err)
	}

	if first.Hash == second.Hash {
		t.Fatal("different nonces produced the same hash")
	}
}

func TestCrashPointNeverBelowOne(t *testing.T) {
	service := newTestService()

	seeds := []string{
		"seed-1",
		"seed-2",
		"seed-3",
		"seed-4",
		"seed-5",
	}

	for i, seed := range seeds {
		result, err := service.GenerateCrashPoint(
			seed,
			"aviator-test",
			int64(i+1),
		)
		if err != nil {
			t.Fatalf(
				"failed for seed %s: %v",
				seed,
				err,
			)
		}

		if result.CrashPoint.LessThan(decimal.NewFromInt(1)) {
			t.Fatalf(
				"crash point below 1.00x: %s",
				result.CrashPoint,
			)
		}
	}
}

func TestServerSeedHashCanBeVerified(t *testing.T) {
	serverSeed := "test-server-seed"

	hash := sha256.Sum256([]byte(serverSeed))
	serverSeedHash := hex.EncodeToString(hash[:])

	expectedLength := 64

	if len(serverSeedHash) != expectedLength {
		t.Fatalf(
			"expected SHA-256 hash length %d, got %d",
			expectedLength,
			len(serverSeedHash),
		)
	}

	if serverSeedHash == "" {
		t.Fatal("server seed hash is empty")
	}
}

func TestInvalidServerSeed(t *testing.T) {
	service := newTestService()

	_, err := service.GenerateCrashPoint(
		"",
		"aviator-test",
		1,
	)

	if err == nil {
		t.Fatal("expected error for empty server seed")
	}
}

func TestInvalidClientSeed(t *testing.T) {
	service := newTestService()

	_, err := service.GenerateCrashPoint(
		"test-server-seed",
		"",
		1,
	)

	if err == nil {
		t.Fatal("expected error for empty client seed")
	}
}

func TestInvalidNonce(t *testing.T) {
	service := newTestService()

	_, err := service.GenerateCrashPoint(
		"test-server-seed",
		"aviator-test",
		-1,
	)

	if err == nil {
		t.Fatal("expected error for negative nonce")
	}
}

func TestInvalidHouseEdge(t *testing.T) {
	service := NewService(Config{
		HouseEdge: decimal.NewFromInt(1),
	})

	_, err := service.GenerateCrashPoint(
		"test-server-seed",
		"aviator-test",
		1,
	)

	if err == nil {
		t.Fatal("expected error for invalid house edge")
	}
}

package multiplier

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCalculateAtStart(t *testing.T) {
	start := time.Date(
		2026,
		9,
		24,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	got := Calculate(start, start)

	if !got.Equal(decimalFromString("1.0000")) {
		t.Fatalf("expected 1.0000, got %s", got.String())
	}
}

func TestCalculateIncreasesOverTime(t *testing.T) {
	start := time.Date(
		2026,
		9,
		24,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	afterOneSecond := start.Add(1 * time.Second)
	afterTenSeconds := start.Add(10 * time.Second)

	m1 := Calculate(start, afterOneSecond)
	m10 := Calculate(start, afterTenSeconds)

	if !m10.GreaterThan(m1) {
		t.Fatalf(
			"expected multiplier to increase: 1s=%s, 10s=%s",
			m1.String(),
			m10.String(),
		)
	}
}

func TestCalculateIsDeterministic(t *testing.T) {
	start := time.Date(
		2026,
		9,
		24,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	now := start.Add(5 * time.Second)

	first := Calculate(start, now)
	second := Calculate(start, now)

	if !first.Equal(second) {
		t.Fatalf(
			"expected deterministic result: first=%s second=%s",
			first.String(),
			second.String(),
		)
	}
}

func TestCalculateNeverBelowOne(t *testing.T) {
	start := time.Date(
		2026,
		9,
		24,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	beforeStart := start.Add(-5 * time.Second)

	got := Calculate(start, beforeStart)

	if got.LessThan(decimalFromString("1.0000")) {
		t.Fatalf("expected multiplier >= 1.0000, got %s", got.String())
	}
}

func decimalFromString(value string) decimal.Decimal {
	d, err := decimal.NewFromString(value)
	if err != nil {
		panic(err)
	}

	return d
}

func TestServiceOnlyAdvancesFromEngineAndStops(t *testing.T) {
	s := NewService()
	s.Start(1, time.Now().Add(-time.Hour))
	if got, err := s.Current(1); err != nil || !got.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("read advanced clock: %s %v", got, err)
	}
	if err := s.Set(1, decimal.RequireFromString("2.47")); err != nil {
		t.Fatal(err)
	}
	s.Stop(1)
	if _, err := s.Current(1); err == nil {
		t.Fatal("stopped round returned live multiplier")
	}
	if err := s.Set(1, decimal.NewFromInt(3)); err == nil {
		t.Fatal("stopped round accepted tick")
	}
	s.StopAll()
	if _, err := s.Current(1); err == nil {
		t.Fatal("leadership shutdown retained state")
	}
}

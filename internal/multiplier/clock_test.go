package multiplier

import (
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestCrashBoundary(t *testing.T) {
	start := time.Unix(1700000000, 0)
	clock := Clock{Rate: 0.08}
	crash, err := clock.CrashAt(start, decimal.NewFromInt(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		now     time.Time
		allowed bool
	}{{crash.Add(-time.Nanosecond), true}, {crash, false}, {crash.Add(time.Nanosecond), false}} {
		_, err := clock.CashoutMultiplier(start, test.now, decimal.NewFromInt(2))
		if (err == nil) != test.allowed {
			t.Fatal("incorrect boundary")
		}
	}
	instant, err := clock.CrashAt(start, decimal.NewFromInt(1))
	if err != nil || !instant.Equal(start) {
		t.Fatal("1x must crash immediately")
	}
	if _, err := (Clock{Rate: 0}).CrashAt(start, decimal.NewFromInt(2)); err == nil {
		t.Fatal("invalid rate accepted")
	}
	if !clock.Calculate(start, start.Add(100000*time.Hour)).Equal(maxMultiplierDecimal) {
		t.Fatal("overflow guard failed")
	}
}

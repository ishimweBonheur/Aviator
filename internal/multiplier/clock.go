package multiplier

import (
	"fmt"
	"github.com/shopspring/decimal"
	"math"
	"time"
)

// Clock is immutable. Persist Rate per round so configuration changes on restart
// cannot move the crash boundary of an existing flight.
type Clock struct{ Rate float64 }

func (c Clock) CrashAt(start time.Time, point decimal.Decimal) (time.Time, error) {
	p, _ := point.Float64()
	if start.IsZero() || p < 1 || math.IsInf(p, 0) || math.IsNaN(p) || c.Rate <= 0 || math.IsInf(c.Rate, 0) || math.IsNaN(c.Rate) {
		return time.Time{}, fmt.Errorf("invalid round timing")
	}
	ns := math.Log(p) / c.Rate * float64(time.Second)
	if ns >= float64(math.MaxInt64) {
		return time.Time{}, fmt.Errorf("crash duration out of range")
	}
	return start.Add(time.Duration(ns)), nil
}
func (c Clock) Calculate(start, now time.Time) decimal.Decimal {
	if start.IsZero() || now.IsZero() || !now.After(start) {
		return oneMultiplier
	}
	exponent := c.Rate * now.Sub(start).Seconds()
	if exponent >= math.Log(maxMultiplier) {
		return maxMultiplierDecimal
	}
	if math.IsNaN(exponent) || exponent < 0 {
		return oneMultiplier
	}
	return decimal.NewFromFloat(math.Exp(exponent)).Round(2)
}

// CashoutMultiplier evaluates the strict authoritative boundary, independently
// of whether the display ticker has observed the crash yet.
func (c Clock) CashoutMultiplier(start, now time.Time, point decimal.Decimal) (decimal.Decimal, error) {
	crashAt, err := c.CrashAt(start, point)
	if err != nil {
		return decimal.Zero, err
	}
	if now.Before(start) || !now.Before(crashAt) {
		return decimal.Zero, fmt.Errorf("round has crashed or has not started")
	}
	current := c.Calculate(start, now)
	if current.GreaterThan(point) {
		current = point
	}
	return current, nil
}

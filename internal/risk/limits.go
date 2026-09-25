package risk

import (
	"fmt"
	"github.com/shopspring/decimal"
)

type Limits struct{ MinBet, MaxBet, MaxPayout, MinDeposit, MaxDeposit, MinWithdrawal, MaxWithdrawal decimal.Decimal }

func Default() Limits {
	return Limits{decimal.NewFromInt(50), decimal.NewFromInt(1000000), decimal.NewFromInt(100000000), decimal.NewFromInt(1), decimal.NewFromInt(10000000), decimal.NewFromInt(1), decimal.NewFromInt(10000000)}
}
func Amount(name string, value, min, max decimal.Decimal) error {
	if !value.Equal(value.Round(2)) {
		return fmt.Errorf("%s must have at most two decimal places", name)
	}
	if value.LessThan(min) || value.GreaterThan(max) {
		return fmt.Errorf("%s must be between %s and %s", name, min.StringFixed(2), max.StringFixed(2))
	}
	return nil
}

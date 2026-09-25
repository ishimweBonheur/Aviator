package multiplier

import (
	"time"

	"github.com/shopspring/decimal"
)

type State struct {
	RoundID    int64
	Multiplier decimal.Decimal
	Running    bool
	StartedAt  time.Time
}

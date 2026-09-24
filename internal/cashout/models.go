package cashout

import "github.com/shopspring/decimal"

type CashoutResponse struct {
	BetID            int64           `json:"bet_id"`
	Multiplier       decimal.Decimal `json:"multiplier"`
	BetAmount        decimal.Decimal `json:"bet_amount"`
	Payout           decimal.Decimal `json:"payout"`
	RemainingBalance decimal.Decimal `json:"remaining_balance"`
}

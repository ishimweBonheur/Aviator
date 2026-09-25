package cashout

import (
	"encoding/json"
	"github.com/shopspring/decimal"
)

type CashoutResponse struct {
	BetID            int64           `json:"bet_id"`
	Multiplier       decimal.Decimal `json:"multiplier"`
	BetAmount        decimal.Decimal `json:"bet_amount"`
	Payout           decimal.Decimal `json:"payout"`
	RemainingBalance decimal.Decimal `json:"remaining_balance"`
}

func (r CashoutResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"bet_id": r.BetID, "multiplier": r.Multiplier.StringFixed(2), "bet_amount": r.BetAmount.StringFixed(2), "payout": r.Payout.StringFixed(2), "remaining_balance": r.RemainingBalance.StringFixed(2)})
}

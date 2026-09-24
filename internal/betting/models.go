package betting

import "github.com/shopspring/decimal"

type BetStatus string

const (
	BetActive    BetStatus = "ACTIVE"
	BetCashedOut BetStatus = "CASHED_OUT"
	BetLost      BetStatus = "LOST"
	BetCancelled BetStatus = "CANCELLED"
)

type Bet struct {
	ID                int64            `json:"id"`
	RoundID           int64            `json:"round_id"`
	UserID            int64            `json:"user_id"`
	BetNumber         int16            `json:"bet_number"`
	Amount            decimal.Decimal  `json:"amount"`
	Status            BetStatus        `json:"status"`
	CashoutMultiplier *decimal.Decimal `json:"cashout_multiplier,omitempty"`
	Payout            decimal.Decimal  `json:"payout"`
	PlacedAt          string           `json:"placed_at"`
	CashedOutAt      *string          `json:"cashed_out_at,omitempty"`
	CreatedAt         string           `json:"created_at"`
}

type PlaceBetRequest struct {
	RoundID   int64  `json:"round_id"`
	BetNumber int16  `json:"bet_number"`
	Amount    string `json:"amount"`
}
package history

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/risk"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"net/http"
	"time"
)

type Handler struct {
	db     *pgxpool.Pool
	limits risk.Limits
}

func NewHandler(db *pgxpool.Pool) *Handler { return &Handler{db: db, limits: risk.Default()} }
func JSON(w http.ResponseWriter, v any)    { httpapi.JSON(w, 200, v) }

// Bets lists the authenticated player's persisted bets.
// @Summary List own bets (latest 200)
// @Tags betting
// @Security BearerAuth
// @Produce json
// @Success 200 {array} PlayerBet
// @Router /api/bets [get]
func (h *Handler) Bets(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.Error(w, "unauthorized", 401)
		return
	}
	rows, err := h.db.Query(r.Context(), "SELECT b.id,b.round_id,b.user_id,b.bet_number,b.amount,b.status,b.cashout_multiplier,b.payout,g.round_number,g.status,g.started_at,g.crash_point,g.growth_rate,g.betting_closes_at,b.auto_cashout_multiplier FROM bets b JOIN game_rounds g ON g.id=b.round_id WHERE b.user_id=$1 ORDER BY b.id DESC LIMIT 200", uid)
	if err != nil {
		httpapi.Error(w, "failed to load bets", 500)
		return
	}
	defer rows.Close()
	result := []PlayerBet{}
	for rows.Next() {
		var bet PlayerBet
		var amount, payout decimal.Decimal
		var cashed, target, point *decimal.Decimal
		var started, closes *time.Time
		var rate float64
		if err = rows.Scan(&bet.ID, &bet.RoundID, &bet.UserID, &bet.BetNumber, &amount, &bet.Status, &cashed, &payout, &bet.RoundNumber, &bet.RoundStatus, &started, &point, &rate, &closes, &target); err != nil {
			httpapi.Error(w, "failed to load bets", 500)
			return
		}
		bet.Amount = amount.StringFixed(2)
		bet.Payout = payout.StringFixed(2)
		bet.PotentialPayout = "0.00"
		if cashed != nil {
			v := cashed.StringFixed(4)
			bet.CashoutMultiplier = &v
		}
		if target != nil {
			v := target.StringFixed(2)
			bet.AutoCashout = &v
		}
		now := time.Now()
		bet.CanCancel = bet.Status == "ACTIVE" && bet.RoundStatus == "BETTING_OPEN" && (closes == nil || now.Before(*closes))
		if bet.Status == "CASHED_OUT" {
			bet.PotentialPayout = bet.Payout
		}
		if bet.Status == "ACTIVE" && bet.RoundStatus == "RUNNING" && started != nil && point != nil {
			value, err := (multiplier.Clock{Rate: rate}).CashoutMultiplier(*started, now, *point)
			if err == nil {
				bet.CanCashout = true
				potential := amount.Mul(value).Round(2)
				if potential.GreaterThan(h.limits.MaxPayout) {
					potential = h.limits.MaxPayout
				}
				bet.PotentialPayout = potential.StringFixed(2)
			}
		}
		result = append(result, bet)
	}
	if rows.Err() != nil {
		httpapi.Error(w, "failed to load bets", 500)
		return
	}
	JSON(w, result)
}

// Transactions lists the authenticated player's ledger.
// @Summary List own wallet transactions (latest 200)
// @Tags wallet
// @Security BearerAuth
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /api/wallet/transactions [get]
func (h *Handler) Transactions(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, `SELECT COALESCE(jsonb_agg(x),'[]'::jsonb) FROM (SELECT id,type,amount::text,reference,balance_before::text,balance_after::text,created_at FROM wallet_transactions WHERE user_id=$1 ORDER BY id DESC LIMIT 200) x`)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, query string) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.Error(w, "unauthorized", 401)
		return
	}
	var data json.RawMessage
	if err := h.db.QueryRow(r.Context(), query, uid).Scan(&data); err != nil {
		httpapi.Error(w, "failed to load history", 500)
		return
	}
	JSON(w, data)
}

// PlayerBet contains only server-calculated action availability and payout values.
type PlayerBet struct {
	ID                int64   `json:"id"`
	RoundID           int64   `json:"round_id"`
	UserID            int64   `json:"user_id"`
	BetNumber         int16   `json:"bet_number"`
	RoundNumber       int64   `json:"round_number"`
	Status            string  `json:"status"`
	RoundStatus       string  `json:"round_status"`
	Amount            string  `json:"amount"`
	Payout            string  `json:"payout"`
	PotentialPayout   string  `json:"potential_payout"`
	CashoutMultiplier *string `json:"cashout_multiplier,omitempty"`
	AutoCashout       *string `json:"auto_cashout_multiplier,omitempty"`
	CanCancel         bool    `json:"can_cancel"`
	CanCashout        bool    `json:"can_cashout"`
}

func (h *Handler) SetLimits(limits risk.Limits) { h.limits = limits }

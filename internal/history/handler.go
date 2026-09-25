package history

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type Handler struct{ db *pgxpool.Pool }

func NewHandler(db *pgxpool.Pool) *Handler { return &Handler{db} }
func JSON(w http.ResponseWriter, v any)    { httpapi.JSON(w, 200, v) }

// Bets lists the authenticated player's persisted bets.
// @Summary List own bets (latest 200)
// @Tags betting
// @Security BearerAuth
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /api/bets [get]
func (h *Handler) Bets(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, `SELECT COALESCE(jsonb_agg(x),'[]'::jsonb) FROM (SELECT b.id,b.round_id,b.user_id,b.bet_number,b.amount::text,b.status,b.cashout_multiplier::text,b.payout::text,b.placed_at,g.round_number,g.status AS round_status FROM bets b JOIN game_rounds g ON g.id=b.round_id WHERE b.user_id=$1 ORDER BY b.id DESC LIMIT 200) x`)
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

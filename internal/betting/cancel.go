package betting

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"context"
	"fmt"
	"github.com/shopspring/decimal"
	"net/http"
	"strconv"
)

type CancelResult struct {
	BetID            int64  `json:"bet_id"`
	Status           string `json:"status"`
	RefundedAmount   string `json:"refunded_amount"`
	RemainingBalance string `json:"remaining_balance"`
}

func (s *Service) Cancel(ctx context.Context, userID, betID int64) (*CancelResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var roundID int64
	if err := tx.QueryRow(ctx, "SELECT round_id FROM bets WHERE id=$1 AND user_id=$2", betID, userID).Scan(&roundID); err != nil {
		return nil, fmt.Errorf("bet not found")
	}
	status, err := s.repository.GetRoundStatus(ctx, tx, roundID)
	if err != nil {
		return nil, err
	}
	if status != "BETTING_OPEN" {
		return nil, fmt.Errorf("betting is closed")
	}
	var amount decimal.Decimal
	if err := tx.QueryRow(ctx, "SELECT status,amount FROM bets WHERE id=$1 AND user_id=$2 FOR UPDATE", betID, userID).Scan(&status, &amount); err != nil {
		return nil, fmt.Errorf("bet not found")
	}
	if status != "ACTIVE" {
		return nil, fmt.Errorf("bet is no longer active")
	}
	if err := s.walletService.CreditTx(ctx, tx, userID, amount, "REFUND", fmt.Sprintf("CANCEL-BET-%d", betID)); err != nil {
		return nil, err
	}
	// Recheck after waiting for the wallet lock. Roll back the refund if the
	// durable deadline passed while this request waited.
	status, err = s.repository.GetRoundStatus(ctx, tx, roundID)
	if err != nil {
		return nil, err
	}
	if status != "BETTING_OPEN" {
		return nil, fmt.Errorf("betting is closed")
	}
	if _, err := tx.Exec(ctx, "UPDATE bets SET status='CANCELLED' WHERE id=$1", betID); err != nil {
		return nil, err
	}
	var balance decimal.Decimal
	if err := tx.QueryRow(ctx, "SELECT balance FROM users WHERE id=$1", userID).Scan(&balance); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.publish(ctx, "BET_CANCELLED", roundID, betID, amount.StringFixed(2))
	return &CancelResult{betID, "CANCELLED", amount.StringFixed(2), balance.StringFixed(2)}, nil
}

// Cancel refunds an active upcoming bet.
// @Summary Cancel an active bet while betting is open
// @Tags betting
// @Security BearerAuth
// @Produce json
// @Param id path int true "Bet ID"
// @Success 200 {object} CancelResult
// @Failure 409 {object} map[string]string
// @Router /api/bets/{id}/cancel [post]
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.Error(w, "unauthorized", 401)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(w, "invalid bet ID", 400)
		return
	}
	result, err := h.service.Cancel(r.Context(), uid, id)
	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}

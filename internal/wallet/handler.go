package wallet

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"net/http"
)

type Handler struct {
	repository *Repository
}

type BalanceResponse struct {
	UserID  int64  `json:"user_id" example:"1"`
	Balance string `json:"balance" example:"1000.00"`
}

func NewHandler(repository *Repository) *Handler {
	return &Handler{
		repository: repository,
	}
}

// GetBalance returns the authenticated user's wallet balance.
// @Summary Get wallet balance
// @Description Returns the current wallet balance for the authenticated user.
// @Tags wallet
// @Produce json
// @Security BearerAuth
// @Success 200 {object} BalanceResponse
// @Failure 401 {object} auth.ErrorResponse
// @Failure 500 {object} auth.ErrorResponse
// @Router /api/wallet/balance [get]
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())

	if !ok {
		httpapi.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	balance, err := h.repository.GetBalance(
		r.Context(),
		userID,
	)

	if err != nil {
		httpapi.Error(
			w,
			"failed to get balance",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id": userID,
		"balance": balance.StringFixed(2),
	})
}

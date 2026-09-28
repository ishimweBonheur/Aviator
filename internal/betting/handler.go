package betting

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"net/http"

	"github.com/shopspring/decimal"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

// PlaceBet places a bet for the authenticated user.
// @Summary Place a bet
// @Description Places one of the user's bets in an open game round.
// @Tags betting
// @Accept json
// @Produce json
// @Param request body PlaceBetRequest true "Bet details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {string} string "Invalid request or bet"
// @Failure 401 {string} string "Unauthorized"
// @Security BearerAuth
// @Router /api/bets [post]
func (h *Handler) PlaceBet(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var request PlaceBetRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpapi.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	amount, err := decimal.NewFromString(request.Amount)
	if err != nil {
		httpapi.Error(
			w,
			"invalid amount",
			http.StatusBadRequest,
		)
		return
	}

	targets := []decimal.Decimal{}
	if request.AutoCashout != nil {
		target, err := decimal.NewFromString(*request.AutoCashout)
		if err != nil {
			httpapi.Error(w, "invalid auto cashout", 400)
			return
		}
		targets = append(targets, target)
	}
	bet, err := h.service.PlaceBet(
		r.Context(),
		userID,
		request.RoundID,
		request.BetNumber,
		amount,
		targets...,
	)
	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"bet": bet,
	})
}

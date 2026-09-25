package cashout

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"net/http"
	"strconv"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

// CashOut godoc
// @Summary Cash out a bet
// @Description Cashes out an active bet using the current server-side multiplier.
// @Tags cashout
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Bet ID"
// @Success 200 {object} CashoutResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/bets/{id}/cashout [post]
func (h *Handler) CashOut(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Only POST is allowed.
	if r.Method != http.MethodPost {
		httpapi.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	// Get authenticated user.
	userID, ok := auth.UserIDFromContext(
		r.Context(),
	)

	if !ok {
		httpapi.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	// Parse bet ID.
	betID, err := strconv.ParseInt(
		r.PathValue("id"),
		10,
		64,
	)

	if err != nil || betID <= 0 {
		httpapi.Error(
			w,
			"invalid bet ID",
			http.StatusBadRequest,
		)
		return
	}

	// Perform server-side cashout.
	result, err := h.service.CashOut(
		r.Context(),
		userID,
		betID,
	)

	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		return
	}
}

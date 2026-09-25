package cashout

import (
	"aviator/backend/internal/auth"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
// @Param bet_id path int true "Bet ID"
// @Success 200 {object} CashoutResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/bets/{bet_id} [post]
func (h *Handler) CashOut(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Only POST is allowed.
	if r.Method != http.MethodPost {
		http.Error(
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
		http.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	// Expected:
	// /api/bets/{bet_id}
	parts := strings.Split(
		strings.Trim(r.URL.Path, "/"),
		"/",
	)

	if len(parts) != 3 ||
		parts[0] != "api" ||
		parts[1] != "bets" ||
		parts[2] == "" {
		http.NotFound(w, r)
		return
	}

	// Parse bet ID.
	betID, err := strconv.ParseInt(
		parts[2],
		10,
		64,
	)

	if err != nil || betID <= 0 {
		http.Error(
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
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)
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

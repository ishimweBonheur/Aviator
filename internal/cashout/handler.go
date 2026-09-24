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

func (h *Handler) CashOut(
	w http.ResponseWriter,
	r *http.Request,
) {
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

	parts := strings.Split(
		strings.Trim(r.URL.Path, "/"),
		"/",
	)

	if len(parts) != 3 || parts[0] != "api" ||
		parts[1] != "bets" ||
		parts[2] == "" {
		http.NotFound(w, r)
		return
	}

	betID, err := strconv.ParseInt(
		parts[2],
		10,
		64,
	)
	if err != nil {
		http.Error(
			w,
			"invalid bet ID",
			http.StatusBadRequest,
		)
		return
	}

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

	json.NewEncoder(w).Encode(result)
}

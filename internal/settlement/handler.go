package settlement

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// SettleRound settles a crashed game round.
// @Summary Settle game round
// @Description Marks a crashed round as settled and resolves any active bets.
// @Tags settlement
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} Result
// @Failure 400 {object} map[string]string
func (h *Handler) SettleRound(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	result, err := h.service.SettleRound(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func getRoundID(r *http.Request) (int64, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/settlement/rounds/")
	path = strings.Split(path, "/")[0]
	return strconv.ParseInt(path, 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

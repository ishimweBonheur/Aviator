package game

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
	return &Handler{
		service: service,
	}
}

// CreateRound creates a new game round.
// @Summary Create game round
// @Description Creates a new game round when no other round is active.
// @Tags game
// @Produce json
// @Success 201 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) CreateRound(w http.ResponseWriter, r *http.Request) {
	round, err := h.service.CreateRound(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, round)
}

// GetCurrentRound returns the active game round.
// @Summary Get current game round
// @Description Returns running and upcoming rounds, server time and live multiplier; future secrets are redacted.
// @Tags game
// @Produce json
// @Success 200 {object} Snapshot
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/game/rounds/current [get]
func (h *Handler) GetCurrentRound(w http.ResponseWriter, r *http.Request) { h.snapshot(w, r) }

// GetRound returns a game round by ID.
// @Summary Get game round
// @Description Returns a game round by its database ID.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/game/rounds/{id} [get]
func (h *Handler) GetRound(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.repository.GetRoundByID(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusNotFound, "round not found")
		return
	}

	writeJSON(w, http.StatusOK, round)
}

// OpenBetting opens betting for a game round.
// @Summary Open betting
// @Description Moves a game round from CREATED to BETTING_OPEN.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) OpenBetting(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.OpenBetting(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, round)
}

// CloseBetting closes betting for a game round.
// @Summary Close betting
// @Description Moves a game round from BETTING_OPEN to BETTING_CLOSED.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) CloseBetting(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.CloseBetting(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, round)
}

// StartRound starts a game round.
// @Summary Start game round
// @Description Moves a game round from BETTING_CLOSED to RUNNING.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) StartRound(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.StartRound(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, round)
}

// CrashRound crashes a running game round.
// @Summary Crash game round
// @Description Moves a game round from RUNNING to CRASHED.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) CrashRound(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.CrashRound(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, round)
}

// SettleRound settles a crashed game round.
// @Summary Settle game round
// @Description Moves a game round from CRASHED to SETTLED.
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Failure 400 {object} map[string]string
func (h *Handler) SettleRound(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid round id")
		return
	}

	round, err := h.service.SettleRound(
		r.Context(),
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, round)
}

func getRoundID(r *http.Request) (int64, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/game/rounds/")

	// Remove action suffix if present.
	path = strings.Split(path, "/")[0]

	return strconv.ParseInt(path, 10, 64)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data interface{},
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

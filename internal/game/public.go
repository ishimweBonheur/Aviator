package game

import (
	"aviator/backend/internal/database"
	"aviator/backend/internal/multiplier"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type Snapshot struct {
	Running           *GameRound `json:"running"`
	Upcoming          *GameRound `json:"upcoming"`
	CurrentMultiplier string     `json:"current_multiplier,omitempty"`
	ServerTime        time.Time  `json:"server_time"`
}

// Fairness reveals the seed only after settlement.
// @Summary Get round fairness commitment or completed reveal
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} GameRound
// @Router /api/game/rounds/{id}/fairness [get]
func (h *Handler) Fairness(w http.ResponseWriter, r *http.Request) { h.GetRound(w, r) }

// Recent lists completed rounds.
// @Summary List recent completed rounds (latest 50)
// @Tags game
// @Produce json
// @Success 200 {array} GameRound
// @Router /api/game/rounds [get]
func (h *Handler) Recent(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.repository.db.Query(r.Context(), "SELECT id FROM game_rounds WHERE status='SETTLED' ORDER BY round_number DESC LIMIT 50")
	if err != nil {
		writeError(w, 500, "failed to load rounds")
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, 500, "failed to load rounds")
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, 500, "failed to load rounds")
		return
	}
	rounds := []*GameRound{}
	for _, id := range ids {
		round, err := h.service.repository.GetRoundByID(r.Context(), id)
		if err != nil {
			writeError(w, 500, "failed to load round")
			return
		}
		rounds = append(rounds, round)
	}
	writeJSON(w, 200, rounds)
}
func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	repo := h.service.repository
	tx, err := repo.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		writeError(w, 500, "failed to load snapshot")
		return
	}
	defer tx.Rollback(r.Context())
	r = r.WithContext(database.WithQuery(r.Context(), tx))
	running, err := repo.GetRunningRound(r.Context())
	if err != nil {
		writeError(w, 500, "failed to load current round")
		return
	}
	upcoming, err := repo.GetUpcomingRound(r.Context())
	if err != nil {
		writeError(w, 500, "failed to load upcoming round")
		return
	}
	result := Snapshot{Running: running, Upcoming: upcoming, ServerTime: time.Now().UTC()}
	if running != nil && running.StartedAt != nil && running.CrashPoint != nil {
		clock := multiplier.Clock{Rate: running.GrowthRate}
		m := clock.Calculate(*running.StartedAt, result.ServerTime)
		if m.GreaterThan(*running.CrashPoint) {
			m = *running.CrashPoint
		}
		result.CurrentMultiplier = m.StringFixed(2)
	}
	writeJSON(w, 200, result)
}

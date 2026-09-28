package game

import (
	"aviator/backend/internal/fairness"
	"net/http"
)

type Verification struct {
	Verified bool   `json:"verified"`
	Message  string `json:"message"`
}

// Verify performs fairness verification after settlement using the recorded house edge.
// @Summary Verify a settled round's seed commitment and crash result
// @Tags game
// @Produce json
// @Param id path int true "Round ID"
// @Success 200 {object} Verification
// @Failure 400,404,409,500 {object} map[string]string
// @Router /api/game/rounds/{id}/verify [get]
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	id, err := getRoundID(r)
	if err != nil || id < 1 {
		writeError(w, 400, "invalid round id")
		return
	}
	round, err := h.service.repository.GetRoundByID(r.Context(), id)
	if err != nil {
		writeError(w, 404, "round not found")
		return
	}
	if round.Status != RoundSettled {
		writeError(w, 409, "verification is available after settlement")
		return
	}
	if round.ServerSeed == nil || round.CrashPoint == nil || round.HouseEdge == nil {
		writeError(w, 409, "recorded fairness data is incomplete")
		return
	}
	expected, err := fairness.NewService(fairness.Config{HouseEdge: *round.HouseEdge}).GenerateCrashPoint(*round.ServerSeed, round.ClientSeed, round.Nonce)
	if err != nil {
		writeError(w, 500, "verification failed")
		return
	}
	verified := hashSeed(*round.ServerSeed) == round.ServerSeedHash && expected.CrashPoint.Equal(*round.CrashPoint)
	message := "Seed commitment and crash result verified."
	if !verified {
		message = "Seed commitment or crash result does not match."
	}
	writeJSON(w, 200, Verification{verified, message})
}

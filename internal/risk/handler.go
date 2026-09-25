package risk

import (
	"aviator/backend/internal/httpapi"
	"net/http"
)

// ServeHTTP returns configured player limits.
// @Summary Get configured betting and wallet limits
// @Tags game
// @Produce json
// @Success 200 {object} Limits
// @Router /api/limits [get]
func (l Limits) ServeHTTP(w http.ResponseWriter, r *http.Request) { httpapi.JSON(w, 200, l) }

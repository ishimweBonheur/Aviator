package admin

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/config"
	"aviator/backend/internal/httpapi"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	repo    *Repository
	cfg     config.Config
	monitor func(context.Context) map[string]any
	current http.HandlerFunc
}

func Register(mux *http.ServeMux, db *pgxpool.Pool, a *auth.Service, cfg config.Config, monitor func(context.Context) map[string]any, current http.HandlerFunc) {
	h := &Handler{&Repository{db}, cfg, monitor, current}
	routes := map[string]http.HandlerFunc{"GET /api/admin/session": h.Session, "GET /api/admin/overview": h.Overview, "GET /api/admin/analytics": h.Analytics, "GET /api/admin/users/{id}": h.User, "PATCH /api/admin/users/{id}/status": h.Status, "POST /api/admin/users/{id}/wallet-adjustments": h.Adjust, "GET /api/admin/rounds/{id}": h.Round, "GET /api/admin/game/status": h.System, "GET /api/admin/game/current": h.Current, "GET /api/admin/config": h.Config}
	for name := range projections {
		resource := name
		routes["GET /api/admin/"+name] = func(w http.ResponseWriter, r *http.Request) {
			result, err := h.repo.list(r.Context(), resource, r.URL.Query())
			respond(w, result, err)
		}
	}
	for route, handler := range routes {
		mux.Handle(route, a.Middleware(a.RequireAdmin(handler)))
	}
}
func respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		// Only service validation errors are exposed. Unexpected storage errors
		// (including non-Postgres driver errors) stay in server logs.
		if problem, ok := err.(*requestError); ok {
			status := http.StatusConflict
			if strings.Contains(problem.message, "not found") {
				status = http.StatusNotFound
			} else if strings.Contains(problem.message, "invalid") || strings.Contains(problem.message, "must") || strings.Contains(problem.message, "unsupported") {
				status = http.StatusBadRequest
			}
			httpapi.Error(w, problem.message, status)
		} else {
			log.Printf("admin request failed: %v", err)
			httpapi.Error(w, "request could not be completed", 500)
		}
		return
	}
	httpapi.JSON(w, 200, result)
}

type requestError struct{ message string }

// @Summary Get running and upcoming rounds for admin monitoring
// @Description Requires ADMIN; uses the public redaction rules for fairness data.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Success 200 {object} game.Snapshot
// @Failure 401,403,500 {object} ErrorResponse
// @Router /api/admin/game/current [get]
func (h *Handler) Current(w http.ResponseWriter, r *http.Request) { h.current(w, r) }

func (e *requestError) Error() string          { return e.message }
func invalid(format string, args ...any) error { return &requestError{fmt.Sprintf(format, args...)} }
func identifier(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, invalid("invalid id")
	}
	return id, nil
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return invalid("invalid request body")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return invalid("invalid request body")
	}
	return nil
}

// @Summary Admin Session
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/session [get]
func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.UserIDFromContext(r.Context())
	respond(w, map[string]any{"id": id, "role": "ADMIN"}, nil)
}

// @Summary Admin Overview
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Success 200 {object} OverviewResponse
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/overview [get]
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	result, err := h.repo.overview(r.Context())
	respond(w, result, err)
}

// @Summary Admin Analytics
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param from query string false "Inclusive RFC3339 timestamp"
// @Param to query string false "Exclusive RFC3339 timestamp"
// @Success 200 {object} AnalyticsResponse
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/analytics [get]
func (h *Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	result, err := h.repo.analytics(r.Context(), r.URL.Query())
	respond(w, result, err)
}

// @Summary Admin User
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param id path int true "Resource ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/users/{id} [get]
func (h *Handler) User(w http.ResponseWriter, r *http.Request) {
	id, err := identifier(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	user, err := h.repo.detail(r.Context(), "users", id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	result := map[string]any{"user": user}
	for _, resource := range []string{"bets", "deposits", "withdrawals", "wallet-transactions"} {
		page, err := h.repo.list(r.Context(), resource, url.Values{"user_id": {strconv.FormatInt(id, 10)}, "page_size": {"10"}})
		if err != nil {
			respond(w, nil, err)
			return
		}
		result[resource] = page.Items
	}
	respond(w, result, nil)
}

// @Summary Admin Round
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param id path int true "Resource ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/rounds/{id} [get]
func (h *Handler) Round(w http.ResponseWriter, r *http.Request) {
	id, err := identifier(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	round, err := h.repo.detail(r.Context(), "rounds", id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	bets, err := h.repo.list(r.Context(), "bets", url.Values{"round_id": {strconv.FormatInt(id, 10)}})
	if err != nil {
		respond(w, nil, err)
		return
	}
	totals, err := h.repo.objects(r.Context(), `SELECT json_build_object('wagered',COALESCE(sum(amount) FILTER(WHERE status IN ('LOST','CASHED_OUT')),0)::text,'payouts',COALESCE(sum(payout) FILTER(WHERE status='CASHED_OUT'),0)::text,'ggr',COALESCE(sum(amount-payout) FILTER(WHERE status IN ('LOST','CASHED_OUT')),0)::text) FROM bets WHERE round_id=$1`, id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	respond(w, map[string]any{"round": round, "bets": bets, "totals": totals[0]}, nil)
}

// @Summary Admin Status
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param id path int true "Resource ID"
// @Accept json
// @Param request body StatusRequest true "Action details. Reuse the wallet reference on retries."
// @Success 200 {object} map[string]string
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/users/{id}/status [patch]
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	id, err := identifier(r)
	var input StatusRequest
	if err == nil {
		err = decode(w, r, &input)
	}
	if err == nil {
		actor, _ := auth.UserIDFromContext(r.Context())
		err = h.repo.status(r.Context(), actor, id, input)
	}
	respond(w, map[string]string{"status": "updated"}, err)
}

// @Summary Admin Adjust
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param id path int true "Resource ID"
// @Accept json
// @Param request body AdjustmentRequest true "Action details. Reuse the wallet reference on retries."
// @Success 200 {object} map[string]string
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/users/{id}/wallet-adjustments [post]
func (h *Handler) Adjust(w http.ResponseWriter, r *http.Request) {
	id, err := identifier(r)
	var input AdjustmentRequest
	if err == nil {
		err = decode(w, r, &input)
	}
	if err == nil {
		actor, _ := auth.UserIDFromContext(r.Context())
		err = h.repo.adjust(r.Context(), actor, id, input)
	}
	respond(w, map[string]string{"status": "completed"}, err)
}

// @Summary Admin System
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/game/status [get]
func (h *Handler) System(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result := h.monitor(ctx)
	result["backend"] = "ok"
	result["postgresql"] = "ok"
	if h.repo.db.Ping(ctx) != nil {
		result["postgresql"] = "unavailable"
	}
	respond(w, result, nil)
}

// @Summary Admin Config
// @Description Requires an active ADMIN role. Analytics use settled rounds, exclude cancelled bets, and bucket days in UTC.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500 {object} ErrorResponse
// @Router /api/admin/config [get]
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]any{"read_only": true, "house_edge": h.cfg.HouseEdge, "betting_window_seconds": h.cfg.BettingWindow.Seconds(), "growth_rate": h.cfg.GrowthRate, "limits": h.cfg.Limits}, nil)
}

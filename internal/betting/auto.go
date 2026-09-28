package betting

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/risk"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

type AutoSetting struct {
	BetNumber   int16   `json:"bet_number"`
	Enabled     bool    `json:"enabled"`
	Amount      string  `json:"amount"`
	AutoCashout *string `json:"auto_cashout_multiplier"`
	LastRoundID int64   `json:"last_round_id"`
	LastError   string  `json:"last_error"`
}
type AutoInput struct {
	Enabled     bool    `json:"enabled"`
	Amount      string  `json:"amount"`
	AutoCashout *string `json:"auto_cashout_multiplier"`
}

func (s *Service) GetAuto(ctx context.Context, userID int64) ([]AutoSetting, error) {
	rows, err := s.db.Query(ctx, `SELECT p.n,COALESCE(a.enabled,false),COALESCE(a.amount,1000)::text,a.auto_cashout_multiplier::text,COALESCE(a.last_round_id,0),COALESCE(a.last_error,'') FROM generate_series(1,2) p(n) LEFT JOIN auto_bet_settings a ON a.user_id=$1 AND a.bet_number=p.n ORDER BY p.n`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AutoSetting{}
	for rows.Next() {
		var a AutoSetting
		if err := rows.Scan(&a.BetNumber, &a.Enabled, &a.Amount, &a.AutoCashout, &a.LastRoundID, &a.LastError); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func ValidateTarget(target decimal.Decimal) error {
	if target.LessThan(decimal.RequireFromString("1.01")) || target.GreaterThan(decimal.NewFromInt(1000000)) || !target.Equal(target.Round(2)) {
		return fmt.Errorf("auto cashout must be 1.01 to 1000000 with at most two decimal places")
	}
	return nil
}
func (s *Service) SaveAuto(ctx context.Context, userID int64, panel int16, input AutoInput) ([]AutoSetting, error) {
	if panel != 1 && panel != 2 {
		return nil, fmt.Errorf("invalid panel")
	}
	amount, err := decimal.NewFromString(input.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount")
	}
	if err = risk.Amount("bet", amount, s.limits.MinBet, s.limits.MaxBet); err != nil {
		return nil, err
	}
	var target any
	if input.AutoCashout != nil {
		value, err := decimal.NewFromString(*input.AutoCashout)
		if err != nil {
			return nil, fmt.Errorf("invalid auto cashout")
		}
		if err = ValidateTarget(value); err != nil {
			return nil, err
		}
		target = value.StringFixed(2)
	}
	_, err = s.db.Exec(ctx, `INSERT INTO auto_bet_settings(user_id,bet_number,enabled,amount,auto_cashout_multiplier) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,bet_number) DO UPDATE SET enabled=EXCLUDED.enabled,amount=EXCLUDED.amount,auto_cashout_multiplier=EXCLUDED.auto_cashout_multiplier,last_error='',updated_at=clock_timestamp()`, userID, panel, input.Enabled, amount.StringFixed(2), target)
	if err != nil {
		return nil, err
	}
	return s.GetAuto(ctx, userID)
}

// AutoSettings manages backend-executed automation; it continues while the browser is closed.
// @Summary Get or update own automatic bet settings
// @Tags betting
// @Security BearerAuth
// @Produce json
// @Success 200 {array} AutoSetting
// @Failure 400,401,403,500 {object} map[string]string
// @Router /api/bets/auto [get]
func (h *Handler) AutoSettings(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpapi.Error(w, "unauthorized", 401)
		return
	}
	var result []AutoSetting
	var err error
	if r.Method == http.MethodGet {
		result, err = h.service.GetAuto(r.Context(), uid)
	} else {
		panel, parseErr := strconv.ParseInt(r.PathValue("panel"), 10, 16)
		if parseErr != nil {
			httpapi.Error(w, "invalid panel", 400)
			return
		}
		var input AutoInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			httpapi.Error(w, "invalid request body", 400)
			return
		}
		result, err = h.service.SaveAuto(r.Context(), uid, int16(panel), input)
	}
	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}

// UpdateAuto documents the PUT route handled by AutoSettings.
// @Summary Update own server-side auto bet settings
// @Description Automatic bets continue without a browser. One attempt per round and panel. A failed bet disables the setting. Auto cashout is captured on each accepted bet.
// @Tags betting
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param panel path int true "Panel 1 or 2"
// @Param request body AutoInput true "Settings"
// @Success 200 {array} AutoSetting
// @Failure 400,401,403,500 {object} map[string]string
// @Router /api/bets/auto/{panel} [put]
func (h *Handler) UpdateAuto(w http.ResponseWriter, r *http.Request) { h.AutoSettings(w, r) }

func (s *Service) RunAutomatic(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick, cancel := context.WithTimeout(ctx, time.Second)
			if err := s.ProcessAutomatic(tick); err != nil && ctx.Err() == nil {
				log.Printf("automatic bets: %v", err)
			}
			cancel()
		}
	}
}

// ProcessAutomatic may run on every instance. Settings, attempts and wallet changes
// commit together; row locks and the existing unique bet key fence retries/failover.
func (s *Service) ProcessAutomatic(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT a.user_id,a.bet_number,g.id FROM auto_bet_settings a CROSS JOIN game_rounds g WHERE a.enabled AND g.status='BETTING_OPEN' AND (g.betting_closes_at IS NULL OR g.betting_closes_at>clock_timestamp()) AND a.last_round_id<g.id ORDER BY a.user_id,a.bet_number LIMIT 100`)
	if err != nil {
		return err
	}
	type candidate struct {
		user, round int64
		panel       int16
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.user, &c.panel, &c.round); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range candidates {
		if err = s.automaticBet(ctx, c.user, c.round, c.panel); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) automaticBet(ctx context.Context, userID, roundID int64, panel int16) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var amount decimal.Decimal
	var target *decimal.Decimal
	err = tx.QueryRow(ctx, `SELECT amount,auto_cashout_multiplier FROM auto_bet_settings WHERE user_id=$1 AND bet_number=$2 AND enabled AND last_round_id<$3 FOR UPDATE SKIP LOCKED`, userID, panel, roundID).Scan(&amount, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bets WHERE user_id=$1 AND round_id=$2 AND bet_number=$3)", userID, roundID, panel).Scan(&exists); err != nil {
		return err
	}
	var bet *Bet
	var failure string
	if !exists {
		save, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		targets := []decimal.Decimal{}
		if target != nil {
			targets = append(targets, *target)
		}
		bet, err = s.placeBetTx(ctx, save, userID, roundID, panel, amount, targets...)
		if err != nil {
			if rollbackErr := save.Rollback(ctx); rollbackErr != nil {
				return rollbackErr
			}
			failure = "Automatic bet was rejected. Check your balance, account status and betting limits."
		} else if err = save.Commit(ctx); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE auto_bet_settings SET last_round_id=$3,last_error=$4,enabled=CASE WHEN $4='' THEN enabled ELSE false END,updated_at=clock_timestamp() WHERE user_id=$1 AND bet_number=$2`, userID, panel, roundID, failure); err != nil {
		return err
	}
	if bet != nil {
		if _, err = tx.Exec(ctx, "UPDATE bets SET is_auto=true WHERE id=$1", bet.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if bet != nil {
		s.publish(ctx, realtime.EventBetPlaced, roundID, bet.ID, amount.StringFixed(2))
	}
	return nil
}

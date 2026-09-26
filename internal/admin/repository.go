package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ db *pgxpool.Pool }
type Page struct {
	Items    []map[string]any `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int64            `json:"total"`
}

// Only explicit projections are allowed: never serialize database users or
// rounds wholesale (password hashes and unrevealed fairness data are private).
var projections = map[string]string{
	"users":               "id,username,email,role,status,balance::text,created_at",
	"bets":                "id,user_id,round_id,bet_number,amount::text,status,cashout_multiplier::text,payout::text,placed_at,cashed_out_at",
	"rounds":              "id,round_number,status,server_seed_hash,client_seed,nonce,CASE WHEN status='SETTLED' THEN server_seed END AS server_seed,CASE WHEN status IN ('CRASHED','SETTLED') THEN crash_point::text END AS crash_point,betting_closes_at,started_at,ended_at,created_at",
	"deposits":            "id,user_id,amount::text,provider,provider_reference,status,created_at,completed_at",
	"withdrawals":         "id,user_id,amount::text,provider,provider_reference,status,created_at,completed_at",
	"wallet-transactions": "id,user_id,type,amount::text,reference,balance_before::text,balance_after::text,created_at",
}

func table(resource string) string {
	if resource == "rounds" {
		return "game_rounds"
	}
	if resource == "wallet-transactions" {
		return "wallet_transactions"
	}
	return resource
}

func (repo *Repository) objects(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := repo.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func dateRange(q url.Values) (any, any, error) {
	var values [2]any
	for i, key := range []string{"from", "to"} {
		if value := q.Get(key); value != "" {
			t, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, nil, invalid("%s must be an RFC3339 timestamp", key)
			}
			values[i] = t
		}
	}
	if values[0] != nil && values[1] != nil && !values[0].(time.Time).Before(values[1].(time.Time)) {
		return nil, nil, invalid("from must be before to")
	}
	return values[0], values[1], nil
}

// @Summary List administrative records
// @Description Requires ADMIN. Amounts are decimal strings. Rounds redact seeds until settlement and crash points until crash. Returns snapshot-consistent total and page, newest first.
// @Tags admin
// @Security BearerAuth
// @Produce json
// @Param page query int false "Page starts at 1" default(1)
// @Param page_size query int false "Page size, maximum 100" default(25)
// @Param status query string false "Status except wallet transactions"
// @Param user_id query int false "User ID for bets, payments, wallet"
// @Param round_id query int false "Round ID for bets"
// @Param search query string false "Username/email search for users"
// @Param type query string false "Wallet transaction type"
// @Param reference query string false "Exact wallet reference"
// @Param from query string false "Inclusive RFC3339 timestamp"
// @Param to query string false "Exclusive RFC3339 timestamp"
// @Success 200 {object} Page
// @Failure 400,401,403,500 {object} ErrorResponse
// @Router /api/admin/users [get]
// @Router /api/admin/bets [get]
// @Router /api/admin/rounds [get]
// @Router /api/admin/deposits [get]
// @Router /api/admin/withdrawals [get]
// @Router /api/admin/wallet-transactions [get]
func (repo *Repository) list(ctx context.Context, resource string, q url.Values) (Page, error) {
	projection, ok := projections[resource]
	if !ok {
		return Page{}, invalid("resource not found")
	}
	page, size := 1, 25
	for key, dest := range map[string]*int{"page": &page, "page_size": &size} {
		if q.Get(key) != "" {
			n, err := strconv.Atoi(q.Get(key))
			if err != nil || n < 1 || n > 1000000 {
				return Page{}, invalid("invalid %s", key)
			}
			*dest = n
		}
	}
	if size > 100 {
		return Page{}, invalid("page_size must be at most 100")
	}
	from, to, err := dateRange(q)
	if err != nil {
		return Page{}, err
	}
	args := []any{from, to}
	date := "created_at"
	if resource == "bets" {
		date = "placed_at"
	}
	where := " WHERE ($1::timestamptz IS NULL OR " + date + ">=$1) AND ($2::timestamptz IS NULL OR " + date + "<$2)"
	add := func(expr string, value any) {
		args = append(args, value)
		where += " AND " + fmt.Sprintf(expr, len(args))
	}
	if status := q.Get("status"); status != "" && resource != "wallet-transactions" {
		add("status=$%d", status)
	}
	for _, key := range []string{"user_id", "round_id"} {
		if value := q.Get(key); value != "" {
			if (key == "round_id" && resource != "bets") || (key == "user_id" && (resource == "users" || resource == "rounds")) {
				return Page{}, invalid("unsupported filter")
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id < 1 {
				return Page{}, invalid("invalid %s", key)
			}
			add(key+"=$%d", id)
		}
	}
	if resource == "users" && q.Get("search") != "" {
		args = append(args, "%"+q.Get("search")+"%")
		where += fmt.Sprintf(" AND (username ILIKE $%d OR email ILIKE $%d)", len(args), len(args))
	}
	if resource == "wallet-transactions" {
		for _, key := range []string{"type", "reference"} {
			if q.Get(key) != "" {
				add(key+"=$%d", q.Get(key))
			}
		}
	}
	// Count and page share one statement and therefore one MVCC snapshot.
	query := fmt.Sprintf(`WITH filtered AS (SELECT %s FROM %s %s), paged AS (SELECT * FROM filtered ORDER BY id DESC LIMIT %d OFFSET %d) SELECT json_build_object('items',COALESCE((SELECT json_agg(paged) FROM paged),'[]'::json),'total',(SELECT count(*) FROM filtered))`, projection, table(resource), where, size, (page-1)*size)
	var raw []byte
	err = repo.db.QueryRow(ctx, query, args...).Scan(&raw)
	if err != nil {
		return Page{}, err
	}
	var result Page
	err = json.Unmarshal(raw, &result)
	result.Page = page
	result.PageSize = size
	return result, err
}

func (repo *Repository) detail(ctx context.Context, resource string, id int64) (map[string]any, error) {
	rows, err := repo.objects(ctx, "SELECT row_to_json(t) FROM (SELECT "+projections[resource]+" FROM "+table(resource)+" WHERE id=$1) t", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, invalid("%s not found", resource)
	}
	return rows[0], nil
}

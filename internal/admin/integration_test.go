package admin

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/config"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	dsn := os.Getenv("ADMIN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ADMIN_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("admin_test_%d", time.Now().UnixNano())
	if _, err = db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); db.Close() })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, name := range []string{"000001_initial_schema.up.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO users(username,email,password_hash,role,balance) VALUES('admin','admin@test','unused','ADMIN',0),('player','player@test','unused','PLAYER',1000)`)
	if err != nil {
		t.Fatal(err)
	}
	return &Repository{pool}
}
func TestAdminFinancialAndQueries(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	input := AdjustmentRequest{"CREDIT", "10.25", "test credit", "test-reference-123"}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.adjust(ctx, 1, 2, input); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var balance string
	var count int
	repo.db.QueryRow(ctx, "SELECT balance::text FROM users WHERE id=2").Scan(&balance)
	repo.db.QueryRow(ctx, "SELECT count(*) FROM wallet_transactions").Scan(&count)
	if balance != "1010.25" || count != 1 {
		t.Fatalf("duplicate adjustment: balance=%s ledger=%d", balance, count)
	}
	input.Amount = "10.26"
	if repo.adjust(ctx, 1, 2, input) == nil {
		t.Fatal("mismatched retry accepted")
	}
	input.Reference = "test-debit-123"
	input.Direction = "DEBIT"
	input.Amount = "2000"
	if repo.adjust(ctx, 1, 2, input) == nil {
		t.Fatal("overdraft accepted")
	}
	if err := repo.status(ctx, 1, 2, StatusRequest{"SUSPENDED", "test suspension"}); err != nil {
		t.Fatal(err)
	}
	if repo.status(ctx, 1, 1, StatusRequest{"BLOCKED", "self lockout"}) == nil {
		t.Fatal("admin suspension accepted")
	}
	_, err := repo.db.Exec(ctx, `INSERT INTO game_rounds(round_number,server_seed_hash,server_seed,client_seed,nonce,status,crash_point,ended_at) VALUES(1,repeat('a',64),'revealed','client',1,'SETTLED',2,now()),(2,repeat('b',64),'secret','client',2,'RUNNING',9,NULL);
 INSERT INTO bets(round_id,user_id,bet_number,amount,status,payout) VALUES(1,2,1,100,'CASHED_OUT',94),(1,2,2,1000,'CANCELLED',0),(2,2,1,500,'ACTIVE',0);
 INSERT INTO deposits(user_id,amount,provider,provider_reference,status,completed_at) VALUES(2,5000,'SANDBOX','deposit-test','COMPLETED',now());`)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := repo.analytics(ctx, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"total_wagered": "100.00", "total_payouts": "94.00", "ggr": "6.00", "rtp_percent": "94.00", "deposits": "5000.00"} {
		if stats[key] != want {
			t.Errorf("%s=%v want %s", key, stats[key], want)
		}
	}
	for resource := range projections {
		page, err := repo.list(ctx, resource, url.Values{"page_size": {"1"}})
		if err != nil {
			t.Fatalf("list %s: %v", resource, err)
		}
		if len(page.Items) != 1 && resource != "withdrawals" {
			t.Errorf("unexpected %s count %d", resource, len(page.Items))
		}
	}
	live, err := repo.detail(ctx, "rounds", 2)
	if err != nil {
		t.Fatal(err)
	}
	if live["server_seed"] != nil || live["crash_point"] != nil {
		t.Fatal("unrevealed fairness data leaked")
	}
	user, err := repo.detail(ctx, "users", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := user["password_hash"]; ok {
		t.Fatal("password hash leaked")
	}
	empty, err := repo.analytics(ctx, url.Values{"from": {"2000-01-01T00:00:00Z"}, "to": {"2000-01-02T00:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	if empty["rtp_percent"] != "0" {
		t.Fatalf("empty analytics: %v", empty)
	}
}

func TestAdminHTTPAuthorization(t *testing.T) {
	repo := testRepository(t)
	mux := http.NewServeMux()
	service := auth.NewService(repo.db, "test-secret")
	Register(mux, repo.db, service, config.Config{}, func(context.Context) map[string]any { return map[string]any{} }, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	token := func(id int64) string {
		v, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": id, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("test-secret"))
		return v
	}
	paths := []string{"session", "overview", "analytics", "users", "users/2", "bets", "rounds", "deposits", "withdrawals", "wallet-transactions", "game/status", "game/current", "config"}
	for _, path := range paths {
		for _, actor := range []int64{0, 1, 2} {
			r := httptest.NewRequest("GET", "/api/admin/"+path, nil)
			if actor != 0 {
				r.Header.Set("Authorization", "Bearer "+token(actor))
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			want := 200
			if actor == 0 {
				want = 401
			}
			if actor == 2 {
				want = 403
			}
			if w.Code != want {
				t.Errorf("actor %d %s: %d %s", actor, path, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"status", "wallet-adjustments"} {
		method := "PATCH"
		if path == "wallet-adjustments" {
			method = "POST"
		}
		r := httptest.NewRequest(method, "/api/admin/users/2/"+path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+token(2))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("player mutation %s: %d", path, w.Code)
		}
	}
	repo.db.Exec(context.Background(), "UPDATE users SET role='PLAYER' WHERE id=1")
	r := httptest.NewRequest("GET", "/api/admin/overview", nil)
	r.Header.Set("Authorization", "Bearer "+token(1))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("revoked role retains access")
	}
}

func TestInitialMigrationRoundTrip(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_initial_schema.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.db.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = repo.db.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema()").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("down left %d tables", remaining)
	}
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_initial_schema.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.db.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.overview(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.list(ctx, "rounds", url.Values{}); err != nil {
		t.Fatal(err)
	}
}

func TestDateRangeValidation(t *testing.T) {
	for _, q := range []url.Values{{"from": {"bad"}}, {"from": {"2026-01-02T00:00:00Z"}, "to": {"2026-01-01T00:00:00Z"}}} {
		if _, _, err := dateRange(q); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}

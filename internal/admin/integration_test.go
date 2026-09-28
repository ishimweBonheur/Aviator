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
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_initial_schema.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
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
 UPDATE bets SET is_auto=true,auto_cashout_multiplier=1.50 WHERE round_id=2;
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
	if _, err := repo.db.Exec(ctx, "UPDATE users SET balance=9000 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	overview, err := repo.overview(ctx)
	if err != nil || overview["total_player_balances"] != "1010.25" {
		t.Fatalf("player liability includes administrators: %v %v", overview, err)
	}
	audit, err := repo.list(ctx, "audit-logs", url.Values{"admin_id": {"1"}, "user_id": {"2"}, "action": {"WALLET_ADJUSTMENT"}, "reference": {"test-reference-123"}})
	if err != nil || audit.Total != 1 {
		t.Fatalf("audit filters: %v %v", audit, err)
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
	paths := []string{"session", "overview", "analytics", "auto-bets", "auto-cashouts", "audit-logs", "users", "users/2", "bets", "rounds", "deposits", "withdrawals", "wallet-transactions", "game/status", "game/current", "config"}
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

func TestAdminRoleManagement(t *testing.T) {
	repo := testRepository(t)
	mux := http.NewServeMux()
	service := auth.NewService(repo.db, "test-secret")
	Register(mux, repo.db, service, config.Config{}, func(context.Context) map[string]any { return map[string]any{} }, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	register := auth.NewHandler(service)
	registration := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(`{"username":"self-promote","email":"self-promote@test","password":"password123","role":"ADMIN"}`))
	registrationResult := httptest.NewRecorder()
	register.Register(registrationResult, registration)
	if registrationResult.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", registrationResult.Code, registrationResult.Body.String())
	}
	var registrationRole string
	if err := repo.db.QueryRow(context.Background(), "SELECT role FROM users WHERE email='self-promote@test'").Scan(&registrationRole); err != nil || registrationRole != "PLAYER" {
		t.Fatalf("registration role=%q err=%v", registrationRole, err)
	}
	token := func(id int64) string {
		v, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": id, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("test-secret"))
		return v
	}
	request := func(actor int64, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token(actor))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if response := request(2, "PATCH", "/api/admin/users/2/role", `{"role":"ADMIN"}`); response.Code != http.StatusForbidden {
		t.Fatalf("player role change: %d %s", response.Code, response.Body.String())
	}
	if response := request(1, "PATCH", "/api/admin/users/1/role", `{"role":"PLAYER"}`); response.Code != http.StatusConflict {
		t.Fatalf("self role change: %d %s", response.Code, response.Body.String())
	}
	if response := request(1, "PATCH", "/api/admin/users/2/role", `{"role":"ADMIN"}`); response.Code != http.StatusOK {
		t.Fatalf("promote player: %d %s", response.Code, response.Body.String())
	}
	if response := request(1, "PATCH", "/api/admin/users/2/role", `{"role":"PLAYER"}`); response.Code != http.StatusOK {
		t.Fatalf("demote admin: %d %s", response.Code, response.Body.String())
	}
	if response := request(2, "POST", "/api/admin/users/admins", `{"username":"third-admin","email":"third-admin@test","password":"password123"}`); response.Code != http.StatusForbidden {
		t.Fatalf("player admin creation: %d %s", response.Code, response.Body.String())
	}
	response := request(1, "POST", "/api/admin/users/admins", `{"username":"third-admin","email":"third-admin@test","password":"password123"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin creation: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "password_hash") {
		t.Fatal("admin creation response included password hash")
	}
	var role string
	if err := repo.db.QueryRow(context.Background(), "SELECT role FROM users WHERE email='third-admin@test'").Scan(&role); err != nil || role != "ADMIN" {
		t.Fatalf("created role=%q err=%v", role, err)
	}
	var createdAudit, roleAudit int
	if err := repo.db.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit_logs WHERE action='ADMIN_CREATED'").Scan(&createdAudit); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit_logs WHERE action='ROLE_CHANGE'").Scan(&roleAudit); err != nil {
		t.Fatal(err)
	}
	if createdAudit != 1 || roleAudit != 2 {
		t.Fatalf("audit events: created=%d role_changes=%d", createdAudit, roleAudit)
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

func TestAdminRecordsIdentifyAccountOwners(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	_, err := repo.db.Exec(ctx, `INSERT INTO game_rounds(round_number,server_seed_hash,client_seed,nonce,status) VALUES(1,repeat('a',64),'client',1,'BETTING_OPEN');
    INSERT INTO bets(round_id,user_id,bet_number,amount,is_auto,auto_cashout_multiplier) VALUES(1,2,1,50,true,2);
    INSERT INTO deposits(user_id,amount,provider,provider_reference) VALUES(2,100,'SANDBOX','owner-deposit');
    INSERT INTO withdrawals(user_id,amount,provider) VALUES(2,50,'SANDBOX');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.adjust(ctx, 1, 2, AdjustmentRequest{"CREDIT", "10.00", "identity check", "owner-adjustment"}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"bets", "auto-bets", "auto-cashouts", "deposits", "withdrawals", "wallet-transactions", "audit-logs"} {
		page, err := repo.list(ctx, resource, url.Values{"user_id": {"2"}})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("%s: %v %v", resource, page, err)
		}
		row := page.Items[0]
		if row["username"] != "player" || row["user_id"] != float64(2) {
			t.Fatalf("%s owner: %v", resource, row)
		}
		if resource == "audit-logs" && row["admin_username"] != "admin" {
			t.Fatalf("audit actor: %v", row)
		}
		if _, found := row["password_hash"]; found {
			t.Fatal("private account data exposed")
		}
		empty, err := repo.list(ctx, resource, url.Values{"user_id": {"1"}})
		if err != nil || empty.Total != 0 {
			t.Fatalf("%s user filter mixed accounts: %v %v", resource, empty, err)
		}
	}
	row, err := repo.detail(ctx, "bets", 1)
	if err != nil || row["username"] != "player" {
		t.Fatalf("detail owner: %v %v", row, err)
	}
}

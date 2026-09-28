package game

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/betting"
	"aviator/backend/internal/cashout"
	"aviator/backend/internal/fairness"
	"aviator/backend/internal/history"
	"aviator/backend/internal/multiplier"
	"aviator/backend/internal/realtime"
	"aviator/backend/internal/settlement"
	"aviator/backend/internal/wallet"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type lifecycleEvents struct{ events []realtime.Event }

func (p *lifecycleEvents) Publish(_ context.Context, event realtime.Event) {
	p.events = append(p.events, event)
}
func (p *lifecycleEvents) State(context.Context, string, any) {}

func lifecycleDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GAME_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GAME_TEST_DATABASE_URL for isolated PostgreSQL lifecycle tests")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("game_test_%d", time.Now().UnixNano())
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
	return pool
}

func TestOverlappingLifecycleAndBetRules(t *testing.T) {
	db := lifecycleDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(db)
	service := NewService(repo, fairness.NewService(fairness.Config{HouseEdge: decimal.RequireFromString("0.03")}))
	m := multiplier.NewService()
	events := &lifecycleEvents{}
	engine := NewEngine(service, m, settlement.NewService(db, settlement.NewRepository(db)), events, events, 5*time.Second, 0.08)
	step := func() {
		t.Helper()
		if err := engine.recoverStep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	w := wallet.NewService(db)
	bets := betting.NewService(db, betting.NewRepository(db), w)
	cash := cashout.NewService(db, cashout.NewRepository(db), w, m)
	exec("INSERT INTO users(username,email,password_hash,balance) VALUES('pilot','pilot@test','unused',10000),('other','other@test','unused',10000)")
	step()
	upcoming, err := repo.GetUpcomingRound(ctx)
	if err != nil || upcoming == nil {
		t.Fatalf("upcoming: %v", err)
	}
	id := upcoming.ID
	if events.events[0].Type != realtime.EventRoundOpened || events.events[1].Type != realtime.EventCountdown || *events.events[1].SecondsRemaining != 5 {
		t.Fatalf("opening sequence: %+v", events.events)
	}
	third, err := bets.PlaceBet(ctx, 2, id, 1, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	first, err := bets.PlaceBet(ctx, 1, id, 1, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	second, err := bets.PlaceBet(ctx, 1, id, 2, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cash.CashOut(ctx, 1, first.ID); err == nil {
		t.Fatal("cashout accepted during countdown")
	}
	if _, err = bets.Cancel(ctx, 1, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = bets.Cancel(ctx, 1, second.ID); err == nil {
		t.Fatal("cancelled inactive bet")
	}
	for seconds := 4; seconds >= 1; seconds-- {
		exec("UPDATE game_rounds SET betting_closes_at=clock_timestamp()+$2*interval '1 second' WHERE id=$1", id, float64(seconds)-0.1)
		step()
		last := events.events[len(events.events)-1]
		if last.Type != realtime.EventCountdown || *last.SecondsRemaining != seconds {
			t.Fatalf("countdown %d: %+v", seconds, last)
		}
		if _, err = m.Current(id); err == nil {
			t.Fatal("multiplier active during betting")
		}
	}
	exec("UPDATE game_rounds SET betting_closes_at=clock_timestamp()-interval '1 second' WHERE id=$1", id)
	if _, err = bets.PlaceBet(ctx, 1, id, 2, decimal.NewFromInt(100)); err == nil {
		t.Fatal("bet after deadline")
	}
	if _, err = bets.Cancel(ctx, 1, first.ID); err == nil {
		t.Fatal("cancel after deadline")
	}
	// Fix a known crash point for the test, without changing production fairness.
	exec("UPDATE game_rounds SET crash_point=2.47 WHERE id=$1", id)
	step()
	running, err := repo.GetRunningRound(ctx)
	if err != nil || running == nil || running.StartedAt == nil {
		t.Fatalf("running: %v", err)
	}
	last := events.events[len(events.events)-2:]
	if last[0].Type != realtime.EventRoundStarted || last[1].Type != realtime.EventMultiplierUpdate || last[1].Multiplier != "1.00" {
		t.Fatalf("start sequence: %+v", last)
	}
	step()
	nextQueued, err := repo.GetUpcomingRound(ctx)
	if err != nil || nextQueued == nil || nextQueued.Status != RoundBettingOpen || nextQueued.BettingClosesAt != nil {
		t.Fatalf("upcoming betting must stay open throughout flight: %v", err)
	}
	queued, err := bets.PlaceBet(ctx, 1, nextQueued.ID, 1, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	queuedOther, err := bets.PlaceBet(ctx, 1, nextQueued.ID, 2, decimal.NewFromInt(100))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cash.CashOut(ctx, 1, queued.ID); err == nil {
		t.Fatal("cashout of queued bet during another flight")
	}
	if _, err = bets.Cancel(ctx, 1, queuedOther.ID); err != nil {
		t.Fatal(err)
	}
	recorderLive := httptest.NewRecorder()
	NewHandler(service).snapshot(recorderLive, httptest.NewRequest("GET", "/api/game/rounds/current", nil))
	var live Snapshot
	if err = json.Unmarshal(recorderLive.Body.Bytes(), &live); err != nil || live.Running == nil || live.Upcoming == nil || live.Phase != "RUNNING" {
		t.Fatalf("snapshot must expose both rounds: %s", recorderLive.Body.String())
	}
	if _, err = bets.Cancel(ctx, 1, first.ID); err == nil {
		t.Fatal("cancel during flight")
	}
	if _, err = bets.PlaceBet(ctx, 1, id, 2, decimal.NewFromInt(100)); err == nil {
		t.Fatal("bet during flight")
	}
	if _, err = cash.CashOut(ctx, 1, first.ID); err != nil {
		t.Fatal(err)
	}
	// An unresolved ACTIVE bet must be settled, even if the engine was delayed.
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '1 hour' WHERE id=$1", id)
	activeID := third.ID
	if _, err = cash.CashOut(ctx, 2, activeID); err == nil {
		t.Fatal("cashout accepted after authoritative crash time")
	}
	recorder := httptest.NewRecorder()
	NewHandler(service).snapshot(recorder, httptest.NewRequest("GET", "/api/game/rounds/current", nil))
	var snapshot Snapshot
	if err = json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Running != nil || snapshot.CurrentMultiplier != "" {
		t.Fatal("expired flight exposed as live")
	}
	before := len(events.events)
	step()
	expected := []string{realtime.EventRoundCrashed, realtime.EventRoundSettled, realtime.EventCountdown}
	got := events.events[before:]
	if len(got) != len(expected) {
		t.Fatalf("post-crash events: %+v", got)
	}
	for i, kind := range expected {
		if got[i].Type != kind {
			t.Fatalf("event %d: %s, want %s", i, got[i].Type, kind)
		}
	}
	if _, err = m.Current(id); err == nil {
		t.Fatal("crashed multiplier still active")
	}
	var status string
	if err = db.QueryRow(ctx, "SELECT status FROM bets WHERE id=$1", activeID).Scan(&status); err != nil || status != "LOST" {
		t.Fatalf("unsettled bet: %s %v", status, err)
	}
	next, _ := repo.GetUpcomingRound(ctx)
	if next == nil || next.Status != RoundBettingOpen || time.Until(*next.BettingClosesAt) < 4*time.Second {
		t.Fatal("next round missing full countdown")
	}
	if next.ID != nextQueued.ID {
		t.Fatal("queued round replaced at crash")
	}
	if _, err = bets.Cancel(ctx, 1, queued.ID); err != nil {
		t.Fatal("queued bet not cancellable during countdown", err)
	}
	// Repeated recovery during countdown must neither start the flight nor emit ticks.
	before = len(events.events)
	step()
	for _, event := range events.events[before:] {
		if event.Type == realtime.EventMultiplierUpdate {
			t.Fatal("tick between crash and next start")
		}
	}
	// Reusing a cancelled panel must debit/refund once for each placement.
	rebet, err := bets.PlaceBet(ctx, 1, next.ID, 1, decimal.NewFromInt(125))
	if err != nil {
		t.Fatal("rebet after cancellation", err)
	}
	if _, err = bets.Cancel(ctx, 1, rebet.ID); err != nil {
		t.Fatal("refund rebet", err)
	}
	rebet, err = bets.PlaceBet(ctx, 1, next.ID, 1, decimal.NewFromInt(150))
	if err != nil {
		t.Fatal(err)
	}
	otherRebet, err := bets.PlaceBet(ctx, 1, next.ID, 2, decimal.NewFromInt(200))
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE game_rounds SET crash_point=2.47, betting_closes_at=clock_timestamp()-interval '1 second' WHERE id=$1", next.ID)
	step()
	if _, err = bets.Cancel(ctx, 1, rebet.ID); err == nil {
		t.Fatal("cancel accepted after queued round started")
	}
	if _, err = cash.CashOut(ctx, 1, rebet.ID); err != nil {
		t.Fatal("queued cashout on own running round", err)
	}
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '1 hour' WHERE id=$1", next.ID)
	step()
	if err = db.QueryRow(ctx, "SELECT status FROM bets WHERE id=$1", otherRebet.ID).Scan(&status); err != nil || status != "LOST" {
		t.Fatalf("second queued panel did not settle independently: %s %v", status, err)
	}

}

func TestSettlementFailureBlocksNextCountdownAndRecovers(t *testing.T) {
	db := lifecycleDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(db)
	service := NewService(repo, fairness.NewService(fairness.Config{HouseEdge: decimal.RequireFromString("0.03")}))
	m := multiplier.NewService()
	events := &lifecycleEvents{}
	engine := NewEngine(service, m, settlement.NewService(db, settlement.NewRepository(db)), events, events, 5*time.Second, 0.08)
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	round, err := repo.GetUpcomingRound(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := *round.BettingClosesAt
	// Restart in BETTING_OPEN keeps the persisted deadline, without extending it.
	engine = NewEngine(service, m, settlement.NewService(db, settlement.NewRepository(db)), events, events, 5*time.Second, 0.08)
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	resumed, err := repo.GetUpcomingRound(ctx)
	if err != nil || !resumed.BettingClosesAt.Equal(deadline) {
		t.Fatal("restart extended countdown")
	}
	exec("UPDATE game_rounds SET betting_closes_at=clock_timestamp()-interval '1 second' WHERE id=$1", round.ID)
	// Exercise the existing fairness path, rather than pre-setting a crash point.
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	running, err := repo.GetRunningRound(ctx)
	if err != nil || running == nil || running.CrashPoint == nil {
		t.Fatal("fairness/start did not prepare round")
	}
	expected, err := service.fairnessService.GenerateCrashPoint(*running.ServerSeed, running.ClientSeed, running.Nonce)
	if err != nil || !expected.CrashPoint.Equal(*running.CrashPoint) {
		t.Fatal("existing fairness changed")
	}
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '100 hours' WHERE id=$1", round.ID)
	exec(`CREATE FUNCTION fail_settlement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='SETTLED' THEN RAISE EXCEPTION 'test settlement unavailable'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER fail_settlement BEFORE UPDATE ON game_rounds FOR EACH ROW EXECUTE FUNCTION fail_settlement()`)
	before := len(events.events)
	if err := engine.recoverStep(ctx); err == nil {
		t.Fatal("expected settlement failure")
	}
	if len(events.events) != before+1 || events.events[before].Type != realtime.EventRoundCrashed {
		t.Fatalf("events during failed settlement: %+v", events.events[before:])
	}
	if _, err := m.Current(round.ID); err == nil {
		t.Fatal("multiplier running during failed settlement")
	}
	if next, _ := repo.GetUpcomingRound(ctx); next != nil {
		t.Fatal("opened betting before settlement succeeded")
	}
	exec("DROP TRIGGER fail_settlement ON game_rounds")
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	next, _ := repo.GetUpcomingRound(ctx)
	if next == nil || next.Status != RoundBettingOpen {
		t.Fatal("failed to recover settlement and open countdown")
	}
}

func TestBackendAutomationPersistsAndExecutesOnce(t *testing.T) {
	db := lifecycleDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(db)
	service := NewService(repo, fairness.NewService(fairness.Config{HouseEdge: decimal.RequireFromString("0.03")}))
	m := multiplier.NewService()
	events := &lifecycleEvents{}
	engine := NewEngine(service, m, settlement.NewService(db, settlement.NewRepository(db)), events, events, 5*time.Second, 0.08)
	exec("INSERT INTO users(username,email,password_hash,balance) VALUES('auto','auto@test','unused',10000)")
	w := wallet.NewService(db)
	bets := betting.NewService(db, betting.NewRepository(db), w)
	cash := cashout.NewService(db, cashout.NewRepository(db), w, m)
	target := "1.10"
	settings, err := bets.SaveAuto(ctx, 1, 1, betting.AutoInput{Enabled: true, Amount: "100.00", AutoCashout: &target})
	if err != nil || !settings[0].Enabled {
		t.Fatalf("settings: %v", err)
	}
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	// No frontend or HTTP request places these bets. Concurrent workers share one attempt.
	errors := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() { errors <- bets.ProcessAutomatic(ctx) }()
	}
	for i := 0; i < 5; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var balance string
	var id, roundID int64
	if err = db.QueryRow(ctx, "SELECT count(*) FROM bets").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate auto bet: %d %v", count, err)
	}
	if err = db.QueryRow(ctx, "SELECT balance::text FROM users WHERE id=1").Scan(&balance); err != nil || balance != "9900.00" {
		t.Fatalf("wallet debited incorrectly: %s %v", balance, err)
	}
	if err = db.QueryRow(ctx, "SELECT id,round_id FROM bets WHERE is_auto AND auto_cashout_multiplier=1.10").Scan(&id, &roundID); err != nil {
		t.Fatal(err)
	}
	if err = cash.ProcessAutomatic(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	db.QueryRow(ctx, "SELECT status FROM bets WHERE id=$1", id).Scan(&status)
	if status != "ACTIVE" {
		t.Fatal("auto cashout during countdown")
	}
	exec("UPDATE game_rounds SET betting_closes_at=clock_timestamp()-interval '1 second',crash_point=10 WHERE id=$1", roundID)
	if err = engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '2 seconds' WHERE id=$1", roundID)
	for i := 0; i < 5; i++ {
		go func() { errors <- cash.ProcessAutomatic(ctx) }()
	}
	for i := 0; i < 5; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	var payout, source string
	if err = db.QueryRow(ctx, "SELECT status,payout::text,cashout_source FROM bets WHERE id=$1", id).Scan(&status, &payout, &source); err != nil || status != "CASHED_OUT" || payout != "110.00" || source != "AUTO" {
		t.Fatalf("auto cashout: %s %s %s %v", status, payout, source, err)
	}
	db.QueryRow(ctx, "SELECT count(*) FROM wallet_transactions WHERE type='WIN'").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate payout")
	}
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '1 hour' WHERE id=$1", roundID)
	if err = engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	// Recreate the service to prove automation survives process-local state loss.
	bets = betting.NewService(db, betting.NewRepository(db), w)
	if err = bets.ProcessAutomatic(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "SELECT count(*) FROM bets").Scan(&count)
	if count != 2 {
		t.Fatal("auto settings did not survive restart")
	}
	// Stop prevents future attempts, without cancelling an already accepted bet.
	if _, err = bets.SaveAuto(ctx, 1, 1, betting.AutoInput{Enabled: false, Amount: "100.00", AutoCashout: &target}); err != nil {
		t.Fatal(err)
	}
	next, _ := repo.GetUpcomingRound(ctx)
	exec("UPDATE game_rounds SET betting_closes_at=clock_timestamp()-interval '1 second',crash_point=1.01 WHERE id=$1", next.ID)
	if err = engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE game_rounds SET started_at=clock_timestamp()-interval '1 hour' WHERE id=$1", next.ID)
	if err = cash.ProcessAutomatic(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "SELECT status FROM bets WHERE round_id=$1", next.ID).Scan(&status)
	if status != "ACTIVE" {
		t.Fatal("automatic payout past crash deadline")
	}
	if err = engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	if err = bets.ProcessAutomatic(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(ctx, "SELECT count(*) FROM bets").Scan(&count)
	if count != 2 {
		t.Fatal("disabled automatic bets still executing")
	}
	// Insufficient funds disable automation; failed transactions leave no debit.
	exec("UPDATE users SET balance=0 WHERE id=1")
	if _, err = bets.SaveAuto(ctx, 1, 1, betting.AutoInput{Enabled: true, Amount: "100.00"}); err != nil {
		t.Fatal(err)
	}
	if err = bets.ProcessAutomatic(ctx); err != nil {
		t.Fatal(err)
	}
	settings, err = bets.GetAuto(ctx, 1)
	if err != nil || settings[0].Enabled || settings[0].LastError == "" {
		t.Fatal("failed auto bet was not disabled")
	}
}

func TestPlayerAutomationAPIAndServerBetValues(t *testing.T) {
	db := lifecycleDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "INSERT INTO users(username,email,password_hash,balance) VALUES('one','one@test','hidden',10000),('two','two@test','hidden',10000)"); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	gameService := NewService(repo, fairness.NewService(fairness.Config{HouseEdge: decimal.RequireFromString("0.03")}))
	events := &lifecycleEvents{}
	m := multiplier.NewService()
	engine := NewEngine(gameService, m, settlement.NewService(db, settlement.NewRepository(db)), events, events, 5*time.Second, 0.08)
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	w := wallet.NewService(db)
	bets := betting.NewService(db, betting.NewRepository(db), w)
	handler := betting.NewHandler(bets)
	authService := auth.NewService(db, "test-secret")
	mux := http.NewServeMux()
	mux.Handle("GET /api/bets/auto", authService.Middleware(http.HandlerFunc(handler.AutoSettings)))
	mux.Handle("PUT /api/bets/auto/{panel}", authService.Middleware(http.HandlerFunc(handler.AutoSettings)))
	mux.Handle("GET /api/bets", authService.Middleware(http.HandlerFunc(history.NewHandler(db).Bets)))
	request := func(method, path, body string, user int64) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if user > 0 {
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": user, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("test-secret"))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, r)
		return response
	}
	if got := request("GET", "/api/bets/auto", "", 0); got.Code != 401 {
		t.Fatalf("unauthenticated automation: %d", got.Code)
	}
	if got := request("PUT", "/api/bets/auto/1", `{"user_id":2,"enabled":true,"amount":"100"}`, 1); got.Code != 400 {
		t.Fatalf("accepted another owner: %d", got.Code)
	}
	if got := request("PUT", "/api/bets/auto/1", `{"enabled":true,"amount":"100","auto_cashout_multiplier":"1.25"}`, 1); got.Code != 200 {
		t.Fatalf("save: %d %s", got.Code, got.Body.String())
	}
	other := request("GET", "/api/bets/auto", "", 2)
	var settings []betting.AutoSetting
	if err := json.Unmarshal(other.Body.Bytes(), &settings); err != nil || settings[0].Enabled {
		t.Fatal("settings leaked across users")
	}
	round, _ := repo.GetUpcomingRound(ctx)
	if _, err := bets.PlaceBet(ctx, 1, round.ID, 2, decimal.NewFromInt(100), decimal.RequireFromString("1.25")); err != nil {
		t.Fatal(err)
	}
	response := request("GET", "/api/bets", "", 1)
	var values []history.PlayerBet
	if err := json.Unmarshal(response.Body.Bytes(), &values); err != nil || len(values) != 1 || !values[0].CanCancel || values[0].CanCashout {
		t.Fatalf("bet action values: %s %v", response.Body.String(), err)
	}
	if strings.Contains(response.Body.String(), "server_seed") || strings.Contains(response.Body.String(), "crash_point") || strings.Contains(response.Body.String(), "password") {
		t.Fatal("private data in player response")
	}
	if _, err := db.Exec(ctx, "UPDATE game_rounds SET betting_closes_at=clock_timestamp()-interval '1 second',crash_point=10 WHERE id=$1", round.ID); err != nil {
		t.Fatal(err)
	}
	if err := engine.recoverStep(ctx); err != nil {
		t.Fatal(err)
	}
	response = request("GET", "/api/bets", "", 1)
	if err := json.Unmarshal(response.Body.Bytes(), &values); err != nil || !values[0].CanCashout || values[0].CanCancel || values[0].PotentialPayout == "0.00" {
		t.Fatalf("server payout: %s %v", response.Body.String(), err)
	}
	if got := request("GET", "/api/bets", "", 2); got.Body.String() != "[]\n" {
		t.Fatal("bets leaked across users")
	}
	verifier := NewHandler(gameService)
	check := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		verifier.Verify(response, httptest.NewRequest("GET", fmt.Sprintf("/api/game/rounds/%d/verify", round.ID), nil))
		return response
	}
	if response := check(); response.Code != 409 {
		t.Fatal("verification exposed an active round")
	}
	// Restore the generated fairness result after the fixed crash point used above.
	original, _ := repo.GetRoundByID(ctx, round.ID)
	fair, err := gameService.fairnessService.GenerateCrashPoint(*original.ServerSeed, original.ClientSeed, original.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE game_rounds SET status='SETTLED',crash_point=$2 WHERE id=$1", round.ID, fair.CrashPoint.String()); err != nil {
		t.Fatal(err)
	}
	if response := check(); response.Code != 200 || !strings.Contains(response.Body.String(), "\"verified\":true") {
		t.Fatalf("verification: %d %s", response.Code, response.Body.String())
	}

}

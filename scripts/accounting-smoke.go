// Run with local PostgreSQL, migrations and backend active:
// go run ./scripts/accounting-smoke.go
package main

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/betting"
	"aviator/backend/internal/config"
	"aviator/backend/internal/database"
	"aviator/backend/internal/deposit"
	"aviator/backend/internal/wallet"
	withdrawal "aviator/backend/internal/withdraw"
	"context"
	"fmt"
	"github.com/shopspring/decimal"
	"sync"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg := config.Load()
	db, err := database.NewPostgres(cfg.DatabaseURL)
	must(err)
	defer db.Close()
	name := fmt.Sprintf("accounting%d", time.Now().UnixNano())
	user, err := auth.NewService(db, cfg.JWTSecret).Register(ctx, name, name+"@example.test", "LocalSandbox123!")
	must(err)
	deposits := deposit.NewService(deposit.NewRepository(db))
	wallets := wallet.NewService(db)
	d, err := deposits.Create(ctx, user.ID, deposit.CreateRequest{Amount: "100.00", Provider: "SANDBOX"})
	must(err)
	concurrent(func() error {
		_, err := deposits.CompleteByProviderReference(ctx, d.Provider, d.ProviderReference)
		return err
	})
	balance, err := wallets.GetBalance(ctx, user.ID)
	must(err)
	check(balance.Equal(decimal.NewFromInt(100)), "duplicate deposit credit")
	withdrawals := withdrawal.NewService(withdrawal.NewRepository(db))
	w, err := withdrawals.Create(ctx, user.ID, withdrawal.CreateRequest{Amount: "50.00", Provider: "SANDBOX"})
	must(err)
	concurrent(func() error { _, err := withdrawals.FailAndRefund(ctx, w.Withdrawal.ID); return err })
	balance, err = wallets.GetBalance(ctx, user.ID)
	must(err)
	check(balance.Equal(decimal.NewFromInt(100)), "duplicate withdrawal refund")
	var roundID int64
	for {
		err = db.QueryRow(ctx, "SELECT id FROM game_rounds WHERE status='BETTING_OPEN' AND betting_closes_at>clock_timestamp()+interval '2 seconds' LIMIT 1").Scan(&roundID)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	bets := betting.NewService(db, betting.NewRepository(db), wallets)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := int16(1); i <= 2; i++ {
		wg.Add(1)
		go func(number int16) {
			defer wg.Done()
			_, err := bets.PlaceBet(ctx, user.ID, roundID, number, decimal.NewFromInt(75))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	check(success == 1, "concurrent bets did not enforce available balance")
	balance, err = wallets.GetBalance(ctx, user.ID)
	must(err)
	check(balance.Equal(decimal.NewFromInt(25)), "wallet overspent")
	var depositsCount, refundsCount int
	must(db.QueryRow(ctx, "SELECT count(*) FILTER (WHERE type='DEPOSIT'),count(*) FILTER (WHERE type='REFUND') FROM wallet_transactions WHERE user_id=$1", user.ID).Scan(&depositsCount, &refundsCount))
	check(depositsCount == 1 && refundsCount == 1, "duplicate audit entries")
	fmt.Println("PASS: concurrent deposit completion, withdrawal failure/refund, simultaneous spending and unique audit entries")
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func concurrent(fn func() error) {
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- fn() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
}

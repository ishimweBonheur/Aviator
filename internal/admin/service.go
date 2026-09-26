package admin

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"strings"

	"aviator/backend/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type StatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type AdjustmentRequest struct {
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

var moneyPattern = regexp.MustCompile(`^[0-9]{1,16}(\.[0-9]{1,2})?$`)

func (repo *Repository) status(ctx context.Context, actor, id int64, input StatusRequest) error {
	if input.Status != "ACTIVE" && input.Status != "SUSPENDED" && input.Status != "BLOCKED" {
		return invalid("invalid status")
	}
	if len(strings.TrimSpace(input.Reason)) < 3 || len(input.Reason) > 500 {
		return invalid("reason must contain 3 to 500 characters")
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role, before string
	if err = tx.QueryRow(ctx, "SELECT role,status FROM users WHERE id=$1 FOR UPDATE", id).Scan(&role, &before); errors.Is(err, pgx.ErrNoRows) {
		return invalid("user not found")
	} else if err != nil {
		return err
	}
	if role == "ADMIN" {
		return invalid("administrator account status must be managed by a database operator")
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET status=$1,updated_at=now() WHERE id=$2", input.Status, id); err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]string{"before": before, "after": input.Status, "reason": input.Reason})
	if _, err = tx.Exec(ctx, "INSERT INTO admin_audit_logs(admin_id,user_id,action,details) VALUES($1,$2,'STATUS',$3)", actor, id, detail); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repo *Repository) adjust(ctx context.Context, actor, id int64, input AdjustmentRequest) error {
	if !moneyPattern.MatchString(input.Amount) {
		return invalid("amount must be a positive decimal string with at most two decimal places")
	}
	amount, err := decimal.NewFromString(input.Amount)
	if err != nil || !amount.IsPositive() || !amount.Equal(amount.Round(2)) || amount.GreaterThan(decimal.RequireFromString("9999999999999999.99")) {
		return invalid("amount must be positive with at most two decimal places")
	}
	if input.Direction != "CREDIT" && input.Direction != "DEBIT" {
		return invalid("invalid direction")
	}
	if len(strings.TrimSpace(input.Reason)) < 3 || len(input.Reason) > 500 {
		return invalid("reason must contain 3 to 500 characters")
	}
	if len(input.Reference) < 8 || len(input.Reference) > 80 {
		return invalid("reference must contain 8 to 80 characters")
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize retries before looking up the idempotency record.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "admin:"+input.Reference); err != nil {
		return err
	}
	var priorActor, priorUser int64
	var previous []byte
	err = tx.QueryRow(ctx, "SELECT admin_id,user_id,details FROM admin_audit_logs WHERE reference=$1", input.Reference).Scan(&priorActor, &priorUser, &previous)
	if err == nil {
		var old AdjustmentRequest
		if json.Unmarshal(previous, &old) != nil {
			return invalid("failed to read adjustment")
		}
		if priorActor != actor || priorUser != id || old != input {
			return invalid("reference already used for another adjustment")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var balanceString string
	if err = tx.QueryRow(ctx, "SELECT balance::text FROM users WHERE id=$1 FOR UPDATE", id).Scan(&balanceString); errors.Is(err, pgx.ErrNoRows) {
		return invalid("user not found")
	} else if err != nil {
		return err
	}
	balance, err := decimal.NewFromString(balanceString)
	if err != nil {
		return err
	}
	if input.Direction == "DEBIT" && balance.LessThan(amount) {
		return invalid("insufficient balance")
	}
	if input.Direction == "CREDIT" && balance.Add(amount).GreaterThan(decimal.RequireFromString("9999999999999999.99")) {
		return invalid("invalid resulting balance")
	}
	service := wallet.NewService(repo.db)
	if input.Direction == "CREDIT" {
		err = service.CreditTx(ctx, tx, id, amount, "ADJUSTMENT", "admin:"+input.Reference)
	} else {
		err = service.DebitTx(ctx, tx, id, amount, "ADJUSTMENT", "admin:"+input.Reference)
	}
	if err != nil {
		return err
	}
	detail, _ := json.Marshal(input)
	if _, err = tx.Exec(ctx, "INSERT INTO admin_audit_logs(admin_id,user_id,action,reference,details) VALUES($1,$2,'WALLET_ADJUSTMENT',$3,$4)", actor, id, input.Reference, detail); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

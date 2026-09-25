package database

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type leaderConnectionKey struct{}
type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func WithLeaderConnection(ctx context.Context, conn *pgx.Conn) context.Context {
	return context.WithValue(ctx, leaderConnectionKey{}, conn)
}
func Query(ctx context.Context, pool *pgxpool.Pool) Querier {
	if conn, ok := ctx.Value(leaderConnectionKey{}).(Querier); ok {
		return conn
	}
	return pool
}

func WithQuery(ctx context.Context, q Querier) context.Context {
	return context.WithValue(ctx, leaderConnectionKey{}, q)
}
func Begin(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	if conn, ok := ctx.Value(leaderConnectionKey{}).(*pgx.Conn); ok {
		return conn.Begin(ctx)
	}
	return pool.Begin(ctx)
}

# Administration

The existing frontend at `../Aviator-frontend` now serves `/admin`. The game screen and existing account/session handling remain in place. All dashboard data comes from authenticated APIs; there are no demo administrative metrics.

## Local setup

From `P:\Aviator`, run `docker compose up --build -d`. The migration service applies `000001_initial_schema.up.sql`, which includes round timing, `users.role` (default `PLAYER`), and `admin_audit_logs`. Alternatively, with `DATABASE_URL` configured, run `go run ./cmd/migrate` before starting the updated server.

The migration history is consolidated into initial up/down files only. An existing database that already applied the former timing and admin migrations has the same schema but version 3 in `schema_migrations`. After verifying that all those changes are present and the version is clean, rebaseline its migration metadata to version 1; do not rerun the initial SQL over existing tables. Databases missing any of those changes must be upgraded before rebaselining. The initial down migration removes all application tables.

Register an account through the existing frontend. A database operator can promote that account for local development:

```sql
UPDATE users SET role = 'ADMIN', updated_at = NOW()
WHERE email = 'your-admin@example.com' AND status = 'ACTIVE';
```

Use your actual account email. No default admin credentials or public promotion endpoint are created. Sign in again so the game header shows the Admin dashboard link, or open `http://localhost:5173/admin` directly. Users without an active ADMIN database role receive 403. Role revocation applies to existing JWTs, and suspended/blocked accounts are denied authenticated operations.

## Pages and APIs

| Frontend | API |
| --- | --- |
| `/admin` | `GET /api/admin/overview`, `GET /api/admin/analytics` |
| `/admin/users` | `GET /api/admin/users` |
| `/admin/users/:id` | `GET /api/admin/users/{id}` |
| `/admin/bets` | `GET /api/admin/bets` |
| `/admin/rounds` | `GET /api/admin/rounds` |
| `/admin/rounds/:id` | `GET /api/admin/rounds/{id}` |
| `/admin/deposits` | `GET /api/admin/deposits` |
| `/admin/withdrawals` | `GET /api/admin/withdrawals` |
| `/admin/wallet` | `GET /api/admin/wallet-transactions` |
| `/admin/analytics` | `GET /api/admin/analytics` |
| `/admin/system` | `GET /api/admin/game/status`, `GET /api/admin/game/current` |
| `/admin/config` | `GET /api/admin/config` |

The guard verifies `GET /api/admin/session`. Every admin route uses the existing JWT middleware followed by a current database role check. Login/register responses add `Role` without changing the existing uppercase identity fields. The frontend reuses `gameService.accountRequest` and its session-expiry handling.

Lists return `{items, page, page_size, total}`, ordered by descending ID. Page sizes default to 25 and are capped at 100. Filters include `status`, `user_id`, `round_id`, user `search`, wallet `type`/`reference`, and RFC3339 `from`/`to` where relevant. From is inclusive and to is exclusive. The date picker uses local time and submits UTC timestamps. Count and page are selected in one database statement.

`PATCH /api/admin/users/{id}/status` accepts `{status, reason}`. Admin account statuses must be managed by a database operator to prevent accidental administrator lockouts. `POST /api/admin/users/{id}/wallet-adjustments` accepts `{direction: "CREDIT" | "DEBIT", amount: "10.25", reason, reference}`. A review modal requires explicit confirmation before either action. Wallet adjustments use the existing decimal wallet service inside a transaction, locking the user and atomically recording the ledger and audit entry. Reuse the reference and exact payload after an uncertain result; concurrent retries apply once. Reusing a reference with a different payload is rejected. Neither page contains provider-success simulation controls.

## Metrics and monitoring

Realized wagers and payouts include only LOST/CASHED_OUT bets from SETTLED rounds, attributed to the round's end time. Cancelled/refunded bets and unresolved stakes are excluded. GGR is wagers minus payouts; RTP is payouts divided by wagers times 100; house margin is GGR divided by wagers times 100. Both percentages are zero with zero wagers. All API money/ratio values are decimal strings. Daily chart buckets use UTC; charts convert strings to numbers only for display. The adjacent tables retain exact strings.

Completed deposits and withdrawals use payment completion time and are shown separately from gaming revenue. Player wallet liability is the sum of account balances. Active users means accounts with ACTIVE status; analytics active players counts distinct participating players during the requested period. Overview cards are all-time, while date filters select the charts' period.

The round monitor uses the existing game WebSocket state and countdown, with a clearly marked last-server-snapshot fallback. Overview, rounds, and system data refresh every 10 seconds. Redis supplies health and lease-presence checks; PostgreSQL remains authoritative for all money. WebSocket count is scoped to the responding instance. Lease presence does not prove the engine is progressing. Runtime configuration is read-only and excludes credentials. Server seeds are visible only after settlement, and crash points only after crash.

## Validation

```powershell
go test ./...
go build ./...
swag init -g cmd/main.go
$env:ADMIN_TEST_DATABASE_URL = 'postgres://aviator:aviator_password@localhost:5434/aviator?sslmode=disable'
go test ./internal/admin -v
docker compose config --quiet
docker compose build
```

Integration tests create and remove a uniquely named schema without changing application records. They cover authorization, role revocation, all read endpoints, concurrent adjustment idempotency, overdraft rejection, analytics exclusion rules, pagination, and fairness redaction.

In `P:\Aviator-frontend`, run `npm run build`, `npm run lint`, and `npm test`. Admin browser tests use mocked API responses to exercise routing, denied access, responsive layout, filters, pagination, and confirmed financial/status requests. API integration tests use real PostgreSQL.

This change implements administration. Auto Bet and automatic cashout from the broader project requirements are not implemented by this dashboard change; this backend checkout does not yet provide their APIs or columns. The admin bets table displays the fields currently stored by the backend rather than inventing automatic/manual values.

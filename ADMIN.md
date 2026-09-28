# Administration

The existing frontend at `../Aviator-frontend` now serves `/admin`. The game screen and existing account/session handling remain in place. All dashboard data comes from authenticated APIs; there are no demo administrative metrics.

## Local setup

From `P:\Aviator`, run `docker compose up --build -d`. The migration service applies `000001_initial_schema.up.sql`, which includes round timing, automatic betting, `users.role` (default `PLAYER`), and `admin_audit_logs`. Alternatively, with `DATABASE_URL` configured, run `go run ./cmd/migrate` before starting the updated server.

The initial up/down pair is the only migration. For an existing database, verify that it already has every object in the consolidated baseline, then set its clean `schema_migrations` version to 1 before running the new migration service:

```sql
UPDATE schema_migrations SET version = 1 WHERE version > 1 AND dirty = false;
```

Do not rerun the initial SQL over existing tables or rebaseline a database missing any baseline objects. The initial down migration removes all application tables.

Normal registration always creates a PLAYER. An existing active administrator can assign another user's role or create an additional administrator from `/admin/users`. For initial setup only, a database operator can promote the first account:

```sql
UPDATE users SET role = 'ADMIN', updated_at = NOW()
WHERE email = 'your-admin@example.com' AND status = 'ACTIVE';
```

The initial database promotion is the bootstrap exception; after that, role changes and admin creation are available only to active ADMIN accounts. Players cannot promote themselves, create administrators, or access `/api/admin/*`. Administrators cannot change their own role, and the last active administrator cannot be demoted. Role changes and admin creation are audited. Role revocation applies to existing JWTs, and suspended/blocked accounts are denied authenticated operations.

## Pages and APIs

| Frontend | API |
| --- | --- |
| `/admin` | `GET /api/admin/overview`, `GET /api/admin/analytics` |
| `/admin/users` | `GET /api/admin/users`, `PATCH /api/admin/users/{id}/role`, `POST /api/admin/users/admins` |
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

`PATCH /api/admin/users/{id}/role` accepts `{role: "PLAYER" | "ADMIN"}` and cannot target the acting administrator. `POST /api/admin/users/admins` accepts `{username, email, password}` and creates an active ADMIN with a bcrypt-hashed password. Both operations are transactional and audited. `PATCH /api/admin/users/{id}/status` accepts `{status, reason}`. Admin account statuses must be managed by a database operator to prevent accidental administrator lockouts. `POST /api/admin/users/{id}/wallet-adjustments` accepts `{direction: "CREDIT" | "DEBIT", amount: "10.25", reason, reference}`. A review modal requires explicit confirmation before these actions. Wallet adjustments use the existing decimal wallet service inside a transaction, locking the user and atomically recording the ledger and audit entry. Reuse the reference and exact payload after an uncertain result; concurrent retries apply once. Reusing a reference with a different payload is rejected. Neither page contains provider-success simulation controls.

## Metrics and monitoring

Realized wagers and payouts include only LOST/CASHED_OUT bets from SETTLED rounds, attributed to the round's end time. Cancelled/refunded bets and unresolved stakes are excluded. GGR is wagers minus payouts; RTP is payouts divided by wagers times 100; house margin is GGR divided by wagers times 100. Both percentages are zero with zero wagers. All API money/ratio values are decimal strings. Daily chart buckets use UTC; charts convert strings to numbers only for display. The adjacent tables retain exact strings.

Completed deposits and withdrawals use payment completion time and are shown separately from gaming revenue. Player wallet liability is the sum of PLAYER account balances, excluding ADMIN balances. Active users means accounts with ACTIVE status; analytics active players counts distinct participating players during the requested period. Overview cards are all-time, while date filters select the charts' period.

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

Automatic bets and automatic cashouts execute on the backend. The dashboard adds `/admin/auto-bets`, `/admin/auto-cashouts`, and `/admin/audit-logs`, backed by authenticated ADMIN-only list APIs with filters and pagination.

The initial migration includes the automatic settings table and bet target/source columns. Workers run on each backend instance; row locks, transactional settings/attempt records and unique bet/ledger keys prevent duplicate debits and payouts. Automatic cashout pays the persisted target only if processed before the authoritative crash boundary. Server downtime or processing delay can cause a missed target; there are no retroactive payouts.

Player APIs: `GET /api/bets/auto`, `PUT /api/bets/auto/{panel}`, optional `auto_cashout_multiplier` on `POST /api/bets`, server-calculated `potential_payout`, `can_cancel`, and `can_cashout` on `GET /api/bets`. Settings continue without a browser until disabled; failed automatic bets disable their setting with a public error.

Current-game snapshots expose `phase` and `seconds_remaining`. Fairness verification uses `GET /api/game/rounds/{id}/verify` after settlement. Generated Swagger includes these contracts and the new administrative routes.

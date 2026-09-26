# Local Aviator system

The backend is in `P:\Aviator`; the existing React/Vite frontend is in `P:\Aviator-frontend`. PostgreSQL owns money and round decisions. Redis distributes public events and coordinates engine ownership. The frontend displays acknowledged API results and server events.

## Start

```powershell
cd P:\Aviator
docker compose up --build -d
```

Compose starts PostgreSQL 18, Redis 7, a one-shot migration container, the backend and the frontend. It retains the existing `aviator_postgres_data` volume. Migration failure prevents backend startup. Never run `docker compose down -v` to restart this system.

Default URLs: frontend `http://localhost:5173`, backend `http://localhost:7000`, Swagger `http://localhost:7000/swagger/index.html`. PostgreSQL uses host port 5434, Redis uses host port 6380 (another local project already occupies 6379). Inside Compose Redis remains `redis:6379`. Set `FRONTEND_PORT` or `REDIS_PORT` to change host ports. Stop an existing native backend before starting the Compose backend.

For native development, copy `.env.example` to `.env` only if you do not already have one, start `docker compose up -d postgres redis`, run `go run ./cmd/migrate`, then `go run ./cmd`. In the frontend folder run `npm ci` and `npm run dev`. Vite proxies `/api`, `/health`, `/ws`; the frontend Docker image uses Nginx for the same paths. Existing frontend design and framework are retained.

## Configuration

Existing `APP_PORT`, `DATABASE_URL`, `JWT_SECRET`, `HOUSE_EDGE` remain supported; house edge defaults to **0**, with the existing fairness formula unchanged.

Added settings and defaults:

| Setting | Default |
| --- | --- |
| REDIS_ADDR | localhost:6380 (Compose: redis:6379) |
| REDIS_PASSWORD | empty for local Redis |
| REDIS_DB | 0 |
| BETTING_WINDOW_SECONDS | 5 |
| MULTIPLIER_GROWTH_RATE | 0.08 |
| MIN_BET_AMOUNT / MAX_BET_AMOUNT | 50 / 1000000 |
| MAX_PAYOUT | 100000000 |
| MIN_DEPOSIT_AMOUNT / MAX_DEPOSIT_AMOUNT | 1 / 10000000 |
| MIN_WITHDRAWAL_AMOUNT / MAX_WITHDRAWAL_AMOUNT | 1 / 10000000 |

Limits require positive amounts with at most two decimals and consistent ranges. The database's minimum bet remains 50. Cashout payout is capped at MAX_PAYOUT, which the UI displays. Growth rate and house edge are saved per new round, so changing configuration does not alter an existing flight or its verification. Legacy rounds have no recorded house edge and show that limitation in the verification UI.

## Game lifecycle and recovery

One running round and one upcoming round are maintained. The upcoming round opens immediately while the running round flies. Its betting deadline is durable; after five seconds it closes and may wait for the current flight to settle before promotion. The UI displays both the flying round and the upcoming countdown. A closed upcoming round cannot accept bets or cancellations even if the engine is temporarily offline.

Every engine pass resumes from PostgreSQL: CREATED opens; BETTING_OPEN resumes its remaining deadline; BETTING_CLOSED generates a missing crash point and starts when no flight is running; RUNNING continues from its original timestamp or crashes immediately if expired; CRASHED settles remaining ACTIVE bets; SETTLED is complete. Settlement does not overwrite CASHED_OUT or CANCELLED bets.

The crash boundary is `started_at + ln(crash_point)/growth_rate`. Cashout uses the shared multiplier Clock after obtaining round, bet and wallet locks. Equality or later fails even if PostgreSQL still says RUNNING because the display ticker has not observed the crash. The ticker controls display updates only. User-visible multipliers use exactly two decimal places; stored crash points retain four.

The initial schema includes betting timestamps, per-round growth rate and house edge, and unique partial indexes for one RUNNING and one upcoming CREATED/BETTING_OPEN/BETTING_CLOSED round. Timing and admin migrations have been consolidated into the initial up/down pair. See `ADMIN.md` for existing-database migration metadata guidance.

## Redis and leadership

| Key/channel | Purpose |
| --- | --- |
| aviator:engine:leader | unique token lease, 10s TTL, renewed every 2s |
| aviator:events | Pub/Sub channel for public events |
| aviator:round:current | ephemeral public running metadata and multiplier, 30s TTL |
| aviator:round:upcoming | ephemeral public upcoming metadata, 30s TTL |
| aviator:bet-action:{id} | 5s duplicate-action suppression for cashout/cancel |

Lease renewal and release compare the ownership token in Lua. Any renewal uncertainty cancels the engine. A PostgreSQL session advisory lock is an additional fence: all engine lifecycle writes and settlement use that same locked database connection, preventing a paused process from writing through a fresh pool connection after losing its DB session. Followers serve HTTP and WebSockets. Only the leader starts the engine.

The one-way event path is service/engine -> Redis Publish -> each instance's subscription -> local WebSocket hub. Hubs never republish. Pub/Sub reconnects; it is best-effort and has no replay. Bounded client queues evict slow consumers. `/api/game/rounds/current` and authenticated history reads reconcile after reconnect or missed events. Redis snapshots intentionally omit future crash time because it would disclose the outcome. PostgreSQL remains the snapshot authority.

Redis outages do not invalidate completed accounting transactions. Short-lived action locks fail open to PostgreSQL's transaction/state checks. Leadership fails closed. Events missed during an outage are recovered through reads, not replayed.

## Public API and Swagger

All authenticated endpoints use `Authorization: Bearer <JWT>`.

| Method | Route | Use |
| --- | --- | --- |
| POST | /api/auth/register | register |
| POST | /api/auth/login | JWT and profile |
| GET | /api/wallet/balance | own wallet |
| GET | /api/wallet/transactions | latest 200 own ledger rows |
| POST, GET | /api/deposits | create / latest 50 own deposits |
| POST, GET | /api/withdrawals | reserve funds / latest 50 own withdrawals |
| POST | /api/bets | place bet #1 or #2 |
| GET | /api/bets | latest 200 own bets, including round status |
| POST | /api/bets/{id}/cancel | atomic refund while open |
| POST | /api/bets/{id}/cashout | authoritative time-checked cashout |
| GET | /api/game/rounds/current | running + upcoming + current_multiplier + server_time |
| GET | /api/game/rounds | latest 50 settled rounds |
| GET | /api/game/rounds/{id} | public round |
| GET | /api/game/rounds/{id}/fairness | commitment, or settled reveal |
| GET | /api/limits | configured limits |
| GET | /ws | public WebSocket upgrade |
| GET | /health | HTTP health |

The old `POST /api/bets/{id}` route is replaced by `/cashout`. Public manual create/open/close/start/crash/settle and settlement routes are unregistered; the engine retains internal services. Seeds are redacted at JSON serialization until SETTLED, and future crash points are redacted until CRASHED. SQL diagnostics are logged server-side and sanitized at player-facing handlers.

Events: ROUND_OPENED, COUNTDOWN (including zero), ROUND_STARTED, MULTIPLIER_UPDATE, BET_PLACED, BET_CANCELLED, BET_CASHED_OUT, ROUND_CRASHED, ROUND_SETTLED. Events include timestamps and round identity. Decimal multiplier/crash values are strings. No client WebSocket message changes game state. WebSocket origin checks remain development-only permissive.

SANDBOX deposit creation, completion, wallet credit and DEPOSIT audit entry commit together. Reprocessing its provider reference does not credit again. MTN_MOMO creates PENDING only. The server generates references and rejects a client-supplied provider_reference. Withdrawals atomically deduct/reserve funds, create PENDING and a WITHDRAWAL audit entry. Internal completion and failure/refund remain idempotent; there is no player-controlled completion endpoint.

## Frontend

The existing centralized API client now connects cancellation, explicit cashout, snapshot, history, limits and fairness. Auth uses the existing session-storage convention and handles expiry/logout. Betting panels distinguish upcoming bets from flying bets, display deadlines and configured payout caps, reconcile persisted state, and refresh balances after mutations. Wallet view offers SANDBOX/MTN_MOMO, pending withdrawals, deposit history and ledger. Round history opens a fairness view that hashes the revealed seed and reproduces the existing HMAC extraction/formula.

The explicit standalone demo remains a separate development mode; backend mode does not use its funds, players or results. Public leaderboard/player aggregates and server auto cashout are not invented; their UI remains unavailable. Requests that mutate funds are never automatically retried. A cancelled slot remains used for that round, matching the existing unique `(round_id,user_id,bet_number)` constraint.

## Validation

```powershell
go fmt ./...
go test ./...
go test -race ./...
go build ./...
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/main.go -o docs
docker compose config --quiet
docker compose build
node scripts/smoke.mjs
go run ./scripts/accounting-smoke.go
```

Frontend: `npm run build`, `npm run lint`, `npm test`, `node live-smoke.mjs`. Live smoke checks intentionally create distinct local SANDBOX test accounts and ledger records. Restart checks: stop native backends, build `go build -o aviator-local.exe ./cmd`, then `node scripts/recovery-smoke.mjs` on Windows. These are focused local checks, not a comprehensive testing project.

## Intentional limits

Real MTN MoMo integration, real external withdrawal-provider integration, a comprehensive new automated test suite, and full production deployment/security hardening are intentionally not implemented. SANDBOX funding is for local development. Redis Pub/Sub has no durable event replay. List endpoints are bounded recent history rather than a full pagination system. Browser fairness arithmetic is for inspection; the existing Go decimal algorithm remains authoritative.

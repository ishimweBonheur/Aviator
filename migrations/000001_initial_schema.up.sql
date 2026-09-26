-- USERS
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    role VARCHAR(20) NOT NULL DEFAULT 'PLAYER' CHECK (role IN ('PLAYER', 'ADMIN')),
    password_hash TEXT NOT NULL,
    balance NUMERIC(18, 2) NOT NULL DEFAULT 0.00 CHECK (balance >= 0.00),
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN (
            'ACTIVE',
            'SUSPENDED',
            'BLOCKED'
        )
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GAME ROUNDS
CREATE TABLE game_rounds (
    id BIGSERIAL PRIMARY KEY,
    round_number BIGINT NOT NULL UNIQUE,
    server_seed_hash CHAR(64) NOT NULL,
    -- Kept secret until the round is completed.
    server_seed TEXT,
    client_seed TEXT NOT NULL,
    nonce BIGINT NOT NULL,
    crash_point NUMERIC(12, 4),
    status VARCHAR(20) NOT NULL DEFAULT 'CREATED' CHECK (
        status IN (
            'CREATED',
            'BETTING_OPEN',
            'BETTING_CLOSED',
            'RUNNING',
            'CRASHED',
            'SETTLED'
        )
    ),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    betting_opened_at TIMESTAMPTZ,
    betting_closes_at TIMESTAMPTZ,
    growth_rate DOUBLE PRECISION NOT NULL DEFAULT 0.08 CHECK (growth_rate > 0 AND growth_rate < 100),
    house_edge NUMERIC,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- BETS
CREATE TABLE bets (
    id BIGSERIAL PRIMARY KEY,
    round_id BIGINT NOT NULL REFERENCES game_rounds(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    -- Each user can have bet #1 and bet #2 per round.
    bet_number SMALLINT NOT NULL CHECK (bet_number IN (1, 2)),
    amount NUMERIC(18, 2) NOT NULL CHECK (amount >= 50.00),
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN (
            'ACTIVE',
            'CASHED_OUT',
            'LOST',
            'CANCELLED'
        )
    ),
    cashout_multiplier NUMERIC(12, 4),
    payout NUMERIC(18, 2) NOT NULL DEFAULT 0.00 CHECK (payout >= 0.00),
    placed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cashed_out_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Prevent more than two bets per user per round.
    CONSTRAINT unique_user_round_bet UNIQUE (round_id, user_id, bet_number)
);

-- DEPOSITS
CREATE TABLE deposits (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0.00),
    provider VARCHAR(50) NOT NULL,
    provider_reference VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (
        status IN (
            'PENDING',
            'PROCESSING',
            'COMPLETED',
            'FAILED',
            'CANCELLED'
        )
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    -- Prevent the same provider payment from being processed twice.
    CONSTRAINT unique_deposit_provider_reference UNIQUE (provider, provider_reference)
);

-- WITHDRAWALS
CREATE TABLE withdrawals (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0.00),
    provider VARCHAR(50) NOT NULL,
    provider_reference VARCHAR(255),
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (
        status IN (
            'PENDING',
            'PROCESSING',
            'COMPLETED',
            'FAILED',
            'CANCELLED'
        )
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- WALLET TRANSACTIONS
CREATE TABLE wallet_transactions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    type VARCHAR(30) NOT NULL CHECK (
        type IN (
            'DEPOSIT',
            'BET',
            'WIN',
            'WITHDRAWAL',
            'REFUND',
            'ADJUSTMENT'
        )
    ),
    amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0.00),
    -- Unique internal reference for idempotency/auditing.
    reference VARCHAR(100) NOT NULL UNIQUE,
    balance_before NUMERIC(18, 2) NOT NULL CHECK (balance_before >= 0.00),
    balance_after NUMERIC(18, 2) NOT NULL CHECK (balance_after >= 0.00),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GAME EVENTS
CREATE TABLE game_events (
    id BIGSERIAL PRIMARY KEY,
    round_id BIGINT NOT NULL REFERENCES game_rounds(id),
    event_type VARCHAR(50) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}' :: JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ADMIN AUDIT LOGS
CREATE TABLE admin_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES users(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    action TEXT NOT NULL,
    reference TEXT UNIQUE,
    details JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- INDEXES
-- Bets
CREATE INDEX idx_bets_round_id ON bets(round_id);

CREATE INDEX idx_bets_user_id ON bets(user_id);

CREATE INDEX idx_bets_round_user ON bets(round_id, user_id);

-- Deposits
CREATE INDEX idx_deposits_user_id ON deposits(user_id);

CREATE INDEX idx_deposits_status ON deposits(status);

CREATE INDEX idx_deposits_created_at ON deposits(created_at);

-- Withdrawals
CREATE INDEX idx_withdrawals_user_id ON withdrawals(user_id);

CREATE INDEX idx_withdrawals_status ON withdrawals(status);

CREATE INDEX idx_withdrawals_created_at ON withdrawals(created_at);

-- Wallet transactions
CREATE INDEX idx_wallet_transactions_user_id ON wallet_transactions(user_id);

CREATE INDEX idx_wallet_transactions_created_at ON wallet_transactions(created_at);

-- Game events
CREATE INDEX idx_game_events_round_id ON game_events(round_id);

CREATE INDEX idx_game_events_created_at ON game_events(created_at);

-- Game rounds
CREATE INDEX idx_game_rounds_status ON game_rounds(status);

CREATE INDEX idx_game_rounds_created_at ON game_rounds(created_at);

CREATE UNIQUE INDEX one_running_round ON game_rounds ((1)) WHERE status = 'RUNNING';

CREATE UNIQUE INDEX one_upcoming_round ON game_rounds ((1)) WHERE status IN ('CREATED', 'BETTING_OPEN', 'BETTING_CLOSED');

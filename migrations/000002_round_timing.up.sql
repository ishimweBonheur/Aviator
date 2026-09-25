ALTER TABLE game_rounds ADD COLUMN betting_opened_at TIMESTAMPTZ;
ALTER TABLE game_rounds ADD COLUMN betting_closes_at TIMESTAMPTZ;
ALTER TABLE game_rounds ADD COLUMN growth_rate DOUBLE PRECISION NOT NULL DEFAULT 0.08 CHECK (growth_rate > 0 AND growth_rate < 100);
-- Preserve the original opening estimate for legacy in-flight rounds.
UPDATE game_rounds SET betting_opened_at = created_at, betting_closes_at = created_at + interval '5 seconds'
WHERE status IN ('BETTING_OPEN', 'BETTING_CLOSED');
CREATE UNIQUE INDEX one_running_round ON game_rounds ((1)) WHERE status = 'RUNNING';
CREATE UNIQUE INDEX one_upcoming_round ON game_rounds ((1)) WHERE status IN ('CREATED','BETTING_OPEN','BETTING_CLOSED');

ALTER TABLE game_rounds ADD COLUMN house_edge NUMERIC;

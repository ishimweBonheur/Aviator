DROP INDEX one_upcoming_round;
DROP INDEX one_running_round;
ALTER TABLE game_rounds DROP COLUMN betting_opened_at, DROP COLUMN betting_closes_at, DROP COLUMN growth_rate;

ALTER TABLE game_rounds DROP COLUMN house_edge;

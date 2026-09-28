ALTER TABLE bets DROP COLUMN cashout_source, DROP COLUMN is_auto, DROP COLUMN auto_cashout_multiplier;

DROP TABLE IF EXISTS auto_bet_settings;

DROP TABLE IF EXISTS game_events;

DROP TABLE IF EXISTS admin_audit_logs;

DROP TABLE IF EXISTS wallet_transactions;

DROP TABLE IF EXISTS withdrawals;

DROP TABLE IF EXISTS deposits;

DROP TABLE IF EXISTS bets;

DROP TABLE IF EXISTS game_rounds;

DROP TABLE IF EXISTS users;

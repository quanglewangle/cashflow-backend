-- Replaces the single-row "settings" (one fixed opening balance) with
-- "balance_checkpoints" (re-anchor any time from a real bank balance).
-- Run once against an already-deployed database that still has "settings":
--   psql cashflow -f migrations/001_balance_checkpoints.sql

CREATE TABLE balance_checkpoints (
    id           SERIAL PRIMARY KEY,
    period_year  SMALLINT NOT NULL,
    period_month SMALLINT NOT NULL CHECK (period_month BETWEEN 1 AND 12),
    balance      NUMERIC(10,2) NOT NULL,
    UNIQUE (period_year, period_month)
);
CREATE INDEX balance_checkpoints_period_idx ON balance_checkpoints (period_year, period_month);

INSERT INTO balance_checkpoints (period_year, period_month, balance)
SELECT opening_year, opening_month, opening_balance FROM settings;

DROP TABLE settings;

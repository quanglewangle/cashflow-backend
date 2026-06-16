ALTER TABLE balance_checkpoints DROP CONSTRAINT balance_checkpoints_period_year_period_month_key;
ALTER TABLE balance_checkpoints ADD COLUMN period_day SMALLINT NOT NULL DEFAULT 1 CHECK (period_day BETWEEN 1 AND 31);
ALTER TABLE balance_checkpoints ADD CONSTRAINT balance_checkpoints_period_unique UNIQUE (period_year, period_month, period_day);
DROP INDEX balance_checkpoints_period_idx;
CREATE INDEX balance_checkpoints_period_idx ON balance_checkpoints (period_year, period_month, period_day);

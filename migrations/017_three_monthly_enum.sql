-- item_frequency's 'three_monthly' value was added to the Go code (see
-- "Add three_monthly frequency", commit e21be60) but no migration ever
-- added it to the actual Postgres enum -- GeneratePeriodEntries's WHERE
-- clause compares frequency against the literal 'three_monthly'
-- regardless of whether any row uses it, so on a database missing this
-- value, /periods/generate (and anything that calls it) fails outright.
-- IF NOT EXISTS makes this safe to run even if the value was already
-- added out-of-band.
-- Run once: psql cashflow -f migrations/017_three_monthly_enum.sql

ALTER TYPE item_frequency ADD VALUE IF NOT EXISTS 'three_monthly';

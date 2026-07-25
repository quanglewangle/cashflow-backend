-- A four-weekly recurring item can land twice in one calendar month (28-day
-- drift against the calendar) -- occurrence_seq distinguishes those rows so
-- each gets its own entry instead of being merged into one inflated line.
-- Always 0 for every other frequency, so existing per-period uniqueness is
-- unchanged for them.
-- Run once: psql cashflow -f migrations/018_entry_occurrence_seq.sql

ALTER TABLE entries ADD COLUMN occurrence_seq SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE entries DROP CONSTRAINT entries_recurring_item_id_period_year_period_month_key;
ALTER TABLE entries ADD CONSTRAINT entries_recurring_period_occurrence_key
    UNIQUE (recurring_item_id, period_year, period_month, occurrence_seq);

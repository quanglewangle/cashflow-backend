-- Lets a card-linked monthly recurring item auto-generate its own decaying
-- "sundries" buffer one-off for every period, instead of relying on someone
-- remembering to add one by hand after each checkpoint (see migration 011's
-- decay columns, and the seed's zeroed-out card default_amounts -- the flat
-- default_amount guess was retired in favour of this buffer, but nothing
-- was creating the buffer itself). sundries_amount/sundries_decay_per_week
-- being NULL (the default) means "don't auto-generate one" -- the fully
-- manual workflow described in the user manual still works unchanged.
-- auto_sundries marks entries created this way so GeneratePeriodEntries can
-- upsert idempotently instead of piling up a duplicate buffer on every call.
-- Run once: psql cashflow -f migrations/016_recurring_item_sundries.sql

ALTER TABLE recurring_items ADD COLUMN sundries_amount NUMERIC(10,2);
ALTER TABLE recurring_items ADD COLUMN sundries_decay_per_week NUMERIC(10,2);

ALTER TABLE entries ADD COLUMN auto_sundries BOOLEAN NOT NULL DEFAULT FALSE;

-- At most one auto-generated buffer per card per payment period.
CREATE UNIQUE INDEX entries_auto_sundries_unique
    ON entries (credit_card_id, period_year, period_month)
    WHERE auto_sundries;

-- Restore an actual estimate for the two cards whose default_amount was
-- zeroed out (migration-adjacent seed edits, 2026-07-24) on the assumption
-- that a decaying buffer would be added per period -- nothing was ever
-- creating that buffer, so every period with no purchases logged yet
-- (e.g. a future month) silently estimated £0. Amount matches each card's
-- old flat default_amount; decay set to taper to £0 over ~4 weeks, roughly
-- one statement cycle.
UPDATE recurring_items SET sundries_amount = 500.00, sundries_decay_per_week = 125.00
    WHERE name = 'Jenny''s card' AND credit_card_id IS NOT NULL;
UPDATE recurring_items SET sundries_amount = 600.00, sundries_decay_per_week = 150.00
    WHERE name = 'Visacard' AND credit_card_id IS NOT NULL;

-- Lets a one-off entry's effective amount shrink over time instead of
-- staying fixed -- e.g. a "sundries" contingency added right after a card
-- checkpoint so the still-accumulating statement isn't under-estimated,
-- manually reduced week by week as real purchases replace the guess.
-- decay_start_date defaults to the day the entry was added; effective
-- amount = max(0, planned_amount - decay_per_week * whole weeks elapsed),
-- computed at read time (not stored). Frozen once actual_amount is set.
-- Run once: psql cashflow -f migrations/011_entry_decay.sql

ALTER TABLE entries ADD COLUMN decay_per_week NUMERIC(10,2);
ALTER TABLE entries ADD COLUMN decay_start_date DATE;

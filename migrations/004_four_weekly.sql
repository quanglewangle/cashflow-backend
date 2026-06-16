-- Adds a 'four_weekly' frequency (28-day cycle, anchored to a known
-- occurrence date, since it drifts against calendar months instead of
-- landing on a fixed day-of-month) and the anchor_date column it needs.
-- Run once against an already-deployed database:
--   psql cashflow -f migrations/004_four_weekly.sql

ALTER TYPE item_frequency ADD VALUE 'four_weekly';
ALTER TABLE recurring_items ADD COLUMN anchor_date DATE;

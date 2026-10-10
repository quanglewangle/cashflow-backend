-- Adds a 'weekly' frequency (7-day cycle, anchored to a known occurrence
-- via anchor_date, like four_weekly): four or five entries a month, one per
-- occurrence, distinguished by occurrence_seq.
-- Run once: psql cashflow -f migrations/022_weekly.sql

ALTER TYPE item_frequency ADD VALUE IF NOT EXISTS 'weekly';

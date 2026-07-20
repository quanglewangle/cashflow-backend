-- Tracks when a card's own repayment entry was actually marked incurred, so
-- sumUnpaidPriorCardBills can tell whether the checkpoint anchoring a later
-- period was taken before or after that payment happened. Marking an entry
-- paid doesn't retroactively change an already-recorded checkpoint's
-- balance -- if the checkpoint predates the payment, the old bill is still
-- baked into it and must still be netted out despite the entry now being
-- incurred. Backfilled to a date far enough back that it predates every
-- existing checkpoint, preserving current behaviour for all existing data.
-- Run once: psql cashflow -f migrations/014_entry_incurred_date.sql

ALTER TABLE entries ADD COLUMN incurred_date DATE;
UPDATE entries SET incurred_date = '2000-01-01' WHERE status = 'incurred';

-- Lets a card's own future payment entry be directly overridden with a
-- what-if guess instead of always being silently recalculated from
-- checkpoint/purchases/default_amount. recalculateCardEntry skips an entry
-- with manually_set = TRUE, unless a real checkpoint now anchors that period
-- -- checkpoints always win, clearing the flag and resuming normal
-- recalculation. Never touched for one-off entries.
-- Run once: psql cashflow -f migrations/013_entry_manual_override.sql

ALTER TABLE entries ADD COLUMN manually_set BOOLEAN NOT NULL DEFAULT FALSE;

-- Distinguishes two repayment models for a credit card:
-- - false (default): pay the statement off in full each cycle -- the period
--   payment is reconstructed from card_checkpoints/card_purchases, falling
--   back to the recurring item's default_amount only when nothing's logged
--   yet. See recalculateCardEntry/sumPurchasesForPeriod.
-- - true: a fixed monthly installment against a revolving (e.g. promotional
--   interest-free) balance -- the period payment is ALWAYS the recurring
--   item's default_amount (plus one-off buffers on top, unchanged); the
--   checkpoint/purchase history no longer drives the payment amount at all,
--   only the separately-computed running balance (see CurrentCardBalance).
-- Run once: psql cashflow -f migrations/015_credit_card_carries_balance.sql

ALTER TABLE credit_cards ADD COLUMN carries_balance BOOLEAN NOT NULL DEFAULT FALSE;

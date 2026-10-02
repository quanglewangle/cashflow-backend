-- Lets a decaying entry count back to zero on a fixed end date instead of
-- decaying forward from decay_start_date -- auto-sundries buffers set it to
-- their statement close, so the weekly steps land whole weeks before the
-- statement and the buffer reaches zero exactly on it (forward decay from a
-- checkpoint or window open left a pro-rated buffer short of zero at the
-- statement, then dropping again after it had closed). NULL keeps the old
-- forward decay for manually added decaying entries.
-- effective amount = min(planned_amount, decay_per_week * ceil(weeks left)),
-- zero on/after decay_end_date.
-- Run once: psql cashflow -f migrations/020_entry_decay_end.sql

ALTER TABLE entries ADD COLUMN decay_end_date DATE;

-- Backfill existing buffers with windowEndForPeriod: the card's statement
-- day, payment_due_month_offset months before the payment period.
UPDATE entries e
SET decay_end_date = (make_date(e.period_year, e.period_month, 1)
                      - make_interval(months => c.payment_due_month_offset)
                      + (c.statement_day - 1) * INTERVAL '1 day')::date
FROM credit_cards c
WHERE e.auto_sundries AND c.id = e.credit_card_id;

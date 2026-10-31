-- Present occasions: a yearly gift allowance (Christmas, a cluster of
-- birthdays) that card purchases in the "Presents" category count against.
-- Each purchase counts toward the next occasion on or after its date. What's
-- left of an occasion's allowance is held back on its card's bills, spread
-- evenly over the days from today (or the day after the previous occasion)
-- up to the occasion -- one card-tagged one-off per payment period, tagged
-- with present_occasion_id so it can be rebuilt whenever spending changes.
-- Each entry holds its full amount until the day after its last day, then
-- drops to zero (decay_per_day = planned_amount, decay_end_date = last + 1),
-- so an unspent allowance is released once the occasion has gone by.
-- Run once: psql cashflow -f migrations/023_present_occasions.sql

CREATE TABLE present_occasions (
    id             SERIAL PRIMARY KEY,
    credit_card_id INT NOT NULL REFERENCES credit_cards(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    month          INT NOT NULL CHECK (month BETWEEN 1 AND 12),
    day            INT NOT NULL CHECK (day BETWEEN 1 AND 31),
    allowance      NUMERIC(12,2) NOT NULL CHECK (allowance > 0)
);

ALTER TABLE entries ADD COLUMN present_occasion_id INT REFERENCES present_occasions(id) ON DELETE CASCADE;

INSERT INTO categories (name, item_type, sort_order)
SELECT 'Presents', 'expense', 0
WHERE NOT EXISTS (SELECT 1 FROM categories WHERE name = 'Presents');

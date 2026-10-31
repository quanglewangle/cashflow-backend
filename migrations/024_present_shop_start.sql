-- Optional "shopping starts" day/month for a present occasion: what's left
-- of its allowance is held back from then (or today, if later) up to the
-- occasion, instead of from the day after the previous occasion. Purchases
-- still count toward the next occasion on or after their date regardless.
-- Run once: psql cashflow -f migrations/024_present_shop_start.sql

ALTER TABLE present_occasions ADD COLUMN shop_start_month INT CHECK (shop_start_month BETWEEN 1 AND 12);
ALTER TABLE present_occasions ADD COLUMN shop_start_day   INT CHECK (shop_start_day BETWEEN 1 AND 31);
ALTER TABLE present_occasions ADD CHECK ((shop_start_month IS NULL) = (shop_start_day IS NULL));

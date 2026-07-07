-- Lets an individual card purchase carry its own category (e.g. the
-- Cashflow app's Google Pay confirm dialog and Items-list edit dialog both
-- already send category_id, but the server was silently dropping it since
-- this column never existed). Nullable -- "No category" is a valid choice.
-- Run once: psql cashflow -f migrations/010_card_purchase_category.sql

ALTER TABLE card_purchases
    ADD COLUMN category_id INT REFERENCES categories(id);

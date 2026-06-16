-- Adds subscription templates (recurring_card_purchases) and links
-- generated card_purchases back to the template that created them.
-- Run once against an already-deployed database:
--   psql cashflow -f migrations/003_recurring_card_purchases.sql

CREATE TABLE recurring_card_purchases (
    id              SERIAL PRIMARY KEY,
    credit_card_id  INT NOT NULL REFERENCES credit_cards(id),
    description     TEXT NOT NULL,
    amount          NUMERIC(10,2) NOT NULL,
    frequency       item_frequency NOT NULL DEFAULT 'monthly',
    day_of_month    SMALLINT NOT NULL CHECK (day_of_month BETWEEN 1 AND 31),
    target_month    SMALLINT CHECK (target_month BETWEEN 1 AND 12),
    active          BOOLEAN NOT NULL DEFAULT TRUE
);

ALTER TABLE card_purchases
    ADD COLUMN recurring_purchase_id INT REFERENCES recurring_card_purchases(id),
    ADD CONSTRAINT card_purchases_recurring_period_uniq UNIQUE (recurring_purchase_id, purchase_date);

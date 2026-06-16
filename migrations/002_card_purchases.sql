-- Adds per-card payment_due_month_offset and the card_purchases table.
-- Run once against an already-deployed database:
--   psql cashflow -f migrations/002_card_purchases.sql

ALTER TABLE credit_cards
    ADD COLUMN payment_due_month_offset SMALLINT NOT NULL DEFAULT 1
        CHECK (payment_due_month_offset BETWEEN 1 AND 3);

CREATE TABLE card_purchases (
    id              SERIAL PRIMARY KEY,
    credit_card_id  INT NOT NULL REFERENCES credit_cards(id),
    description     TEXT NOT NULL,
    amount          NUMERIC(10,2) NOT NULL,
    purchase_date   DATE NOT NULL
);
CREATE INDEX card_purchases_card_idx ON card_purchases (credit_card_id, purchase_date);

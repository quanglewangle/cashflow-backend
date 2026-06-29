-- Records the actual balance owed on a credit card at a specific date,
-- analogous to balance_checkpoints for the main account. Lets you verify
-- or correct the running total of logged purchases against the card app.
-- Run once: psql cashflow -f migrations/009_card_checkpoints.sql

CREATE TABLE card_checkpoints (
    id             BIGSERIAL PRIMARY KEY,
    credit_card_id BIGINT    NOT NULL REFERENCES credit_cards(id),
    period_year    SMALLINT  NOT NULL,
    period_month   SMALLINT  NOT NULL CHECK (period_month BETWEEN 1 AND 12),
    period_day     SMALLINT  NOT NULL DEFAULT 1 CHECK (period_day BETWEEN 1 AND 31),
    balance        NUMERIC(10,2) NOT NULL,
    UNIQUE (credit_card_id, period_year, period_month, period_day)
);

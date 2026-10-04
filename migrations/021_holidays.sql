-- Holidays: a planned stretch of extra card spending at a fixed amount per
-- day, on top of the card's usual sundries buffer. Each holiday is
-- materialized as one card-tagged one-off per payment period its days fall
-- into (a holiday spanning a statement day splits across two bills), each
-- tagged with holiday_id so editing/deleting the holiday can replace them.
-- Those entries decay daily via decay_per_day, counting back from
-- decay_end_date (the day after their last holiday day): the full amount
-- until the holiday starts, decay_per_day less for each day spent, zero
-- once it's over -- by then the spending is on the card itself.
-- effective amount = min(planned_amount, decay_per_day * ceil(days left)).
-- Run once: psql cashflow -f migrations/021_holidays.sql

CREATE TABLE holidays (
    id             SERIAL PRIMARY KEY,
    credit_card_id INT NOT NULL REFERENCES credit_cards(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    start_date     DATE NOT NULL,
    end_date       DATE NOT NULL,
    per_day        NUMERIC(12,2) NOT NULL CHECK (per_day > 0),
    CHECK (end_date >= start_date)
);

ALTER TABLE entries ADD COLUMN decay_per_day NUMERIC(12,2);
ALTER TABLE entries ADD COLUMN holiday_id INT REFERENCES holidays(id) ON DELETE CASCADE;

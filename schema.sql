-- Cashflow forecast database schema.
-- Lives in its own Postgres database ("cashflow"), separate from the
-- "sites" database used by qrzlook, on the same fimblefowl.co.uk instance.
--
-- Create the database once (as the postgres/peer user):
--   createdb cashflow
-- Then load this file:
--   psql cashflow -f schema.sql

CREATE TYPE item_type AS ENUM ('income', 'expense', 'savings');
CREATE TYPE item_frequency AS ENUM ('monthly', 'annual', 'irregular');
CREATE TYPE entry_status AS ENUM ('planned', 'incurred');

-- One row per physical credit card. statement_day/payment_due_day are
-- day-of-month numbers (1-31); payment_due_day is usually in the month
-- *after* statement_day.
CREATE TABLE credit_cards (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    statement_day   SMALLINT NOT NULL CHECK (statement_day BETWEEN 1 AND 31),
    payment_due_day SMALLINT NOT NULL CHECK (payment_due_day BETWEEN 1 AND 31)
);

-- Top-level grouping, mirrors the spreadsheet sections (Income / Committed / Savings).
CREATE TABLE categories (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    item_type  item_type NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

-- The reusable template for a budget line (e.g. "Council Tax", "Barclaycard",
-- "glasses"). This is never modified by paying a bill -- only entries are.
-- Because the template persists, an annual item automatically generates a
-- fresh entry every year instead of being zeroed and forgotten.
CREATE TABLE recurring_items (
    id              SERIAL PRIMARY KEY,
    category_id     INT NOT NULL REFERENCES categories(id),
    name            TEXT NOT NULL,
    item_type       item_type NOT NULL,
    frequency       item_frequency NOT NULL,
    default_amount  NUMERIC(10,2),
    due_day         SMALLINT CHECK (due_day BETWEEN 1 AND 31),     -- monthly/annual items
    target_month    SMALLINT CHECK (target_month BETWEEN 1 AND 12), -- annual items only
    credit_card_id  INT REFERENCES credit_cards(id),
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    notes           TEXT
);

-- One row per template per forecast period (month). This is what the app
-- actually displays and edits; generated idempotently from recurring_items
-- (see /periods/{year}/{month}/generate). One-off items that aren't backed
-- by a template (recurring_item_id NULL) can be added directly to a period.
CREATE TABLE entries (
    id                SERIAL PRIMARY KEY,
    recurring_item_id INT REFERENCES recurring_items(id),
    category_id       INT NOT NULL REFERENCES categories(id),
    period_year       SMALLINT NOT NULL,
    period_month      SMALLINT NOT NULL CHECK (period_month BETWEEN 1 AND 12),
    name              TEXT NOT NULL,
    item_type         item_type NOT NULL,
    planned_amount    NUMERIC(10,2) NOT NULL DEFAULT 0,
    actual_amount     NUMERIC(10,2),
    status            entry_status NOT NULL DEFAULT 'planned',
    credit_card_id    INT REFERENCES credit_cards(id),
    UNIQUE (recurring_item_id, period_year, period_month)
);
CREATE INDEX entries_period_idx ON entries (period_year, period_month);

-- Known-good balances, checked against the real bank app from time to
-- time (same habit as the spreadsheet's hand-typed "brought forward").
-- Forecast() uses the most recent checkpoint at/before the requested
-- period as its base and walks forward from there -- add a new
-- checkpoint any time to correct drift (cash spending, interest, fees,
-- etc. that no entry captures) without touching earlier months.
CREATE TABLE balance_checkpoints (
    id           SERIAL PRIMARY KEY,
    period_year  SMALLINT NOT NULL,
    period_month SMALLINT NOT NULL CHECK (period_month BETWEEN 1 AND 12),
    balance      NUMERIC(10,2) NOT NULL,
    UNIQUE (period_year, period_month)
);
CREATE INDEX balance_checkpoints_period_idx ON balance_checkpoints (period_year, period_month);

-- Seed data matching the current spreadsheet's cards.
INSERT INTO credit_cards (name, statement_day, payment_due_day) VALUES
    ('Visacard',    14, 9),
    ('Barclaycard', 23, 19),
    ('Jenny''s card', 22, 19);

-- Seed categories matching the spreadsheet's sections. Add more from the
-- app any time -- this is just enough to get started.
INSERT INTO categories (name, item_type, sort_order) VALUES
    ('Income',    'income',  0),
    ('Committed', 'expense', 1),
    ('Savings',   'savings', 2);

-- Starting checkpoint: edit the balance to today's actual figure before
-- first use (POST /checkpoints from the app adds new ones later).
INSERT INTO balance_checkpoints (period_year, period_month, balance) VALUES
    (EXTRACT(YEAR FROM CURRENT_DATE)::SMALLINT, EXTRACT(MONTH FROM CURRENT_DATE)::SMALLINT, 0);

-- Savings accounts (e.g. Marcus) -- a third kind of account alongside cash
-- and the credit cards. Money moves between cash and a savings account via
-- ordinary entries tagged with savings_account_id, the same way
-- credit_card_id ties an entry to a card:
-- - item_type 'savings': a transfer INTO the account (leaves cash, as before)
-- - item_type 'income':  a transfer OUT of the account (arrives in cash)
-- The account's own balance is never checkpointed against cash; it's walked
-- forward from opening_balance/opening_date, crediting interest monthly on
-- interest_day -- see SavingsProjection in db.go. Re-anchor it by updating
-- opening_balance/opening_date to a fresh real figure.
-- interest_rate is the AER as a percentage (3.75 = 3.75%).
-- Run once: psql cashflow -f migrations/019_savings_accounts.sql

CREATE TABLE savings_accounts (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    opening_balance NUMERIC(12,2) NOT NULL,
    opening_date    DATE NOT NULL,
    interest_rate   NUMERIC(6,3) NOT NULL DEFAULT 0,
    interest_day    SMALLINT NOT NULL DEFAULT 1 CHECK (interest_day BETWEEN 1 AND 31)
);

ALTER TABLE entries ADD COLUMN savings_account_id INT REFERENCES savings_accounts(id) ON DELETE SET NULL;

-- Every transfer to savings goes to a savings account: a 'savings' entry
-- with no account named is tagged with the first one, whichever path
-- created it (app, period generation from a recurring item, etc). Only
-- savings/income entries can be transfers, so an entry switched to
-- 'expense' drops its tag.
CREATE FUNCTION entries_default_savings_account() RETURNS trigger AS $$
BEGIN
    IF NEW.item_type = 'expense' THEN
        NEW.savings_account_id := NULL;
    ELSIF NEW.item_type = 'savings' AND NEW.savings_account_id IS NULL THEN
        NEW.savings_account_id := (SELECT min(id) FROM savings_accounts);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER entries_default_savings_account
    BEFORE INSERT OR UPDATE ON entries
    FOR EACH ROW EXECUTE FUNCTION entries_default_savings_account();

INSERT INTO savings_accounts (name, opening_balance, opening_date, interest_rate, interest_day)
    VALUES ('Marcus', 12177.00, '2026-09-27', 3.75, 10);

-- Tag the existing transfers (fires the trigger for the 'savings' ones),
-- plus July's "Transfer from savings" income entry.
UPDATE entries SET savings_account_id = (SELECT id FROM savings_accounts WHERE name = 'Marcus')
    WHERE item_type = 'savings'
       OR (item_type = 'income' AND name ILIKE '%from savings%');

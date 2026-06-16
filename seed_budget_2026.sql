-- Optional seed: recurring_items matching the budget spreadsheet's
-- Income/Committed lines as of June 2026. Run once, after schema.sql:
--   psql cashflow -f seed_budget_2026.sql
--
-- Amounts use each line's steady-state monthly figure from the sheet
-- (the column where it had settled, ignoring one-off ramp-up months).
-- Adjust afterwards from the app any time -- editing a recurring_item's
-- default_amount only changes future generated entries.

-- category ids: 1=Income, 2=Committed, 3=Savings (per schema.sql's seed order)
-- credit_cards: Visacard=1, Barclaycard=2, Jenny's card=3 (per schema.sql's insert order)

INSERT INTO recurring_items (category_id, name, item_type, frequency, default_amount, due_day, credit_card_id, active) VALUES
    (1, 'State pension Peter', 'income', 'monthly', 1264.87, NULL, NULL, TRUE),
    (1, 'State pension Jenny',  'income', 'monthly',  710.54, NULL, NULL, TRUE),
    (1, 'Teachers pension',     'income', 'monthly',  577.00, NULL, NULL, TRUE),
    (1, 'Aviva',                'income', 'monthly',  354.00, NULL, NULL, TRUE),
    (1, 'Scot Wid',             'income', 'monthly',   46.00, NULL, NULL, TRUE),

    (2, 'Council Tax',          'expense', 'monthly', 200.00, 1,  NULL, TRUE),
    (2, 'O2',                   'expense', 'monthly',  40.00, 30, NULL, TRUE),
    (2, 'Water/TV licence',     'expense', 'monthly',  70.00, NULL, NULL, TRUE),
    (2, 'Octopus',              'expense', 'monthly',  80.00, NULL, NULL, TRUE),
    (2, 'Charity',              'expense', 'monthly',  30.00, NULL, NULL, TRUE),
    (2, 'Plusnet',              'expense', 'monthly',  25.00, 18, NULL, TRUE),
    (2, 'Visacard',             'expense', 'monthly', 600.00, 9,  1,    TRUE),
    (2, 'Barclaycard',          'expense', 'monthly', 170.00, 19, 2,    TRUE),
    (2, 'repay Marcus',         'expense', 'monthly', 500.00, NULL, NULL, TRUE),
    (2, 'Jack & Archie',        'expense', 'monthly',  60.00, NULL, NULL, TRUE),
    (2, 'Jenny''s card',        'expense', 'monthly', 500.00, 19, 3,    TRUE);

-- From the sheet's "unusual payments" list: only "car" was confirmed as a
-- genuine annual cost (it landed in the July column -> target_month=7).
-- The rest (gudun, jenny's card lump payment, splashback, Trevor, vet,
-- glasses) were one-offs and are deliberately not seeded as templates --
-- add them ad-hoc from the app if/when they recur.
INSERT INTO recurring_items (category_id, name, item_type, frequency, default_amount, target_month, active) VALUES
    (2, 'car', 'expense', 'annual', 370.00, 7, TRUE);

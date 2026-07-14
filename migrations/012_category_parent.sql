-- Lets a category be a subcategory of another, for two-level taxonomy
-- (e.g. "Car" > "Fuel", "Insurance", "Servicing & MOT"). NULL means top-level.
-- Run once: psql cashflow -f migrations/012_category_parent.sql

ALTER TABLE categories ADD COLUMN parent_id INT REFERENCES categories(id);

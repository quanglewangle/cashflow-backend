-- Make deleting a recurring_card_purchases template automatically remove
-- all generated card_purchases instances that reference it.
ALTER TABLE card_purchases
    DROP CONSTRAINT card_purchases_recurring_purchase_id_fkey,
    ADD CONSTRAINT card_purchases_recurring_purchase_id_fkey
        FOREIGN KEY (recurring_purchase_id)
        REFERENCES recurring_card_purchases(id)
        ON DELETE CASCADE;

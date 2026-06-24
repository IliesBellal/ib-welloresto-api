ALTER TABLE upsell_suggestions
    DROP COLUMN staff_member_id;

ALTER TABLE orderitems
    DROP COLUMN is_upsell;

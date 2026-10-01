-- Reverts 134_packages_pro_complet.up.sql. Safe only while no merchant
-- references either package_id (true at the time this migration is
-- written — see docs/decisions.md) ; a merchant on "Pro"/"Complet" would
-- have its packages FK broken by this delete.
DELETE FROM packages WHERE package_name IN ('Pro', 'Complet');

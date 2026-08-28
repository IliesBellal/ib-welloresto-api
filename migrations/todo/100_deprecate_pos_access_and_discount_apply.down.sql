-- Reverts 100_deprecate_pos_access_and_discount_apply.up.sql.
--
-- Restores the two catalog rows (same key/domain/label/is_sensitive/
-- sort_order as their original insert in 095). Does NOT restore the
-- role_permissions rows the up migration deleted — which role held which of
-- these two keys is gone the moment the up migration runs, same
-- irrecoverable-data caveat as any other DELETE in this migration set. A
-- restored merchant wanting "Employé polyvalent" to carry these again would
-- need to re-grant them by hand (PUT /roles/{id}/permissions).
INSERT INTO permissions (key, domain, label, is_sensitive, sort_order) VALUES
    ('pos.access',         'pos', 'Encaisser au point de vente', false, 10),
    ('pos.discount.apply', 'pos', 'Appliquer une remise',        false, 30)
ON CONFLICT (key) DO NOTHING;

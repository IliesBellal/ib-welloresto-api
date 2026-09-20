-- Rollback de 148_permission_cds_manage.up.sql.
-- Retirer d'abord les attributions aux rôles, sinon la FK bloque la
-- suppression du catalogue (même ordre que 103_permission_catalog_lot10.down.sql).

DELETE FROM role_permissions WHERE permission_key = 'cds.manage';

DELETE FROM permissions WHERE key = 'cds.manage';

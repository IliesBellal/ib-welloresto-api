-- RBAC lot 1: seeds the fixed catalog of 14 grantable permissions.
--
-- Idempotent on purpose (ON CONFLICT (key) DO NOTHING) — this file is
-- expected to be replayed (e.g. after being merged into another environment
-- that already ran it), and re-running it must never error nor duplicate rows.
--
-- `description` is left empty ('' — the column default) on purpose: content
-- is a product-copy task, not a schema task. `sort_order` follows the order
-- below, incrementing by 10 to leave room for later insertions without a
-- renumbering migration.
--
-- Single source of truth: internal/permission/keys_gen.go declares one Go
-- constant per key below and internal/permission/keys_gen_test.go fails the
-- build if that file and this INSERT list diverge. If you add/rename/remove a
-- key here, update keys_gen.go in the same change.

INSERT INTO permissions (key, domain, label, is_sensitive, sort_order) VALUES
    ('pos.access',             'pos',       'Encaisser au point de vente',                             false, 10),
    ('pos.ticket.reopen',      'pos',       'Rouvrir un ticket clôturé',                                true,  20),
    ('pos.discount.apply',     'pos',       'Appliquer une remise',                                     false, 30),
    ('pos.refund',             'pos',       'Rembourser une vente',                                     true,  40),
    ('pos.cash_drawer.open',   'pos',       'Ouvrir le tiroir-caisse hors encaissement',                true,  50),
    ('catalog.manage',         'catalog',   'Gérer les produits, les tarifs et les cartes',             true,  60),
    ('inventory.manage',       'inventory', 'Gérer les stocks et les inventaires',                      false, 70),
    ('haccp.manage',           'haccp',     'Gérer le suivi HACCP',                                     false, 80),
    ('customers.manage',       'customers', 'Gérer et exporter les fiches clients',                     true,  90),
    ('staff.manage',           'staff',     'Gérer les employés, les postes, les rôles et les droits',  true,  100),
    ('staff.schedule.manage',  'staff',     'Gérer le planning et les pointages',                       false, 110),
    ('reports.sales.read',     'reports',   'Consulter et exporter les rapports de vente',              false, 120),
    ('reports.financial.read', 'reports',   'Consulter et exporter les rapports financiers',            true,  130),
    ('settings.manage',        'settings',  'Paramétrer l''établissement',                              true,  140)
ON CONFLICT (key) DO NOTHING;

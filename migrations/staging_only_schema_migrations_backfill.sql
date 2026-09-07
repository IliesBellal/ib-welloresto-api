-- PROMPT 27 Phase 2 — remplissage rétroactif de schema_migrations pour
-- STAGING UNIQUEMENT.
--
-- Ne PAS jouer ce fichier contre production. La production reçoit son propre
-- remplissage le jour du déploiement, à partir de son propre diagnostic
-- (cmd/diagnose_migrations) — copier ces lignes vers production affirmerait
-- que des migrations y sont appliquées alors que c'est indéterminable sans
-- ce diagnostic (voir docs/migration-postgres/67-migration-status-audit.md
-- §3.2, et le runbook docs/DEPLOIEMENT_PROD.md, étape 0).
--
-- État constaté sur staging le 2026-09-07 (vérifications en lecture seule,
-- schéma réel — colonnes/tables/index/valeurs d'enum/lignes de catalogue —
-- jamais le nom du fichier seul), par cette session, prérequis à
-- l'exécution de 123_schema_migrations.up.sql. Diverge sur deux points de
-- l'audit du 2026-09-03 (67-migration-status-audit.md), tous deux dans le
-- sens de PLUS de migrations appliquées depuis (l'état bouge, ne pas
-- supposer que cette photo reste valable sans revérifier) :
--   - 087 (index analytics) : alors NON APPLIQUÉE, aujourd'hui APPLIQUÉE
--     (les 4 index sont présents et valides).
--   - 118/119/121/122 : migrations postérieures à cet audit (chantiers
--     PROMPT 21 et PROMPT 26), toutes appliquées à ce jour.
--
-- Volontairement absentes de ce remplissage (NON appliquées sur staging au
-- 2026-09-07, revérifié) : 104, 105, 106, 111 (plan §3.1 de l'audit, pas
-- encore jouées), 112/113/117 (hors plan, décisions produit documentées
-- séparément), 120 (préparée mais délibérément non jouée — voir son propre
-- fichier .up.sql).
--
-- Idempotent (ON CONFLICT DO NOTHING) : rejouable sans effet si déjà passé.

INSERT INTO schema_migrations (version, filename, applied_at, applied_by, notes) VALUES
    ('087', '087_analytics_indexes.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 — appliquée entre le 2026-09-03 (audit 67, alors NON appliquée) et le 2026-09-07, date exacte non tracée.'),
    ('094', '094_roles_schema.up.sql', now(), NULL, 'Remplissage rétroactif — appliquée sous le numéro 089 avant renumérotation (voir migrations/migrations_numbering_test.go).'),
    ('095', '095_roles_permissions_catalog.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('096', '096_seed_system_roles.up.sql', now(), NULL, 'No-op SQL (réservation de numéro) ; la population réelle est cmd/seed_system_roles, déjà exécuté sur staging (docs/RBAC_DEPLOIEMENT_PROD.md).'),
    ('097', '097_permission_pos_status_manage.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('098', '098_access_observation.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('099', '099_merchant_default_role_admin.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('100', '100_deprecate_pos_access_and_discount_apply.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('101', '101_production_profiles.up.sql', now(), NULL, 'Appliquée via un autre chemin que ce fichier (le fichier disque était en syntaxe MySQL non convertie jusqu''à sa réécriture PROMPT 27 Phase 1) — voir 67-migration-status-audit.md §1.2.'),
    ('102', '102_delivery_travel_seconds.up.sql', now(), NULL, 'Idem 101 — voir 67-migration-status-audit.md §1.2.'),
    ('103a', '103_permission_catalog_lot10.up.sql', now(), NULL, 'Identifiant "103a" : deux fichiers distincts partagent le numéro 103 (collision documentée, migrations/migrations_numbering_test.go).'),
    ('103b', '103_production_ready_delivery_arrival.up.sql', now(), NULL, 'Identifiant "103b". Appliquée via un autre chemin que ce fichier avant sa réécriture PROMPT 27 Phase 1 (clause AFTER MySQL) — voir 67-migration-status-audit.md §1.2.'),
    ('107', '107_import_component_mappings.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('108', '108_api_request_logs_response_payload.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('109', '109_api_request_logs_created_at_index.up.sql', now(), NULL, 'Appliquée avant sa réécriture en CREATE INDEX CONCURRENTLY (PROMPT 27 Phase 1) — l''effet (l''index) est identique, seule la méthode de construction a changé.'),
    ('110', '110_drop_dead_legacy_rights_columns.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2.'),
    ('114', '114_write_path_instrumentation.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 07 lot 1).'),
    ('115', '115_permission_reports_staff_performance_read.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 10).'),
    ('116', '116_write_path_instrumentation_lot2.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 11).'),
    ('118', '118_discounts_integer_key_expansion.up.sql', now(), NULL, 'Appliquée avant la réécriture de son index unique sur orderitems en CONCURRENTLY (PROMPT 27 Phase 1) — l''effet est identique, seule la méthode de construction a changé (PROMPT 21 Phase 2+3).'),
    ('119', '119_discount_redemptions_historical_backfill.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 21 Phase 5) — 545 lignes reconstruites.'),
    ('121', '121_customer_stats_counted_marker.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 26 Phase 2).'),
    ('122', '122_customer_stats_reconciliation_runs.up.sql', now(), NULL, 'Remplissage rétroactif PROMPT 27 Phase 2 (PROMPT 26 Phase 4).')
ON CONFLICT (version) DO NOTHING;

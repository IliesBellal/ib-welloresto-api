-- Ajoute la permission cds.manage — administration des écrans d'affichage
-- client (module internal/modules/cds, voir
-- wello-resto-customer-display-system/docs/CDS_DECISIONS.md).
--
-- Clé distincte de kiosk.manage, et non réutilisation de celle-ci : les deux
-- parcs sont séparés (décision D10) et un restaurateur doit pouvoir confier
-- la gestion des écrans sans ouvrir celle des bornes, qui encaissent des
-- paiements. sort_order 175 : juste après kiosk.manage (170), dont le CDS est
-- le complément naturel, et avant seating_plan.manage (180).
--
-- Idempotent (ON CONFLICT DO NOTHING), comme 095/097/103.
-- Source unique de vérité : internal/permission/keys_gen.go doit gagner la
-- constante correspondante dans le même changement (keys_gen_test.go le
-- vérifie), et la clé doit garder au moins une route réelle dans
-- cmd/api/routes.go (routes_rbac_permission_coverage_test.go le vérifie).

INSERT INTO permissions (key, domain, label, description, is_sensitive, sort_order) VALUES
    ('cds.manage', 'cds', 'Gérer les écrans d''affichage client', 'Administrer les écrans d''affichage client : appareils, codes d''enrôlement, filtres d''affichage et contenus marketing.', false, 175)
ON CONFLICT (key) DO NOTHING;

-- RBAC lot 10: adds 5 new permission keys (bookings.manage, platforms.manage,
-- kiosk.manage, pos.analytics, seating_plan.manage) and backfills the
-- `description` column for the 13 existing keys, which migration 095 left
-- empty on purpose ("content is a product-copy task, not a schema task" —
-- see 095's header). The back-office role editor (PermissionsEditor.tsx)
-- now renders `description` unconditionally under every permission's
-- label, so an empty description reads as a UI bug rather than an
-- intentional gap — backfilling both existing and new keys in the same
-- migration closes that gap for good.
--
-- sort_order: pos.analytics (55) slots into the existing pos.* group,
-- right after pos.cash_drawer.open (50) and before catalog.manage (60).
-- The other 4 are brand-new domains, appended after settings.manage (140).
--
-- Idempotent like migration 095/097 (ON CONFLICT DO NOTHING on the INSERT).
-- Single source of truth: internal/permission/keys_gen.go must gain the
-- matching 5 Go constants in the same change (keys_gen_test.go enforces
-- this), and each new key must guard at least one real route in
-- cmd/api/routes.go (routes_rbac_permission_coverage_test.go enforces
-- this).

UPDATE permissions SET description = 'Ouvrir et fermer l''établissement depuis le point de vente.' WHERE key = 'pos.status.manage';
UPDATE permissions SET description = 'Rouvrir un ticket de vente déjà clôturé pour le corriger.' WHERE key = 'pos.ticket.reopen';
UPDATE permissions SET description = 'Effectuer un remboursement sur une vente déjà encaissée.' WHERE key = 'pos.refund';
UPDATE permissions SET description = 'Ouvrir le tiroir-caisse en dehors d''un encaissement.' WHERE key = 'pos.cash_drawer.open';
UPDATE permissions SET description = 'Créer, modifier et publier les produits, tarifs et cartes du menu.' WHERE key = 'catalog.manage';
UPDATE permissions SET description = 'Gérer les stocks, les inventaires et les mouvements de stock.' WHERE key = 'inventory.manage';
UPDATE permissions SET description = 'Configurer les seuils et paramètres du suivi HACCP.' WHERE key = 'haccp.manage';
UPDATE permissions SET description = 'Créer, modifier et exporter les fiches clients.' WHERE key = 'customers.manage';
UPDATE permissions SET description = 'Gérer les employés, les postes, les rôles et l''attribution des droits.' WHERE key = 'staff.manage';
UPDATE permissions SET description = 'Gérer le planning de l''équipe et les pointages.' WHERE key = 'staff.schedule.manage';
UPDATE permissions SET description = 'Consulter et exporter les rapports de vente.' WHERE key = 'reports.sales.read';
UPDATE permissions SET description = 'Consulter et exporter les rapports financiers.' WHERE key = 'reports.financial.read';
UPDATE permissions SET description = 'Paramétrer l''établissement (imprimantes, profils de production, etc.).' WHERE key = 'settings.manage';

INSERT INTO permissions (key, domain, label, description, is_sensitive, sort_order) VALUES
    ('pos.analytics',       'pos',          'Consulter les analyses de vente', 'Accéder à la page Analyse (statistiques de vente et de vente additionnelle).', false, 55),
    ('bookings.manage',     'bookings',     'Paramétrer les réservations',     'Configurer les paramètres de réservation, les horaires et les règles de durée.', false, 150),
    ('platforms.manage',    'platforms',    'Gérer les canaux et plateformes', 'Connecter et configurer les plateformes de vente (Uber Eats, Deliveroo, ScanNOrder) et les paiements Stripe.', false, 160),
    ('kiosk.manage',        'kiosk',        'Gérer les bornes Kiosk',          'Administrer les bornes Kiosk : appareils, codes d''enrôlement et paramètres d''affichage.', false, 170),
    ('seating_plan.manage', 'seating_plan', 'Gérer le plan de salle',          'Configurer les salles, tables et zones du plan de salle.', false, 180)
ON CONFLICT (key) DO NOTHING;

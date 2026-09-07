-- PROMPT 27 Phase 3 — liste nominative des pertes d'accès RBAC en production.
--
-- Réclamée depuis le chantier RBAC, jamais produite. Lecture seule, sans
-- effet de bord. À lancer contre production AVANT la bascule
-- (cmd/assign_admin_role --apply), et seulement APRÈS que la vague A
-- (schéma + catalogue) et cmd/seed_system_roles (vague B, étape 2) ont
-- tourné — cette requête a besoin de roles/role_permissions déjà peuplées
-- pour savoir ce que la bascule s'apprête à accorder.
--
-- Contexte (docs/RBAC_DEPLOIEMENT_PROD.md, docs/migration-postgres/
-- 67-migration-status-audit.md) : en production, role_id est NULL pour tous
-- les comptes. L'accès d'aujourd'hui vient donc entièrement du monde
-- historique (internal/modules/auth/permissions.go, Has()) :
--   - admin = TRUE court-circuite tout : accès à la totalité du catalogue.
--   - sinon, seules 10 clés du catalogue ont un repli booléen
--     (legacyPermissionFallback) ; les 9 autres ne sont accordées à AUCUN
--     compte admin = FALSE, quels que soient ses booléens.
--
-- Neuf clés sans repli aujourd'hui (vérifié contre le catalogue réel de
-- staging le 2026-09-07, 19 clés au total — le chiffre "huit" cité par un
-- brief antérieur au 2026-09-03 ne comptait pas encore
-- reports.staff_performance.read, ajoutée depuis par la migration 115) :
-- bookings.manage, inventory.manage, kiosk.manage, platforms.manage,
-- pos.analytics, pos.refund, pos.ticket.reopen,
-- reports.staff_performance.read, seating_plan.manage. Elles ne sont donc
-- accordées aujourd'hui qu'aux liens portant admin = TRUE — exactement ce
-- que cette requête rend visible : un compte qui a admin = TRUE aujourd'hui
-- mais n'obtiendra pas, une fois la bascule jouée, un rôle "admin" complet
-- sur ces 19 clés (rôle absent, pas encore seedé, ou invariant rompu) perd
-- silencieusement l'accès à certaines d'entre elles — sans qu'aucun test
-- fonctionnel actuel ne les couvre, puisque rien ne les exerce aujourd'hui
-- hors du court-circuit admin.
--
-- La requête ne se limite pas à ces 9 clés : elle calcule le diff complet
-- (18 ou 19 clés selon le catalogue réel) entre "ce que le monde historique
-- accorde aujourd'hui" et "ce que le rôle admin de l'établissement accordera
-- après cmd/assign_admin_role --apply" (qui assigne toujours le rôle admin,
-- jamais un autre rôle — docs/RBAC_DEPLOIEMENT_PROD.md §4), pour repérer
-- aussi une perte sur les 10 clés à repli si un établissement a un rôle
-- admin incomplet pour une tout autre raison (seed_system_roles pas encore
-- joué, tâche de réconciliation pas encore passée).
--
-- Lecture du résultat :
--   - 0 ligne (ou uniquement des comptes de test connus) -> bascule sereine.
--   - Une ligne sur un compte réel (gérant, salarié) -> NE PAS lancer
--     cmd/assign_admin_role --apply avant d'avoir compris pourquoi (le plus
--     probable : cmd/seed_system_roles n'a pas encore tourné pour ce
--     merchant_id, ou l'invariant "rôle admin = catalogue complet" est rompu
--     pour cet établissement — revérifier avec la requête de l'étape 3.1.3
--     de docs/RBAC_DEPLOIEMENT_PROD.md).

WITH catalog AS (
    SELECT key FROM permissions WHERE deprecated_at IS NULL
),
legacy_grants AS (
    -- Ce que Has() accorde AUJOURD'HUI à chaque compte actif (role_id NULL
    -- partout en production au moment de l'écriture de cette requête) :
    -- admin = TRUE accorde tout ; sinon seules les 10 clés à repli booléen,
    -- évaluées à leur valeur actuelle (internal/modules/auth/permissions.go,
    -- legacyPermissionFallback — garder ce diff synchronisé avec ce fichier
    -- si de nouvelles clés y gagnent un repli).
    SELECT ur.id AS users_rights_id, c.key
    FROM users_rights ur
    CROSS JOIN catalog c
    WHERE ur.enabled = TRUE
      AND (
        ur.admin = TRUE
        OR (c.key = 'pos.status.manage'       AND COALESCE(ur.access_wrreception, FALSE) = TRUE)
        OR (c.key = 'pos.cash_drawer.open'     AND COALESCE(ur.open_cash_drawer,   FALSE) = TRUE)
        OR (c.key = 'catalog.manage'           AND COALESCE(ur.manage_menu,        FALSE) = TRUE)
        OR (c.key = 'haccp.manage'             AND COALESCE(ur.manage_haccp,       FALSE) = TRUE)
        OR (c.key = 'customers.manage'         AND COALESCE(ur.manage_customers,   FALSE) = TRUE)
        OR (c.key = 'staff.manage'             AND COALESCE(ur.manage_users,       FALSE) = TRUE)
        OR (c.key = 'staff.schedule.manage'    AND COALESCE(ur.manage_plannings,   FALSE) = TRUE)
        OR (c.key = 'reports.sales.read'       AND COALESCE(ur.view_reports,       FALSE) = TRUE)
        OR (c.key = 'reports.financial.read'   AND COALESCE(ur.view_financials,    FALSE) = TRUE)
        OR (c.key = 'settings.manage'          AND COALESCE(ur.manage_settings,    FALSE) = TRUE)
      )
),
future_grants AS (
    -- Ce que la bascule accordera réellement : le rôle "admin" de
    -- l'établissement du compte. cmd/assign_admin_role assigne toujours ce
    -- rôle-là (jamais un autre) à chaque lien users_rights existant.
    SELECT ur.id AS users_rights_id, rp.permission_key AS key
    FROM users_rights ur
    JOIN roles r ON r.merchant_id = ur.merchant_id AND r.system_key = 'admin'
    JOIN role_permissions rp ON rp.role_id = r.id
    WHERE ur.enabled = TRUE
)
SELECT
    m.fullname                AS etablissement,
    ur.merchant_id,
    u.user_id,
    u.first_name,
    u.last_name,
    u.email,
    ur.admin                  AS admin_legacy_aujourdhui,
    (r.id IS NOT NULL)        AS role_admin_existe_deja,
    lg.key                    AS clef_perdue
FROM legacy_grants lg
JOIN users_rights ur ON ur.id = lg.users_rights_id
JOIN users u ON u.user_id = ur.user_id AND u.merchant_id = ur.merchant_id
JOIN merchant m ON m.id::text = ur.merchant_id
LEFT JOIN roles r ON r.merchant_id = ur.merchant_id AND r.system_key = 'admin'
WHERE NOT EXISTS (
    SELECT 1 FROM future_grants fg
    WHERE fg.users_rights_id = lg.users_rights_id AND fg.key = lg.key
)
ORDER BY etablissement, u.email, clef_perdue;

-- Jointure merchant confirmée sur staging le 2026-09-07 : m.id::text =
-- ur.merchant_id (merchant.id est numérique, merchant.brand_id est vide sur
-- tous les échantillons observés — ne pas utiliser brand_id). Un
-- users_rights.merchant_id sans ligne merchant correspondante (JOIN, pas
-- LEFT JOIN, l'exclut délibérément de ce rapport) est l'anomalie déjà connue
-- documentée dans docs/RBAC_BASCULE.md §5 — un merchant_id orphelin, hors
-- périmètre de cette requête, à traiter séparément si elle réapparaît en
-- production.

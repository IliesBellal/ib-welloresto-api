-- File de réveils par compte de plateforme de livraison (Deliveroo aujourd'hui,
-- Uber Eats ensuite : colonne platform).
--
-- Une ligne par compte (platform + merchant_id + location_id) déjà publié au
-- moins une fois. Elle dit QUAND relire l'état de la plateforme pour l'aligner
-- sur WelloResto, au lieu de scruter tous les comptes à intervalle fixe :
--   next_check_at / check_kind : prochain réveil et sa raison
--     changed  un statut produit vient de changer
--     opening  juste après l'ouverture prévue du site (Deliveroo remet « épuisé »
--              en « disponible » à l'ouverture)
--     recheck  second contrôle après l'ouverture
--     safety   filet de sécurité toutes les 2 à 3 heures
--     retry    nouvelle tentative après un échec
--   check_attempts : tentatives d'attente de l'ouverture réelle (statut OPEN)
--   menu_dirty_since : le catalogue a changé depuis la dernière publication ;
--     le menu est recalculé et republié s'il a vraiment changé, après quelques
--     minutes de calme
--   opening_hours / time_zone : horaires d'ouverture lus chez la plateforme,
--     relus chaque jour
--
-- Seules les lignes existantes sont marquées par les changements de catalogue :
-- un compte jamais publié (bouton du back-office) n'est jamais republié seul.
--
-- Documentation : docs/deliveroo-menu-sync.md.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS marketplace_sync_state (
    platform             varchar(20)  NOT NULL,
    merchant_id          varchar(20)  NOT NULL,
    location_id          varchar(50)  NOT NULL,
    next_check_at        timestamptz  NULL,
    check_kind           varchar(20)  NULL,
    check_attempts       integer      NOT NULL DEFAULT 0,
    menu_dirty_since     timestamptz  NULL,
    opening_hours        jsonb        NULL,
    time_zone            varchar(64)  NULL,
    opening_refreshed_at timestamptz  NULL,
    last_check_at        timestamptz  NULL,
    last_error           text         NULL,
    PRIMARY KEY (platform, merchant_id, location_id)
);

CREATE INDEX IF NOT EXISTS idx_marketplace_sync_state_due
    ON marketplace_sync_state (next_check_at)
    WHERE next_check_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_marketplace_sync_state_dirty
    ON marketplace_sync_state (menu_dirty_since)
    WHERE menu_dirty_since IS NOT NULL;

COMMENT ON TABLE marketplace_sync_state IS
    'File de réveils par compte de plateforme de livraison (docs/deliveroo-menu-sync.md).';

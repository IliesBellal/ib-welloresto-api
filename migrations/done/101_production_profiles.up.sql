-- Profils de production : remplace le filtre local (SharedPreferences,
-- ProductionSettingsNotifier) de l'app Flutter par un CRUD backend nommé, par
-- établissement. Chaque produit rattaché à un profil porte deux flags
-- indépendants (should_produce, should_monitor), pas un choix exclusif.
--
-- split_by_source / display_only_paid_orders : deux réglages d'affichage de
-- l'écran production, avant portés par ProductionSettingsNotifier
-- (SharedPreferences, par appareil) — désormais portés par le profil lui-même
-- (comme name), donc synchronisés avec la définition du profil plutôt que
-- par appareil.
--
-- Style calqué sur printers (migrations/done/043_printers_production_product_ids.up.sql,
-- 048_printers_paper_width_mm.up.sql) pour production_profiles, et sur
-- product_configurable_attribute (internal/modules/menu/repository.go) pour
-- product_production_profiles : table de jointure sans merchant_id ni FK,
-- ownership vérifié en code applicatif (production_profiles.merchant_id +
-- products.merchant_id) — convention du projet, pas de FK vers les tables
-- historiques (cf. migrations/done/032_delivery_module.up.sql).
--
-- Pas de permission RBAC dédiée sur ce module : les endpoints
-- /production-profiles ne sont protégés que par authMiddleware (au même
-- niveau que /printers), sur décision explicite.
--
-- load_slot_interval_minutes / load_slot_duration_hours / load_max_capacity_count :
-- réglages de l'indicateur de charge de production (écran PRODUCTION),
-- avant portés par SharedPreferences (par appareil, un seul jeu de valeurs
-- pour tout l'établissement) — désormais portés par le profil lui-même,
-- comme split_by_source, puisque chaque poste de production (le profil) a sa
-- propre cadence et sa propre capacité. Défauts alignés sur les anciennes
-- constantes SharedPreferences (15 / 4 / 15).
--
-- PostgreSQL migration (réécrite le 2026-09-07, PROMPT 27 Phase 1) : le
-- fichier original était en syntaxe MySQL non convertie (ENGINE=InnoDB,
-- INT UNSIGNED, DATETIME ... ON UPDATE, KEY inline) — invalide en Postgres.
-- Cette version reprend le schéma tel que réellement obtenu sur staging
-- (docs/migration-postgres/67-migration-status-audit.md §1.2), où cette
-- migration a été appliquée par un autre chemin que ce fichier : types
-- Postgres, updated_at NOT NULL DEFAULT CURRENT_TIMESTAMP sans trigger (pas
-- de reprise du comportement ON UPDATE CURRENT_TIMESTAMP de MySQL — aucun
-- trigger n'existe sur staging, updated_at doit être renseigné explicitement
-- par le code applicatif à chaque écriture), et idx_production_profiles_merchant
-- en index séparé (pas de KEY inline en Postgres).

CREATE TABLE production_profiles (
    production_profile_id      varchar(64)  NOT NULL,
    merchant_id                 varchar(64)  NOT NULL,
    name                        varchar(255) NOT NULL,
    split_by_source              boolean      NOT NULL DEFAULT true,
    display_only_paid_orders     boolean      NOT NULL DEFAULT false,
    load_slot_interval_minutes   integer      NOT NULL DEFAULT 15,
    load_slot_duration_hours     integer      NOT NULL DEFAULT 4,
    load_max_capacity_count      integer      NOT NULL DEFAULT 15,
    created_at                   timestamp    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                   timestamp    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (production_profile_id)
);

CREATE INDEX IF NOT EXISTS idx_production_profiles_merchant
    ON production_profiles (merchant_id);

-- Jointure produit x profil. should_produce / should_monitor sont deux flags
-- indépendants (un produit peut être l'un, l'autre, les deux, ou ni l'un ni
-- l'autre — dans ce dernier cas la ligne est simplement absente, voir
-- ReplaceProducts dans internal/modules/productionprofiles/repository.go).
-- Pas de colonne merchant_id ici, comme product_configurable_attribute : le
-- scoping se fait via production_profiles et products.
CREATE TABLE product_production_profiles (
    production_profile_id varchar(64) NOT NULL,
    product_id              varchar(64) NOT NULL,
    should_produce           boolean    NOT NULL DEFAULT false,
    should_monitor           boolean    NOT NULL DEFAULT false,
    PRIMARY KEY (production_profile_id, product_id)
);

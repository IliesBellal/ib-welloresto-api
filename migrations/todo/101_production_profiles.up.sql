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

CREATE TABLE production_profiles (
    production_profile_id     VARCHAR(64)  NOT NULL,
    merchant_id                VARCHAR(64)  NOT NULL,
    name                        VARCHAR(255) NOT NULL,
    split_by_source             BOOLEAN      NOT NULL DEFAULT TRUE,
    display_only_paid_orders    BOOLEAN      NOT NULL DEFAULT FALSE,
    load_slot_interval_minutes  INT UNSIGNED NOT NULL DEFAULT 15,
    load_slot_duration_hours    INT UNSIGNED NOT NULL DEFAULT 4,
    load_max_capacity_count     INT UNSIGNED NOT NULL DEFAULT 15,
    created_at                  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  DATETIME     NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (production_profile_id),
    KEY idx_production_profiles_merchant (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Jointure produit x profil. should_produce / should_monitor sont deux flags
-- indépendants (un produit peut être l'un, l'autre, les deux, ou ni l'un ni
-- l'autre — dans ce dernier cas la ligne est simplement absente, voir
-- ReplaceProducts dans internal/modules/productionprofiles/repository.go).
-- Pas de colonne merchant_id ici, comme product_configurable_attribute : le
-- scoping se fait via production_profiles et products.
CREATE TABLE product_production_profiles (
    production_profile_id VARCHAR(64) NOT NULL,
    product_id              VARCHAR(64) NOT NULL,
    should_produce          BOOLEAN     NOT NULL DEFAULT FALSE,
    should_monitor          BOOLEAN     NOT NULL DEFAULT FALSE,
    PRIMARY KEY (production_profile_id, product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

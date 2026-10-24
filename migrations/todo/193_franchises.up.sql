-- Franchises : regroupement d'établissements autour d'une page hub ScanNOrder
-- (/franchise/{slug}) et d'un menu Franchise au back-office. Voir
-- docs/franchises.md.
--
-- 1. brands devient franchises (et brand_id franchise_id, ici et dans merchant).
--    « brand » désigne déjà ailleurs le canal d'une commande (orders.brand,
--    brand_status, brand_store_id), la marque d'une fiche client
--    (customer_brand), celle d'une carte (card_brand) et l'identifiant de marque
--    Deliveroo (integration_deliveroo.brand_id). Un renommage garde les lignes
--    existantes.
-- 2. franchises : logo, bannière et description deviennent facultatifs, ajout
--    de brand_color (couleur du hub) et d'un index unique sur slug.
-- 3. merchant.franchise_hub_visible : l'établissement apparaît sur le hub.
-- 4. franchise_members : qui gère quelle franchise (owner | manager). Les
--    droits de franchise ne passent pas par le RBAC par établissement.
--
-- Un établissement appartient au plus à une franchise (merchant.franchise_id).
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Le code lit franchises,
-- merchant.franchise_id et franchise_members ; l'ancien code lisait brands
-- (GET /scannorder/brands/{slug}, que rien n'appelle).
--
-- Rejouable : chaque étape vérifie l'état courant.

-- 1. Renommages.
DO $$
BEGIN
    IF to_regclass('brands') IS NOT NULL AND to_regclass('franchises') IS NULL THEN
        ALTER TABLE brands RENAME TO franchises;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'franchises' AND column_name = 'brand_id') THEN
        ALTER TABLE franchises RENAME COLUMN brand_id TO franchise_id;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'brands_pkey') THEN
        ALTER TABLE franchises RENAME CONSTRAINT brands_pkey TO franchises_pkey;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'merchant' AND column_name = 'brand_id') THEN
        ALTER TABLE merchant RENAME COLUMN brand_id TO franchise_id;
    END IF;
END $$;

-- 2. franchises. Les identifiants générés (helpers.GeneratePrefixedID("fra"),
--    40 caractères) dépassent l'ancien varchar(35).
ALTER TABLE franchises
    ALTER COLUMN franchise_id TYPE varchar(64),
    ALTER COLUMN name TYPE varchar(80),
    ALTER COLUMN logo_url TYPE text,
    ALTER COLUMN logo_url DROP NOT NULL,
    ALTER COLUMN banner_url TYPE text,
    ALTER COLUMN banner_url DROP NOT NULL,
    ALTER COLUMN description TYPE text,
    ALTER COLUMN description DROP NOT NULL;

ALTER TABLE franchises
    ADD COLUMN IF NOT EXISTS brand_color varchar(7),
    ADD COLUMN IF NOT EXISTS updated_at timestamptz;

UPDATE franchises SET logo_url = NULL WHERE logo_url = '';
UPDATE franchises SET banner_url = NULL WHERE banner_url = '';
UPDATE franchises SET description = NULL WHERE description = '';

CREATE UNIQUE INDEX IF NOT EXISTS franchises_slug_key ON franchises (slug);

COMMENT ON TABLE franchises IS 'Regroupement d''établissements : page hub ScanNOrder /franchise/{slug} et menu Franchise du back-office (docs/franchises.md).';
COMMENT ON COLUMN franchises.brand_color IS 'Couleur du hub (#RRGGBB), NULL = thème par défaut.';

-- 3. merchant.
ALTER TABLE merchant ALTER COLUMN franchise_id TYPE varchar(64);
UPDATE merchant SET franchise_id = NULL WHERE franchise_id = '';

ALTER TABLE merchant
    ADD COLUMN IF NOT EXISTS franchise_hub_visible boolean NOT NULL DEFAULT true;

CREATE INDEX IF NOT EXISTS merchant_franchise_id_idx ON merchant (franchise_id) WHERE franchise_id IS NOT NULL;

COMMENT ON COLUMN merchant.franchise_id IS 'Franchise de l''établissement (franchises.franchise_id), au plus une.';
COMMENT ON COLUMN merchant.franchise_hub_visible IS 'L''établissement apparaît sur le hub de sa franchise.';

-- 4. Gestionnaires. Aucune FK vers les tables historiques (convention des
--    migrations récentes) ; la suppression d'une franchise retire ses membres
--    côté code, dans la même transaction.
CREATE TABLE IF NOT EXISTS franchise_members (
    franchise_id varchar(64) NOT NULL,
    user_id      varchar(64) NOT NULL,
    role         varchar(16) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz,
    PRIMARY KEY (franchise_id, user_id),
    CONSTRAINT franchise_members_role_check CHECK (role IN ('owner', 'manager'))
);

CREATE INDEX IF NOT EXISTS franchise_members_user_id_idx ON franchise_members (user_id);

COMMENT ON TABLE franchise_members IS 'Gestionnaires d''une franchise. owner : tout, dont les gestionnaires et la suppression ; manager : établissements et apparence du hub.';

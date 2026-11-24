-- Banque d'images Wello Resto (docs/banque-images.md).
--
-- Images communes à tous les établissements, proposées par le back-office
-- partout où l'on choisit une image (produits, options, catégories,
-- bannières…). Le fichier est dans le bucket R2 public, sous
-- wello_resto_images_storage/image_bank/{category}/ ; les établissements qui
-- la choisissent enregistrent l'URL telle quelle. L'API ne supprime jamais un
-- fichier de ce dossier (r2.Client.DeleteFile).
--
-- Alimentée uniquement par cmd/image_bank_import (dossier + manifeste CSV).
--   slug          identifiant stable venant du manifeste : clé d'upsert.
--   category      code de catégorie, liste fermée côté API
--                 (internal/modules/imagebank/categories.go).
--   format        square (produit, option, catégorie) ou banner (bannière).
--   tags          mots-clés séparés par des virgules, en minuscules.
--   content_hash  sha256 du fichier source : un import relancé ne renvoie
--                 pas un fichier inchangé.
--   source, license, author, source_url : provenance et licence, à
--                 conserver pour pouvoir justifier chaque image.
--   enabled       false = retirée de la banque (le fichier reste en place :
--                 des établissements l'utilisent peut-être encore).
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.
CREATE TABLE IF NOT EXISTS image_bank (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug varchar(120) NOT NULL,
    category varchar(40) NOT NULL,
    name varchar(160) NOT NULL,
    tags text NOT NULL DEFAULT '',
    format varchar(10) NOT NULL DEFAULT 'square',
    image_url text NOT NULL,
    r2_key text NOT NULL,
    content_type varchar(20) NOT NULL,
    width integer NOT NULL DEFAULT 0,
    height integer NOT NULL DEFAULT 0,
    size_bytes integer NOT NULL DEFAULT 0,
    content_hash char(64) NOT NULL,
    source varchar(40) NOT NULL DEFAULT '',
    license varchar(80) NOT NULL DEFAULT '',
    author varchar(160) NOT NULL DEFAULT '',
    source_url text NOT NULL DEFAULT '',
    sort_order integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_image_bank_slug UNIQUE (slug),
    CONSTRAINT chk_image_bank_format CHECK (format IN ('square', 'banner'))
);
CREATE INDEX IF NOT EXISTS idx_image_bank_listing ON image_bank (enabled, format, category, sort_order);
CREATE INDEX IF NOT EXISTS idx_image_bank_r2_key ON image_bank (r2_key);

COMMENT ON TABLE image_bank IS 'Banque d''images Wello Resto commune à tous les établissements (docs/banque-images.md).';
COMMENT ON COLUMN image_bank.format IS 'square (produit, option, catégorie) ou banner (bannière).';
COMMENT ON COLUMN image_bank.content_hash IS 'sha256 du fichier source importé (import idempotent).';

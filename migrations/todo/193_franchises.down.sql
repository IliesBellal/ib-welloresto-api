-- Rollback de 193_franchises.up.sql.
-- À jouer APRÈS le retrait du code qui lit franchises, franchise_members et
-- merchant.franchise_* (modules franchises et scannorder). Les gestionnaires et
-- la visibilité sur le hub sont perdus ; les franchises redeviennent brands
-- avec leurs établissements.
--
-- Les colonnes élargies (varchar(64), text) ne sont pas restreintes : un
-- identifiant généré depuis ne tiendrait plus dans varchar(35). Logo, bannière
-- et description restent facultatifs pour la même raison.

DROP TABLE IF EXISTS franchise_members;

DROP INDEX IF EXISTS merchant_franchise_id_idx;
ALTER TABLE merchant DROP COLUMN IF EXISTS franchise_hub_visible;

DROP INDEX IF EXISTS franchises_slug_key;
ALTER TABLE IF EXISTS franchises
    DROP COLUMN IF EXISTS brand_color,
    DROP COLUMN IF EXISTS updated_at;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'merchant' AND column_name = 'franchise_id') THEN
        ALTER TABLE merchant RENAME COLUMN franchise_id TO brand_id;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'franchises_pkey') THEN
        ALTER TABLE franchises RENAME CONSTRAINT franchises_pkey TO brands_pkey;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'franchises' AND column_name = 'franchise_id') THEN
        ALTER TABLE franchises RENAME COLUMN franchise_id TO brand_id;
    END IF;

    IF to_regclass('franchises') IS NOT NULL AND to_regclass('brands') IS NULL THEN
        ALTER TABLE franchises RENAME TO brands;
    END IF;
END $$;

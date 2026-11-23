-- Référencement des pages établissement ScanNOrder (docs/scannorder-seo.md).
--
--   seo_links       liens vers les autres pages de l'établissement (fiche
--                   Google, Instagram, Facebook, TripAdvisor…), tableau JSON
--                   d'URL https. Publiés en sameAs (schema.org) : ils
--                   relient la page ScanNOrder à la fiche Google de
--                   l'établissement. NULL = aucun.
--   seo_updated_at  dernière modification des champs seo_* depuis le
--                   back-office. Avec merchant_parameters.last_menu_update,
--                   donne le lastmod du sitemap et déclenche le signalement
--                   IndexNow (Bing, Yahoo…). NULL = jamais modifiés.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (GetMerchantSEOInfo et le
-- sitemap lisent les colonnes). Rejouable. Ajout de colonnes nullables sans
-- valeur par défaut : instantané (verrou bref sur scannorder_settings).
ALTER TABLE scannorder_settings
  ADD COLUMN IF NOT EXISTS seo_links text,
  ADD COLUMN IF NOT EXISTS seo_updated_at timestamptz;

COMMENT ON COLUMN scannorder_settings.seo_links IS 'Liens publics de l''établissement (fiche Google, réseaux sociaux), tableau JSON d''URL https, publiés en sameAs. NULL = aucun.';
COMMENT ON COLUMN scannorder_settings.seo_updated_at IS 'Dernière modification des champs seo_* (lastmod du sitemap, signalement IndexNow). NULL = jamais modifiés.';

-- Unicité de qrcodes.code.
--
-- Le code d'un QR est l'identifiant public d'un établissement dans l'URL de sa
-- carte ScanNOrder (/restaurant/{code}) ; toutes les lectures font
-- WHERE code = ? en supposant un seul résultat. Les codes étaient des UUID,
-- donc jamais en collision ; les nouveaux établissements reçoivent un code
-- lisible nom-ville (pos.uniqueMainQRCode), d'où cet index qui rend une
-- collision impossible plutôt que silencieuse.
--
-- S'arrête (sans rien modifier) s'il existe déjà des doublons. Pour les lister :
--   SELECT code, COUNT(*) FROM qrcodes GROUP BY code HAVING COUNT(*) > 1;
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

DO $$
DECLARE
    dup_count integer;
BEGIN
    IF to_regclass('public.qrcodes') IS NULL THEN
        RAISE NOTICE 'qrcodes absente : rien à faire';
        RETURN;
    END IF;

    SELECT COUNT(*) INTO dup_count
    FROM (SELECT code FROM public.qrcodes GROUP BY code HAVING COUNT(*) > 1) d;

    IF dup_count > 0 THEN
        RAISE EXCEPTION 'qrcodes.code : % code(s) en double, index unique non créé (voir le commentaire de la migration)', dup_count;
    END IF;

    CREATE UNIQUE INDEX IF NOT EXISTS qrcodes_code_key ON public.qrcodes (code);
END
$$;

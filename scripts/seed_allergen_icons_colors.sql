-- Renseigne icon (emoji) et color (hex) pour les 14 allergènes réglementaires UE
-- (table globale `allergens`, non liée à un merchant).
--
-- Idempotent : chaque UPDATE matche sur le nom (ILIKE, tolérant aux variantes
-- d'accents/orthographe) et peut être rejoué sans risque.
--
-- Usage :
--   1. Lancer d'abord la SELECT de vérification ci-dessous pour voir l'état actuel
--      (et en garder une copie, il n'y a pas de script de rollback dédié).
--   2. Lancer ce script sur staging, vérifier le rendu dans le back-office / SNO,
--      puis rejouer sur prod.
--
-- psql -h <host> -U <user> -d <db> -f scripts/seed_allergen_icons_colors.sql

\echo 'Avant mise à jour :'
SELECT allergen_id, name, code, icon, color FROM allergens ORDER BY name;

BEGIN;

DO $$
DECLARE
  r RECORD;
  affected INT;
BEGIN
  FOR r IN
    SELECT * FROM (VALUES
      -- pattern (ILIKE),         icon, color
      ('%gluten%',                '🌾', '#B45309'),
      ('%crustac%',                '🦐', '#DC2626'),
      ('%œuf%',                    '🥚', '#F59E0B'),
      ('%oeuf%',                   '🥚', '#F59E0B'),
      ('%poisson%',                '🐟', '#0284C7'),
      ('%arachide%',               '🥜', '#C2410C'),
      ('%cacahu%',                 '🥜', '#C2410C'),
      ('%soja%',                   '🌱', '#65A30D'),
      ('%soya%',                   '🌱', '#65A30D'),
      ('%lait%',                   '🥛', '#2563EB'),
      ('%coque%',                  '🌰', '#92400E'),  -- fruits à coque
      ('%céleri%',                 '🌿', '#15803D'),
      ('%celeri%',                 '🌿', '#15803D'),
      ('%moutarde%',               '🟡', '#CA8A04'),
      ('%sésame%',                 '🟤', '#A16207'),
      ('%sesame%',                 '🟤', '#A16207'),
      ('%sulfite%',                '🍷', '#7C3AED'),
      ('%lupin%',                  '🌸', '#DB2777'),
      ('%mollusque%',              '🐚', '#0D9488')
    ) AS v(pattern, icon, color)
  LOOP
    UPDATE allergens
       SET icon = r.icon,
           color = r.color
     WHERE name ILIKE r.pattern;

    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected = 0 THEN
      RAISE NOTICE 'pattern % -> AUCUNE ligne matchée (vérifier le nom en base)', r.pattern;
    ELSE
      RAISE NOTICE 'pattern % -> % ligne(s) mise(s) à jour', r.pattern, affected;
    END IF;
  END LOOP;
END $$;

\echo 'Après mise à jour :'
SELECT allergen_id, name, code, icon, color FROM allergens ORDER BY name;

-- Vérifier la sortie ci-dessus : toute ligne encore avec icon/color vide n'a
-- matché aucun pattern (nom inattendu) et doit être traitée manuellement.
-- Si tout est correct : COMMIT; sinon : ROLLBACK;
COMMIT;

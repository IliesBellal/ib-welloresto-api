-- Migration 004 : conversion des valeurs numériques de stock_movements en texte explicite
-- À exécuter AVANT de déployer le code qui utilise les nouvelles valeurs.
--
-- Valeurs `source` :
--   'scan'   → réception via scan code-barres (AddStockBarcode)
--   'manual' → ajustement back-office (add, remove, loss)
--   'order'  → consommation automatique à la fermeture d'une commande
--
-- Valeurs `movement` :
--   'add'     → ajout de stock
--   'remove'  → retrait/correction manuelle
--   'loss'    → perte (produit tombé, périmé, etc.)
--   'consume' → consommation lors de la clôture d'une commande

-- ── ÉTAPE 1 : Migrer `source` ──────────────────────────────────────────────────
-- source='2' + movement='1' (add) = scan de réception
-- tout le reste = ajustement manuel
UPDATE stock_movements SET source = 'scan'   WHERE source = '2' AND movement = '1';
UPDATE stock_movements SET source = 'manual' WHERE source IN ('1', '2');

-- ── ÉTAPE 2 : Migrer `movement` ────────────────────────────────────────────────
-- Avant ce déploiement, movement='2' ne pouvait être qu'un retrait manuel
-- (la consommation automatique à la commande n'existait pas encore).
UPDATE stock_movements SET movement = 'add'    WHERE movement = '1';
UPDATE stock_movements SET movement = 'remove' WHERE movement = '2';
UPDATE stock_movements SET movement = 'loss'   WHERE movement = '4';

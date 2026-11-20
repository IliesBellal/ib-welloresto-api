-- Paiement attendu d'une commande (docs/order-lifecycle.md, « Paiement
-- attendu »).
--
-- Un client qui commande par téléphone « paiera en espèces à la livraison »
-- n'a encore rien payé : jusqu'ici la caisse enregistrait quand même un
-- paiement ES, signé dans la chaîne fiscale alors que l'argent n'était pas
-- encaissé. Ces deux colonnes portent l'INTENTION, sans paiement :
--
--   expected_mop            moyen annoncé (ES, CB, TR… = models.HandCollectedMOPs),
--                           NULL = rien d'annoncé.
--   expected_tender_amount  espèces seulement : montant que le client compte
--                           donner (centimes), pour afficher la monnaie à
--                           rendre. NULL = non précisé.
--
-- Ce sont des informations de la commande, hors charge utile signée : elles
-- se modifient librement tant que la commande est ouverte. Le statut « payé »
-- reste dérivé des vrais paiements (payments), jamais de ces colonnes. Elles
-- sont conservées une fois la commande encaissée (historique de l'annonce).
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (le code lit les colonnes).
-- Rejouable. Ajout de colonnes nullables sans valeur par défaut : instantané,
-- mais verrou ACCESS EXCLUSIVE le temps de son acquisition (sur une table
-- chaude, jouer avec SET lock_timeout = '3s' et relancer en cas d'échec).
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS expected_mop varchar(20),
  ADD COLUMN IF NOT EXISTS expected_tender_amount integer;

COMMENT ON COLUMN orders.expected_mop IS 'Moyen de paiement annoncé (paiement à la livraison / au retrait), sans paiement enregistré. NULL = aucun.';
COMMENT ON COLUMN orders.expected_tender_amount IS 'Espèces : montant que le client compte donner, en centimes (pour la monnaie à rendre). NULL = non précisé.';

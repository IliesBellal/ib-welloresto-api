-- Deux heures dérivées, à sens fixe (pas une seule colonne dont le sens
-- dépendrait de `scheduled` — voir docs/DELIVERY_TRAVEL_TIME.md) :
--
-- production_ready_at : deadline cuisine, renseignée pour TOUTE commande
--   (livraison ou non). estimated_ready est la date de livraison promise
--   (heure choisie par le client/staff, livreur à la porte) pour une commande
--   programmée -> deadline cuisine = estimated_ready - delivery_travel_seconds.
--   Pour une commande non programmée, estimated_ready EST déjà la deadline
--   cuisine (calculée automatiquement, temps de prépa seul) -> valeur reprise
--   telle quelle.
--
-- delivery_arrival_at : heure d'arrivée livreur estimée, NULL pour le
--   non-livraison (NULL qui a un sens : "pas de livraison"). Pour une
--   commande programmée, estimated_ready EST par définition cette heure ;
--   sinon estimated_ready + delivery_travel_seconds si le trajet est connu.
--
-- Calculées à l'écriture (resolveProductionReadyAt / resolveDeliveryArrivalAt,
-- internal/modules/order_life_cycle/repository.go), comme estimated_ready et
-- delivery_travel_seconds.
--
-- PostgreSQL migration (réécrite le 2026-09-07, PROMPT 27 Phase 1) : seule
-- syntaxe MySQL non convertie du fichier original, la clause AFTER (Postgres
-- n'ordonne pas les colonnes à la demande) — supprimée, sans autre
-- changement. Le schéma obtenu correspond à ce qui est réellement sur
-- staging (docs/migration-postgres/67-migration-status-audit.md §1.2) :
-- timestamp without time zone, nullable, pas de défaut.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS production_ready_at timestamp,
  ADD COLUMN IF NOT EXISTS delivery_arrival_at timestamp;

-- dateCall : confirmé non mort (contrairement à la demande initiale) mais
-- sans réelle utilité propre — toujours écrit à UTC_TIMESTAMP() à la création
-- (jamais une valeur distincte de creation_date), lu uniquement en fallback
-- `callHour || creation_date` dans wello-back-office (3 endroits, migrés vers
-- creation_date seul dans ce même chantier). Remplacé par les deux colonnes
-- ci-dessus plutôt que conservé sans usage.
ALTER TABLE orders
  DROP COLUMN dateCall;

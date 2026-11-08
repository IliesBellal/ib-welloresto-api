# Zones de livraison : calcul, enregistrement, affichage

Chaque client livré reçoit une zone (A1, NE2, F6…) d'après la découpe choisie
par l'établissement dans le back-office (Paramètres > Établissement >
Livraison > « Zones de livraison »). La zone est enregistrée sur le client
(`customer.customer_zone_code`), renvoyée avec les commandes et affichée dans
la production du POS, à droite de l'adresse.

Implémenté le 2026-11-08. Convention et aperçu cartographique :
`wello-back-office/docs/zones-livraison-apercu.md`.

## Convention

Même règle que l'aperçu du back-office (`src/lib/deliveryZones.ts`,
`zoneLabelForPoint`), portée en Go dans
[internal/modules/deliveryzone/zone.go](../internal/modules/deliveryzone/zone.go).
Les deux implémentations ont les mêmes cas de test ; toute évolution se fait
des deux côtés.

| Type | Secteurs | Plages / lignes | Exemple |
|---|---|---|---|
| RADIAL | A, B, C… sens horaire, A commence plein nord | 1, 2, 3… du centre vers l'extérieur | B2 |
| CARDINAL | N/E/S/O ou N/NE/…/NO, secteurs **centrés** sur leur direction | idem | NE2 |
| GRID | colonnes A, B… d'ouest en est | lignes 1, 2… du nord au sud | F6 |

- Plages `"0-3,3-5,5-999"` : min incluse, max exclue ; plages illisibles
  ignorées, tri par min. Au-delà de la dernière plage : pas de zone.
- Grille centrée sur `grid_origin_lat/lng`, à défaut sur l'établissement
  (`merchant.lat/lng`), couvrant au moins 10 km ; hors grille : pas de zone.
- **O** pour l'ouest (décision d'Ilies, 2026-11-08). L'ancien moteur (API PHP)
  écrivait **W** avec un cap non corrigé de la latitude ; ces codes hérités
  (10 clients sur staging) sont remplacés à la prochaine commande en livraison
  du client.
- Une position absente ou à 0,0 (enregistrée ainsi par Deliveroo pour les
  commandes sans adresse et par d'anciens imports) n'a pas de zone.
- `customer_zone_code` est un varchar(4) : un libellé plus long (16 secteurs et
  au moins 10 plages, ex. NNE10) n'est pas enregistré plutôt que tronqué.

## Enregistrement

Un seul point d'écriture, `OrdersLifeCycleRepository.upsertCustomer`
([order_life_cycle/repository.go](../internal/modules/order_life_cycle/repository.go)),
commun à la création et à la modification de commande et donc à tous les flux :
caisse, ScanNOrder, borne, Uber Eats / Deliveroo. Après l'écriture du client,
`refreshCustomerZone` appelle `deliveryzone.RefreshCustomerZone`, qui relit
`customer_lat/lng` (que la commande vient de mettre à jour), la découpe actuelle
et la position de l'établissement, puis écrit la zone.

Quand :

- toute commande **en livraison** à l'adresse habituelle du client ;
- toute autre commande qui transmet des coordonnées (elles écrasent
  `customer_lat/lng`, la zone doit suivre la position enregistrée).

Effets :

- adresse changée → nouvelle zone ; découpe changée → zone recalculée à la
  commande suivante ;
- hors découpe, sans position valide ou sans découpe → zone **effacée** (un
  ancien code ne survit pas) ;
- jamais bloquant : point de sauvegarde, échec journalisé (`delivery zone not
  refreshed`), la commande est créée normalement.

ScanNOrder : `CreateOrderSNO` passe par `order_life_cycle.CreateOrder`. Un
client reconnu par son téléphone garde l'adresse et les coordonnées saisies
dans la commande (`GetCustomerByPhone` ne fait que reprendre son
`customer_id`), donc la zone suit l'adresse de la commande.

## Adresse ponctuelle

Une commande avec `use_customer_temporary_address` écrit ses coordonnées dans
`customer_temporary_*` et ne touche ni l'adresse habituelle ni sa zone. Il n'y a
pas de colonne pour la zone d'une adresse ponctuelle : la relecture des
commandes (`OrdersFetcher.FetchAndBuildOrders`) la calcule à la volée pour les
livraisons concernées (découpe lue une fois, seulement s'il y en a) et la
renvoie dans `customer.customer_zone_code` à la place de celle de l'adresse
habituelle.

## Affichage

- **POS, production** : `ProductionOrderHeaderCell` affiche la zone dans une
  pastille à droite de l'adresse, uniquement quand l'adresse est affichée
  (livraison faite par le restaurant ; pas pour Uber Eats / Deliveroo).
  Champ `customerZoneCode` de `CustomerResponse` / `CustomerDto`.
- **Back-office** : la fiche client (`CustomerDetailsSheet.tsx`) affiche
  toujours un encart factice (« Zone de Livraison A », « ~2.5km ») — à
  remplacer par la vraie zone, non fait.

## Tests

- `go test ./internal/modules/deliveryzone/` : convention (mêmes cas que le
  back-office), longueur de colonne, positions invalides.
- `TestDeliveryZone_Postgres` (`-tags postgres_integration`,
  order_life_cycle) : par le vrai `CreateOrder` et le vrai fetcher —
  livraison, changement d'adresse, à emporter avec et sans coordonnées, adresse
  ponctuelle, commande sans coordonnées, hors découpe, découpe retirée. Passé
  sur staging le 2026-11-08.
- POS : `test/ui/views/production/order/production_order_header_cell_test.dart`.

## Déploiement

Aucune migration (la colonne existe). Ordre indifférent : un POS sans la mise à
jour ignore le champ, un POS à jour sans l'API affiche simplement rien.

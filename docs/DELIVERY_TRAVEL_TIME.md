# Deadline de production = estimated_ready − temps de trajet livraison

Document vivant : implémentation, process d'utilisation, et **journal des décisions** tenu au fil de
l'eau (y compris les décisions annulées et pourquoi). Mis à jour à chaque étape livrée.

- **Démarré le** : 2026-08-28 — **étape 2 le 2026-08-29** (deux heures dérivées : deadline production
  vs arrivée livreur)
- **Périmètre** : API Go (`ib-welloresto-api`), POS Flutter (`wello_resto_flutter`), ScanNOrder React
  (`wello-resto-scannorder`), **wello-back-office** (depuis l'étape 2, retrait de `dateCall`)
- **État** : ✅ code livré dans les 4 dépôts — **migrations 102 et 103 pas encore appliquées**, **aucune
  vérification d'exécution réelle** (pas de DB de dev connectée dans cette session) — voir [§5](#5-vérifications-effectuées)
- **Reste à faire avant production** : appliquer `migrations/todo/102_delivery_travel_seconds.up.sql`
  **et** `103_production_ready_delivery_arrival.up.sql`, QA manuelle sur les 3 apps (voir [§6](#6-avancement)),
  laisser tourner le cron une fois pour vérifier le premier calcul de moyenne

---

## 1. Contexte : ce qui existait avant

Le restaurateur programme une commande livraison (fonction *schedule* du POS) en choisissant une date
de livraison. Cette date est écrite telle quelle dans `orders.estimated_ready` et utilisée partout
ensuite comme si c'était la date de fin de cuisine — alors que c'est la date de livraison **promise au
client**. Le temps de trajet n'était jamais soustrait :

| Constat | Emplacement |
|---|---|
| `orders.estimated_ready` est le seul champ de timing, écrit tel quel par le client | [order_life_cycle/repository.go](../internal/modules/order_life_cycle/repository.go) `insertOrderBase`/`updateOrderBase` |
| Le module Google Maps est un **pur passthrough**, aucune durée n'est parsée ni stockée | [googlemaps/service.go](../internal/modules/googlemaps/service.go) `GetAndLogRoute` |
| Le POS Flutter calcule déjà un ETA Google Maps live dans le formulaire client, mais l'affiche puis le jette | `customer_trafic_info.dart` (`_trafficInfos.durationInTrafficValue`), jamais persisté dans `CustomerSession` |
| OSRM n'existe **pas** côté API — uniquement côté frontend ScanNOrder, pour le tracking **post-commande** | `wello-resto-scannorder/src/hooks/useOsrmRoute.ts`, consommé par `useDeliveryTracking.ts` |
| ScanNOrder estimait `estimated_ready = now + 20min` en dur pour toute commande immédiate, et affichait `"15–25 min"` statique ou recopiait le temps de préparation cuisine sous le libellé « Temps de livraison » | `CheckoutFlow.tsx`, `CatalogueHeader.tsx` |
| Aucun module ne sort/filtre les commandes par `estimated_ready` côté backend (`orderByFilter` toujours vide) — le tri se fait côté client | [orders/orders_fetcher_builder.go](../internal/modules/orders/orders_fetcher_builder.go) |

Précédent réutilisé : le cron `UpdateAverageDistributionTime` (`@every 15m`, fenêtre glissante 24h,
upsert dans `average_distribution_time`) — voir [distribution.go](../internal/tasks/distribution.go).
C'est le patron répliqué pour le temps de trajet.

---

## 2. Journal des décisions

### D1 — Stocker le temps de trajet, pas le recalculer a posteriori
**Décision** : nouvelle colonne `orders.delivery_travel_seconds` (secondes, nullable), écrite au
moment de la création/mise à jour de la commande, à côté de `estimated_ready`.
**Pourquoi** : le temps de trajet dépend de l'adresse et de l'instant de la demande — il n'est pas
reconstructible plus tard sans refaire un appel routing. Le capturer au moment où il est déjà connu
(Google Maps/OSRM déjà interrogés côté client) est gratuit ; le recalculer après coup ne le serait pas.

### D2 — Réutiliser le routing déjà présent côté client, ne rien ajouter côté API
**Décision initiale envisagée** : ajouter un module OSRM proxifié côté backend (symétrique à
`googlemaps`), pour que les deux frontends passent par l'API.
**Tranchée par l'utilisateur** : non — OSRM reste côté ScanNOrder (appel direct navigateur, comme pour
le tracking post-commande), Google Maps reste côté POS Flutter (appel déjà existant dans le formulaire
client, `GET /external/routes`). Le backend se contente de **stocker** la valeur reçue.
**Pourquoi** : évite d'ajouter un nouveau module/dépendance côté API pour une fonctionnalité qui
existe déjà côté client des deux côtés — la logique de routing appartient au client qui connaît le
device/OSRM public/Google Maps ; centraliser aurait été de la duplication d'infrastructure sans gain
net pour ce périmètre.

### D3 — Le type de commande prime toujours sur la valeur envoyée par le client
**Décision** : `resolveDeliveryTravelSeconds` (backend) vérifie `order_type == DELIVERY` **avant**
de faire confiance à `req.Order.DeliveryTravelSeconds` fourni par le client, pas après.
**Pourquoi** : le POS Flutter garde l'état adresse/trajet en session (`CustomerSession`) même quand le
restaurateur repasse la commande en emporter/sur place sans vider ce champ (`clear()` ne réinitialise
déjà pas `addressLat`/`addressLng` aujourd'hui — comportement préexistant, non corrigé ici). Sans ce
garde-fou côté serveur, un `delivery_travel_seconds` obsolète aurait pu être persisté sur une commande
non-livrée. Écrit d'abord comme `if DeliveryTravelSeconds != nil { return ... }` puis corrigé pendant
la même session après relecture — voir [repository.go](../internal/modules/order_life_cycle/repository.go)
`resolveDeliveryTravelSeconds`.

### D4 — Moyenne glissante 24h comme filet de sécurité, pas comme source principale
**Décision** : nouveau cron `UpdateAverageDeliveryTime` (`@every 15m`), fenêtre 24h glissante,
minimum 3 commandes livrées pour produire une moyenne, bornes de sécurité [60s, 3600s] — calqué sur
`UpdateAverageDistributionTime` (mêmes constantes de fenêtre, seuils propres au volume plus faible des
commandes livraison). Alimente `average_delivery_time(merchant_id, delivery_time_seconds)`.
**Usage** : (a) filet de sécurité backend quand une commande livraison a une adresse mais pas de valeur
live fournie ; (b) estimation pré-checkout ScanNOrder tant que l'adresse n'est pas encore connue/résolue
par OSRM.
**Pourquoi pas de constante statique (15 min) partout** : l'utilisateur avait initialement proposé un
forfait fixe pour ScanNOrder ; répliquer le cron existant donne une moyenne réelle par marchand,
recalculée automatiquement, sans coût d'implémentation supplémentaire (le patron existe déjà). La
constante `DEFAULT_DELIVERY_SECONDS = 15*60` reste le repli de dernier recours (aucune donnée du tout).

### D5 — Le badge client production reste sur `estimated_ready` brut
**Décision** : côté écran production (POS Flutter), seuls le tri de la liste et les seuils internes
(`isScheduledWithinClassicLeadTimeWindow`, `isElapsedSinceLastUpdateLate`) utilisent la deadline
effective (`estimated_ready - delivery_travel_seconds`, via le nouvel helper
`ProductionController.effectiveProductionDeadline`). Le badge affiché au personnel
(`production_order_header_values.dart` / `WelloLiveOrderAgeBadge`) continue d'afficher `estimated_ready`
brut.
**Pourquoi** : `estimated_ready` est l'heure promise au client (utile au personnel pour juger si la
commande part à temps chez le client) ; la deadline effective est un signal de tri interne. Afficher
deux heures différentes pour la même commande aurait été plus confus qu'utile.

### D6 — Convention de nommage : suffixe `_seconds` explicite, contrairement à l'existant
**Décision** : `delivery_travel_seconds` / `average_delivery_time.delivery_time_seconds`, avec suffixe
explicite — alors que `average_distribution_time.distribution_time` (existant) ne porte l'unité que
dans un commentaire SQL libre.
**Pourquoi** : la recherche préalable a confirmé une incohérence dans le repo (`preparation_time` en
minutes vs `distribution_time` en secondes, distingués seulement par commentaire). Autant ne pas
reproduire l'ambiguïté sur les nouvelles colonnes, sans toucher aux anciennes (hors périmètre).

---

## Étape 2 (2026-08-29) — deux heures dérivées : deadline production vs arrivée livreur

L'implémentation de l'étape 1 soustrayait toujours `estimated_ready - delivery_travel_seconds` pour
la deadline production, **quel que soit** l'état `scheduled`. Faux pour une commande non programmée :
`estimated_ready` y est déjà auto-calculé par la cuisine (`ComputeEstimatedReady`, temps de prépa
seul) — soustraire le trajet une deuxième fois avançait la deadline au lieu de calculer l'heure
d'arrivée livreur. Sémantique corrigée, demandée et validée par l'utilisateur :

- **Commande programmée** (`scheduled=true`) : `estimated_ready` = heure choisie par le client/staff
  = heure à laquelle le livreur est à la porte. Deadline production = `estimated_ready - delivery_travel_seconds`.
- **Commande non programmée** (`scheduled=false`) : `estimated_ready` = heure de fin cuisine
  auto-calculée. Heure d'arrivée livreur estimée = `estimated_ready + delivery_travel_seconds`.

### D7 — Deux colonnes à sens fixe, pas une colonne à double sens
**Décision initiale envisagée** : une seule colonne `production_or_delivery_estimate`, dont le sens
dépendrait de `scheduled`.
**Écartée à la relecture du plan**, sur question directe de l'utilisateur ("est-ce que ce ne serait
pas plus pertinent et plus simple en deux colonnes ?") : une colonne à double sens reproduit
exactement le problème qu'on corrige sur `estimated_ready` (une valeur, deux significations selon le
contexte) — mauvais pour la propreté (tribal knowledge requise pour lire la base en SQL direct),
l'entretien (la règle `scheduled ? - : +` devrait être réappliquée par chaque consommateur : Go, Dart,
futur reporting), et l'évolutivité (une requête analytique sur "heure de livraison" deviendrait un
`CASE WHEN scheduled...` au lieu d'un `SELECT` simple).
**Retenu** : `orders.production_ready_at` (deadline cuisine, renseignée pour **toute** commande) et
`orders.delivery_arrival_at` (heure d'arrivée livreur, `NULL` pour le non-livraison — un `NULL` qui a
un sens, pas une donnée manquante), chacune à sens unique, toujours correctement remplie côté backend
(`resolveProductionReadyAt`/`resolveDeliveryArrivalAt`, [order_life_cycle/repository.go](../internal/modules/order_life_cycle/repository.go)).
Pas de backfill : aucune commande réelle n'a encore `delivery_travel_seconds` (migration 102 pas
appliquée), rien à recalculer rétroactivement.

### D8 — Correction d'un bug latent : `scheduled` pouvait mentir sur la nature de `estimated_ready`
**Découverte** : `ComputeEstimatedReady` (fallback auto-calculé, temps de prépa seul) est appliqué
**avant** que `resolveIsScheduled` ne s'exécute (`repository.go` `CreateOrder`/`UpdateOrder`). Si un
client envoyait `is_scheduled=true` avec `estimated_ready` vide, le fallback remplissait la valeur, et
`resolveIsScheduled` ne voyait plus une valeur vide — `scheduled=true` persistait avec une date qui
n'était pourtant pas un choix client, contredisant l'intention déjà documentée dans le code
(commentaire de `resolveIsScheduled`, non appliqué dans ce cas précis).
**Pourquoi ça devait être corrigé ici** : D7 dépend entièrement de la fiabilité de `scheduled` pour
choisir la bonne direction (+/-). Un `scheduled` menteur aurait fait calculer `production_ready_at`/
`delivery_arrival_at` dans le mauvais sens pour ce cas précis.
**Correction** : aux deux points d'appel de `ComputeEstimatedReady`, dès que le fallback est
effectivement appliqué, `req.Order.IsScheduled` est forcé à `false` à cet endroit — la seule source
fiable pour savoir "cette valeur n'a pas été fournie par le client".

### D9 — `dateCall` n'était pas mort, contrairement à la demande initiale
**Hypothèse de départ (utilisateur)** : `dateCall` inutilisé, à remplacer par le nouveau champ.
**Recherche** : lu dans 3 endroits de wello-back-office (`analyticsService.ts`, `customersService.ts`,
**`DashboardOrderHistory.tsx`** — écran routé/vivant), toujours en fallback `callHour || creation_date`.
Dans les faits `dateCall` valait quasi toujours `creation_date` (écrit à `UTC_TIMESTAMP()` à la
création, jamais une valeur distincte) — un fallback qui ne se déclenchait presque jamais, mais bien
réel.
**Décision, confirmée par l'utilisateur** : supprimer `dateCall` quand même (migration 103), et migrer
les 3 lectures back-office vers `creation_date` seul — changement mécanique et sûr vu l'équivalence de
fait constatée.

### D10 — Correction ScanNOrder : `estimated_ready` non-programmé redevient temps cuisine seul
**Constat** : l'étape 1 calculait `estimated_ready = now + prépa + trajet` pour une commande livraison
"Standard" (non programmée) — cohérent avec l'ancien comportement (une seule heure, combinée), plus
avec la sémantique de l'étape 2 (non-programmé ⇒ `estimated_ready` = temps cuisine auto, sans trajet).
**Correction** : `CheckoutFlow.tsx` `handleConfirm`, cas DELIVERY non programmé, `estimated_ready =
now + prépa` (le trajet n'y est plus ajouté). `delivery_travel_seconds` continue d'être envoyé
séparément pour que le backend calcule `delivery_arrival_at = estimated_ready + trajet`.
**Ce qui ne change pas** : l'affichage client (en-tête catalogue, option "Standard" au checkout)
continue de combiner prépa+trajet pour l'estimation montrée — seule la valeur envoyée comme
`estimated_ready` au backend change.

### D11 — Plus de recalcul client des deadlines : `OrderDto` lit directement le backend
**Décision** : `ProductionController.effectiveProductionDeadline` (étape 1, toujours
`estimated_ready - travel`, faux pour le cas non-programmé et une règle métier dupliquée côté client)
est retiré. `OrderDto` gagne les champs directs `productionReadyAt`/`deliveryArrivalAt` (miroir des
colonnes backend, déjà correctement calculées) et un seul getter `kitchenReadyAt =>
productionReadyAt ?? estimatedReady` (repli pour les commandes antérieures à la migration).
**Pourquoi** : la règle `scheduled ? - : +` ne doit vivre qu'à un seul endroit (le backend) — la
dupliquer côté Dart aurait recréé exactement le problème identifié en D7 pour la base de données, côté
code cette fois.
**Effet de bord découvert en implémentant** : un troisième appelant de l'ancienne méthode existait,
non repéré pendant la planification — [production_load_slots.dart](../lib/ui/widgets/production/sidebar/production_load_slots.dart),
l'indicateur de charge de production utilisé à la fois sur l'écran PRODUCTION et sur "commandes en
cours" (`combineAllProfiles`). Migré vers `order.kitchenReadyAt` comme les autres, confirmant au
passage que ce widget est bien partagé entre les deux écrans visés par D12.

### D12 — Affichage des deux heures, labellisées
- **Écran production** ([production_order_header_values.dart](../lib/ui/views/production/order/production_order_header_values.dart)) :
  le badge d'urgence (`WelloLiveOrderAgeBadge`) reçoit désormais `order.kitchenReadyAt` au lieu de
  `order.estimatedReady` brut — corrige aussi sa coloration, qui utilisait la mauvaise heure pour une
  commande programmée. Ligne secondaire ajoutée (icône livreur + heure, `DateHelper.formatProductionScheduleDateTime`),
  visible seulement quand `deliveryArrivalAt` est connu.
- **Écran « En cours »** ([order_cell.dart](../lib/ui/widgets/home/order_list/order_cell.dart), confirmé
  par l'utilisateur comme l'écran visé — l'équivalent back-office existe dans le code mais n'est relié
  à aucune route) : badge sur `order.deliveryArrivalAt ?? order.kitchenReadyAt` (ETA côté client :
  arrivée livreur pour une livraison, sinon deadline cuisine — exactement ce que représentait
  `estimated_ready` avant sa spécialisation). Nouveau libellé `"Cuisine HH:mm · Livraison HH:mm"`
  quand les deux heures sont connues, prioritaire sur l'ancien libellé de créneau programmé (plus
  informatif dans tous les cas).

### D13 — La deadline cuisine manquait encore en heure d'horloge sur l'écran production
**Question posée par l'utilisateur après livraison de D12** : "est-ce clair pour la cuisine de savoir
à quelle heure sortir quelle commande ?"
**Constat, honnête** : non, pas totalement. La carte ne montrait aucune heure d'horloge pour la
deadline cuisine elle-même — seulement le badge `WelloLiveOrderAgeBadge` (temps écoulé + couleur
d'urgence, jamais une heure absolue). La seule heure en clair était "🛵 HH:mm" (arrivée livreur,
ajoutée en D12) — une heure **plus tardive** que la vraie deadline cuisine pour une commande
programmée, avec un vrai risque de lecture erronée ("j'ai jusque-là") sous pression.
**Correction** : ligne "🍳 HH:mm" ajoutée à côté de "🛵 HH:mm" dans
[production_order_header_values.dart](../lib/ui/views/production/order/production_order_header_values.dart),
sur `order.kitchenReadyAt` (déjà utilisé pour la couleur du badge, maintenant aussi affiché en clair).
Les deux lignes partagent un composant `_TimeLine` (icône + heure locale) pour rester visuellement
cohérentes ; la ligne cuisine est en gras (`FontWeight.w700`) — c'est l'info la plus actionnable pour
ce poste, le badge élapsé reste utile en complément pour repérer une commande qui traîne, pas à sa
place.

### D14 — Les deux heures remontées en haut de la carte, à la place de l'heure programmée
**Demande utilisateur** : en haut de la carte, `ScheduleTimerInfo` n'affichait (uniquement pour les
commandes programmées) que l'heure programmée elle-même — qui est la date de livraison, pas la date
de production, le même problème de fond que D7/D13 mais au sommet de la carte cette fois, l'endroit le
plus visible. Demande : y remplacer cet affichage par l'heure de production (à gauche), et l'heure
d'arrivée livreur si pertinent (à droite, même ligne).
**Décision** : nouveau widget [ProductionTimingRow](../lib/ui/views/production/order/production_timing_row.dart),
affiché dès que `order.kitchenReadyAt` existe (donc pour **toute** commande, programmée ou non — pas
seulement les programmées comme l'ancien `ScheduleTimerInfo`), avec le même style visuel (icône 24px,
texte 20px, `AppColor.secondaryColor`) puisqu'il reprend le même emplacement bien visible.
`MainAxisAlignment.spaceBetween` pousse l'heure de livraison à l'extrémité droite de la carte quand
elle existe.
**`ScheduleTimerInfo` non touché** : toujours utilisé tel quel par [schedule_order_card.dart](../lib/ui/widgets/production/schedule_control/schedule_order_card.dart)
(bannière des commandes programmées à venir) — un contexte différent où "l'heure programmée" est
justement l'info recherchée (un aperçu de ce qui arrive), pas une carte de production active.
**Conséquence** : les lignes "🍳"/"🛵" ajoutées en D13 dans `ProductionOrderHeaderValues` (bas de
carte) sont retirées — désormais dupliquées avec `ProductionTimingRow` en haut de carte. Cette zone ne
garde que le badge de temps écoulé (`WelloLiveOrderAgeBadge`), dont la couleur d'urgence continue de
dépendre de `order.kitchenReadyAt`.

### D15 — Plus de mot "Demain" dans le formatage : débordement de carte
**Constat utilisateur** : sur `ProductionTimingRow` (D14), quand la deadline cuisine **et** l'heure de
livraison tombent toutes les deux "demain", les deux blocs affichaient "Demain - HH:mm" côte à côte —
assez long pour faire déborder la carte.
**Décision** : [DateHelper.formatProductionScheduleDateTime](../lib/helpers/date_helper.dart) (partagée
avec `ScheduleTimerInfo`/la bannière programmée, seul autre appelant) n'a plus que deux cas au lieu de
trois : aujourd'hui → `"HH:mm"` seul ; tout le reste (demain compris) → `"dd/MM - HH:mm"`. Le mot
"Demain" et le format `"Lun 16 sept - HH:mm"` (jour de semaine + mois en toutes lettres) disparaissent
au profit d'une date chiffrée courte, sans ambiguïté quel que soit le jour, et systématiquement plus
courte que les deux formes qu'elle remplace.

### D16 — Toujours trop large : date affichée une seule fois pour toute la ligne
**Constat utilisateur** : même raccourci en `dd/MM`, `ProductionTimingRow` restait trop large — la date
était répétée sous les deux heures alors qu'elles appartiennent à la même commande et tombent quasi
toujours le même jour (livraison peu après la cuisine).
**Décision** (deux options proposées avec aperçu ASCII, celle-ci retenue par l'utilisateur) : la date
n'apparaît plus qu'une fois pour toute la ligne — calculée une seule fois à partir de l'heure cuisine
(`DateHelper.formatDateLabelIfNotToday`, nouvelle fonction extraite de
`formatProductionScheduleDateTime`, qui la réutilise) — suivie des deux heures seules (`HH:mm`, sans
date propre) sous chaque icône. `ScheduleTimerInfo`/la bannière programmée continue d'utiliser
`formatProductionScheduleDateTime` (comportement inchangé, une seule heure par carte là-bas).
**Police/placement, sur demande explicite de conseil** : icônes 24px→18px, heures 20px→16px (trop
grandes à deux blocs sur une ligne) ; deadline cuisine en gras (`w700`, l'info la plus actionnable),
livraison en poids normal ; date partagée en 12px atténué (repère secondaire, pas l'info principale),
placée avant l'icône 🍳 plutôt que sur sa propre ligne pour garder l'effet "coup d'œil" sur une seule
ligne.
**Simplification assumée** : la date partagée est calculée sur l'heure cuisine, pas sur chaque heure
individuellement. Si l'arrivée livreur tombe rarement sur un autre jour que la cuisine (commande passée
juste avant minuit), elle s'affiche quand même sans sa propre date — imprécision mineure et rare,
jugée acceptable plutôt que de complexifier le composant pour un cas limite.

---

## 3. Architecture

```
POS Flutter (staff programme une commande livraison)
  CustomerFormView (saisie adresse) ──> GET /external/routes (Google Maps, déjà existant)
      └─ CustomerSession.travelTimeSeconds (nouveau, capture ce qui était jeté)
             └─ OrderDraftManager.getOrCreatePendingOrder() ──> OrderPayload.delivery_travel_seconds
                                                                        │
ScanNOrder (client final, checkout DELIVERY)                           │
  CheckoutFlow ──> useOsrmRoute (déjà existant pour le tracking,       │
                    réutilisé ici avant commande) ──> delivery_travel_seconds
                                                                        │
                                                                        ▼
                                          POST /orders (create/update) ── order_life_cycle
                                                   resolveDeliveryTravelSeconds :
                                                     1) order_type != DELIVERY -> nil
                                                     2) valeur client fournie -> gardée telle quelle
                                                     3) adresse connue, pas de valeur -> average_delivery_time (fallback)
                                                     4) pas d'adresse -> nil
                                                   └─ orders.delivery_travel_seconds

Cron @every 15m : UpdateAverageDeliveryTime
  AVG(orders.delivery_travel_seconds) / marchand, fenêtre 24h, min 3 commandes
  └─ upsert average_delivery_time(merchant_id, delivery_time_seconds)

Production (écran cuisine, POS Flutter)
  effectiveProductionDeadline(order) = estimated_ready - (delivery_travel_seconds ?? 0)
  └─ utilisé pour le tri et les seuils de retard, pas pour le badge affiché au client/personnel
```

### Schéma

| Table / colonne | Type | Rôle |
|---|---|---|
| `orders.delivery_travel_seconds` | `INT NULL` | temps de trajet capturé pour **cette** commande (secondes) ; `NULL` = non-livraison ou pas encore connu |
| `average_delivery_time.merchant_id` | `VARCHAR(64)` PK | |
| `average_delivery_time.delivery_time_seconds` | `INT NOT NULL` | moyenne glissante 24h, recalculée toutes les 15 min |
| `average_delivery_time.created_at` / `updated_at` | `DATETIME` | audit |

Migration : `migrations/todo/102_delivery_travel_seconds.{up,down}.sql`.

### Surface de code livrée

| Élément | Emplacement |
|---|---|
| `OrderRequest.DeliveryTravelSeconds`, `Order.DeliveryTravelSeconds` | [create_order_models.go](../internal/models/create_order_models.go), [orders_model.go](../internal/models/orders_model.go) |
| `resolveDeliveryTravelSeconds`, écriture insert/update | [order_life_cycle/repository.go](../internal/modules/order_life_cycle/repository.go) |
| Lecture (SELECT + scan) pour la réponse `Order` | [orders/orders_fetcher_builder.go](../internal/modules/orders/orders_fetcher_builder.go) |
| `deliverytime.AverageSeconds` (lookup fallback) | [modules/deliverytime/estimate.go](../internal/modules/deliverytime/estimate.go) |
| `TasksManager.UpdateAverageDeliveryTime` (cron) | [internal/tasks/delivery_time.go](../internal/tasks/delivery_time.go), enregistré dans [cmd/api/tasks.go](../cmd/api/tasks.go) |
| `MerchantData.AverageDeliverySeconds` (exposé à ScanNOrder) | [scannorder/models.go](../internal/modules/scannorder/models.go), [scannorder/service.go](../internal/modules/scannorder/service.go) `computeGetMerchant` |
| `CustomerSession.travelTimeSeconds`, capture dans `_persistSessionData` | `wello_resto_flutter/lib/data/services/customer_session.dart`, `customer_form_view.dart` |
| `OrderDto.deliveryTravelSeconds`, lecture session → draft | `order_dto.dart`, `order_response.dart`, `order_draft_manager.dart` |
| Envoi au backend | `order_payload.dart`, `order_payload_mapper.dart` |
| `ProductionController.effectiveProductionDeadline` + 3 points d'usage (tri, 2 seuils) | `production_controller.dart`, `product_focus_orders_container.dart`, `classic_orders_container.dart` |
| `useOsrmRoute` réutilisé au checkout, payload, affichages | `wello-resto-scannorder/src/components/cart/CheckoutFlow.tsx`, `src/lib/api/payload.ts`, `src/lib/api/types.ts` |
| Affichage pré-checkout (moyenne cron) | `src/components/catalogue/CatalogueHeader.tsx`, `src/lib/utils/datetime.ts` (`DEFAULT_DELIVERY_SECONDS`) |

---

## 4. Process d'utilisation

### Pour le restaurateur (POS Flutter)
1. Saisir l'adresse du client dans le formulaire (`CustomerFormView`) : le badge trafic Google Maps
   s'affiche comme avant, et la durée est désormais capturée en session.
2. Programmer la commande (date/heure) — écran indépendant, aucun changement de flux.
3. Si l'adresse est saisie **après** la programmation : rien de spécial à faire, le pipeline
   session → draft → payload est rejoué à chaque (re)construction de la commande, comme pour les
   autres champs adresse.
4. Écran production : le tri et les indicateurs de retard tiennent désormais compte du trajet, sans
   changement visible dans le badge affiché.

### Pour le client (ScanNOrder)
1. En choisissant « Livraison » et une adresse, l'estimation affichée (en-tête catalogue, option
   « Standard » au checkout) combine désormais préparation + trajet réel (OSRM) dès que l'adresse est
   résolue, ou la moyenne du marchand en attendant.
2. Aucune action utilisateur supplémentaire requise.

### Pour l'exploitation
- Cron **`@every 15m`**, `UpdateAverageDeliveryTime` — tourne sur tous les environnements comme le
  reste des tâches (`cmd/api/tasks.go`, pas de gate par `ENV`).
- Tant qu'un marchand n'a pas 3 commandes livraison avec `delivery_travel_seconds` renseigné sur les
  dernières 24h, `average_delivery_time` n'a pas de ligne pour lui → repli sur
  `DEFAULT_DELIVERY_SECONDS` (15 min) côté ScanNOrder, et sur `nil` (pas de soustraction) côté
  production tant qu'aucune valeur n'est disponible du tout.
- **Migration requise avant tout déploiement** : `102_delivery_travel_seconds.up.sql` (ajout colonne +
  nouvelle table). Pas encore appliquée à ce stade.

---

## 5. Vérifications effectuées

**Aucune vérification d'exécution réelle** n'a été faite dans cette session : pas de base de données
de dev connectée, pas de serveur API lancé, pas d'app Flutter ni de dev server React démarrés. Ce qui a
été vérifié :

| Dépôt | Commande | Résultat |
|---|---|---|
| `ib-welloresto-api` | `go build ./...` | ✅ compile |
| `ib-welloresto-api` | `go vet ./internal/modules/order_life_cycle/... ./internal/modules/orders/... ./internal/modules/scannorder/... ./internal/modules/deliverytime/... ./internal/tasks/... ./cmd/api/...` | ✅ (2 warnings préexistants, sans rapport avec ce chantier — fichier de test `customers.NewCustomersService` et lock-copy `routes.go`, déjà présents avant cette session) |
| `ib-welloresto-api` | `go test ./internal/modules/deliverytime/... ./internal/tasks/...` | ✅ (`deliverytime` : pas de tests ; `tasks` : suite existante toujours verte) |
| `ib-welloresto-api` | `gofmt -l` sur tous les fichiers Go touchés | ✅ aucun problème de formatage restant |
| `wello_resto_flutter` | `dart analyze` sur les 9 fichiers Dart touchés | ✅ *No issues found!* |
| `wello-resto-scannorder` | `npx tsc --noEmit` | ✅ exit 0, aucune erreur |
| `wello-resto-scannorder` | `npx eslint` sur les 5 fichiers touchés | ✅ exit 0 |
| `ib-welloresto-api` (étape 2) | `go build ./...` + `go vet` + `go test` (mêmes packages) + `gofmt -l` | ✅ (mêmes 2 warnings préexistants, toujours sans rapport) |
| `wello-back-office` (étape 2) | `npx tsc --noEmit` après retrait de `callHour` | ✅ exit 0 |
| `wello_resto_flutter` (étape 2) | `dart analyze` ciblé (9 fichiers) puis `dart analyze` **projet entier** | ✅ 0 erreur ; 140 infos préexistantes sans rapport (avoid_print, deprecated_member_use, etc. — vérifié qu'aucune ne cite les fichiers/champs touchés) |
| `wello-resto-scannorder` (étape 2) | `npx tsc --noEmit` + `npx eslint` sur `CheckoutFlow.tsx` | ✅ exit 0 |

**Non vérifié — à faire avant mise en production** :
- Application réelle de la migration 102 (MySQL de dev/staging).
- Test bout-en-bout POS : création d'une commande livraison avec adresse, vérification en base de
  `delivery_travel_seconds`, vérification visuelle de l'écran production.
- Test bout-en-bout ScanNOrder : parcours livraison jusqu'au checkout, vérification du payload réseau
  et de l'affichage.
- Premier passage du cron `UpdateAverageDeliveryTime` en conditions réelles (aucune commande
  historique avec `delivery_travel_seconds` n'existe avant ce déploiement — la table restera vide
  jusqu'à ce que 3 commandes livraison passent avec le nouveau code).

---

## 6. Avancement

| Étape | État |
|---|---|
| Backend : schéma + persistance + lecture + fallback + cron + exposition ScanNOrder | ✅ codé, non vérifié en exécution |
| POS Flutter : capture Google Maps existant, propagation, écran production | ✅ codé, `dart analyze` propre |
| ScanNOrder : réutilisation OSRM existant, payload, affichages | ✅ codé, `tsc`/`eslint` propres |
| Migration appliquée (dev/staging/prod) | ❌ pas fait (102 **et** 103) |
| QA manuelle (3 apps) | ❌ pas fait |
| Vérification du premier calcul de moyenne par le cron | ❌ pas fait |
| Étape 2 : `production_ready_at`/`delivery_arrival_at` (backend + persistance + lecture) | ✅ codé, non vérifié en exécution |
| Étape 2 : fix `resolveIsScheduled`/`ComputeEstimatedReady` | ✅ codé |
| Étape 2 : retrait `dateCall`/`callHour` (backend + wello-back-office) | ✅ codé, `tsc` propre |
| Étape 2 : correction ScanNOrder (`estimated_ready` non-programmé) | ✅ codé |
| Étape 2 : `OrderDto.kitchenReadyAt`/`deliveryArrivalAt`, écrans production + « En cours » | ✅ codé, `dart analyze` propre |

---

## 7. Références

- Plan initial validé par l'utilisateur (étape 1) et plan de l'étape 2 : `C:\Users\Ilies\.claude\plans\magical-strolling-puddle.md`
  (le fichier est réutilisé d'une étape à l'autre — son contenu reflète l'étape la plus récente, pas
  un historique)
- Patron de cron répliqué : [internal/tasks/distribution.go](../internal/tasks/distribution.go)
- Passthrough Google Maps existant (non modifié) : [internal/modules/googlemaps/](../internal/modules/googlemaps/)
- Hook OSRM existant (non modifié, réutilisé) : `wello-resto-scannorder/src/hooks/useOsrmRoute.ts`
- Migration 103 : [migrations/todo/103_production_ready_delivery_arrival.up.sql](../migrations/todo/103_production_ready_delivery_arrival.up.sql)

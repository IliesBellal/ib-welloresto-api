# Audit — Commandes Uber Eats livrées par l'établissement (BYOC / self-delivery)

> Objectif : vérifier le bon fonctionnement du cycle de vie complet d'une commande Uber Eats
> livrée par le livreur du restaurant (BYOC = *Bring Your Own Courier*, aussi appelé
> *self-delivery* côté Uber), de la réception de la commande à la fin de la livraison,
> incluant la synchronisation des statuts entre Uber Eats et WelloResto, et l'intégration
> dans l'app POS Flutter (`wello_resto_flutter`).
>
> Audit du 2026-08-19. Aucune modification de code n'a été effectuée — ce document est
> uniquement le constat de l'existant.
>
> Tags utilisés (convention reprise de `docs/DELIVERY_API_AUDIT.md`) :
> - `[EXISTE]` — fonctionnalité présente et opérationnelle.
> - `[PARTIEL]` — la brique existe mais est incomplète, buguée, ou non branchée.
> - `[ABSENT]` — rien d'équivalent trouvé, à construire.
> - `[À VÉRIFIER]` — hypothèse à confirmer (doc Uber, environnement réel, ou produit).

---

## 0. Résumé exécutif

Le système sait **recevoir** une commande Uber Eats, **l'accepter/la refuser** côté Uber, et
la **préparer** (statut "ready") — ces trois étapes fonctionnent et notifient bien Uber Eats.

En revanche, à partir du moment où le livreur du restaurant part effectivement en tournée
(BYOC), **plus aucune information ne remonte vers Uber Eats** : ni le départ en livraison,
ni la livraison effective. L'infrastructure d'appel existe pourtant côté backend
(`UberEatsBYOCStatusUpdate` / endpoint Uber `/restaurantdelivery/status`) mais n'est
**branchée nulle part** — deux `TODO` explicites dans `order_life_cycle/service.go` le
confirment. Cela signifie qu'aujourd'hui, **Uber Eats n'est jamais informé qu'une commande
BYOC a été livrée**, ce qui expose potentiellement l'établissement à des problèmes de SLA,
de facturation ou de litige côté Uber.

Un second problème structurant : la sécurité du webhook entrant Uber Eats est **cassée** —
la vérification de signature retourne toujours `true` et aucun secret n'est configuré.
N'importe qui connaissant l'URL du webhook peut forger un événement Uber (créer, annuler,
ou marquer livrée une commande).

Côté app POS Flutter, le flux fonctionne "par accident" plutôt que par conception : les
commandes Uber Eats BYOC rejoignent correctement le pool des tournées maison (le filtre
`fulfillmentType == DELIVERY_BY_RESTAURANT` ne fait aucune distinction de marque, donc
ça marche), mais **aucun écran ne signale au personnel qu'une commande Uber Eats est en
réalité livrée par un livreur maison** — ni en cuisine (écran de production), ni au
dispatch (hub de livraison), ni pour le livreur lui-même (interface mobile). C'est
exactement le point de vigilance demandé dans le brief de cet audit, et il n'est couvert
dans aucun des quatre écrans examinés.

---

## 1. Cycle de vie complet — schéma

```
UBER EATS                                   WELLORESTO API (backend Go)
──────────                                  ──────────────────────────
1. Nouvelle commande
   webhook "orders.notification" ────────▶  webhook/ubereats/handler/http_handler.go
                                              → service/service.go:95 ProcessEvent
                                              → service/order_notification.go:12
                                                 - idempotence (Redis event_id + ExistsByBrandOrderID)
                                                 - GetOrderByURL (récup commande complète chez Uber)
                                                 - mapping produits/modifiers (auto-création si inconnu
                                                   — ⚠ CASSÉ pour un modifier group inconnu, voir §4.7)
                                                 - order_mapper.go:11 MapUberOrderToRequest :
                                                     Brand = "UBER_EATS"
                                                     OrderType = DELIVERY (ou TAKE_AWAY si Uber envoie "PICK_UP")
                                                     FulfillmentType = valeur BRUTE de order.type Uber,
                                                       aucune constante/validation dédiée [À VÉRIFIER §4.1]
                                                     BrandStatus = order.CurrentState (brut)
                                                     MerchantApproval = PENDING_APPROVAL
                                                 - insert DB (ou auto-accept si store.AutoAcceptOrders)

2. Restaurateur accepte / refuse (POS)
   PATCH /orders/{id}/accept ──────────────▶  order_life_cycle/service.go:607 SetOrderAccepted
                        ◀──────────────────  POST /v1/delivery/order/{id}/accept        [EXISTE]
   PATCH /orders/{id}/deny   ──────────────▶  order_life_cycle/service.go:744 SetOrderDenied
                        ◀──────────────────  POST /v1/delivery/order/{id}/deny          [EXISTE]

3. Cuisine → prêt
   PATCH /orders/{id}/ready_for_distribution▶ order_life_cycle/service.go:828
                        ◀──────────────────  POST /v1/delivery/order/{id}/ready         [EXISTE]

4. Livreur maison part en livraison (BYOC)
   PATCH /orders/{id}/start_delivery ──────▶  order_life_cycle/service.go:719-726
                                                 case BrandUberEats :
                                                   // TODO recherche le bon endpoint Uber Eats
                                                   // (appel commenté, jamais exécuté)
                        ✗ AUCUN APPEL VERS UBER                                          [ABSENT]

   [Flux alternatif via tournée maison delivery_session :
    StartDeliverySession écrit brand_status='EN_ROUTE_TO_DROPOFF' en DB sans filtrer
    sur la marque et sans jamais appeler Uber non plus — même trou.]

5. Livraison effectuée
   PATCH /orders/{id}/delivered ───────────▶  order_life_cycle/service.go:322-325
                                                 case BrandUberEats :
                                                   // "No endpoint to call..."
                                                   return nil
                        ✗ AUCUN APPEL VERS UBER                                          [ABSENT]

   [Idem via delivery_sessions → MarkDeliveryStopDelivered → SetDelivered → même trou]

──────────────────────────────────────────────────────────────────────────────────────
ENTRANT — statuts d'un coursier fourni par UBER (marketplace/Uber Direct, PAS le BYOC) :
webhook "delivery.state_changed" ───────────▶ event_delivery_status.go:11 HandleDeliveryStatus
    SCHEDULED           → accepte la commande localement
    EN_ROUTE_TO_PICKUP, ARRIVED_AT_PICKUP → no-op
    EN_ROUTE_TO_DROPOFF → brand_status + delivery_start + isDistributed=true
    ARRIVED_AT_DROPOFF  → no-op
    FINISHED, COMPLETED → ferme la commande (fiscal, fidélité, reçu)
    FAILED              → state=CLOSED, brand_status=FAILED

webhook "orders.cancel" ────────────────────▶ event_order_canceled.go:8 HandleOrderCanceled
    → brand_status='CANCELED', state='CLOSED', payments désactivés
    ⚠ AUCUNE garde d'état (pas de vérif "commande encore ouverte") — voir §4.6
```

**Conséquence directe** : pour une commande BYOC, aucun de ces événements entrants
(`delivery.state_changed`) n'a de raison d'arriver côté Uber puisqu'il n'y a pas de
coursier Uber assigné — c'est justement pour cela que l'endpoint sortant BYOC
(`/restaurantdelivery/status`) existe. Or ce dernier n'est jamais appelé. **Le cycle de
vie BYOC est donc fonctionnellement à sens unique : WelloResto sait tout faire en interne,
mais Uber Eats n'apprend jamais que la commande a été prise en charge par le restaurant.**

---

## 2. Backend — inventaire détaillé

### 2.1 `[EXISTE]` — Réception, acceptation, refus, mise en préparation

| Étape | Déclencheur local | Appel sortant Uber | Fichiers |
|---|---|---|---|
| Réception commande | webhook `orders.notification` | — | `webhook/ubereats/service/order_notification.go:12-70` |
| Auto-accept si configuré | `store.AutoAcceptOrders` | `POST .../accept` | `order_notification.go:60-66` |
| Accept manuel | `SetOrderAccepted` | `POST /v1/delivery/order/{id}/accept` | `order_life_cycle/service.go:607-667`, `modules/ubereats/service.go:179-274`, `client.go:135-138` |
| Deny manuel | `SetOrderDenied` | `POST .../deny` | `order_life_cycle/service.go:744-805`, `modules/ubereats/service.go:292-350`, `client.go:206-210` |
| Ready for pickup | `SetReadyForDistribution` / `SetDistributedProducts` | `POST .../ready` | `order_life_cycle/service.go:828-881,528-582`, `modules/ubereats/service.go:387-415`, `client.go:220-224` |
| Cancel local | `DeleteOrder` | `POST .../cancel` | `order_life_cycle/service.go:210-215`, `modules/ubereats/service.go:353-384`, `client.go:213-217` |

Tous ces appels sortants sont **asynchrones fire-and-forget** (goroutine, timeout 30s) : le
PATCH local répond avant même que l'appel Uber ait abouti. En cas d'échec, seul un
`log.Error` est émis — pas de retry automatique.

### 2.2 `[ABSENT]` — Notification Uber au démarrage et à la fin de la livraison BYOC

- `order_life_cycle/service.go:719-726` (`StartDelivery`, cas `BrandUberEats`) : appel
  commenté, `TODO recherche le bon endpoint chez Uber Eats`.
- `order_life_cycle/service.go:322-325` (`DeliverOrder`, cas `BrandUberEats`) : commentaire
  `"No endpoint to call..."`, alors que l'endpoint existe (voir ci-dessous).
- L'infrastructure d'appel existe pourtant : `modules/ubereats/client.go:241-245`
  (`UpdateBYOCStatus`, `POST /v1/eats/orders/{id}/restaurantdelivery/status`) et
  `modules/ubereats/service.go:489-508` (`UberEatsBYOCStatusUpdate`) — **mais cette méthode
  n'est appelée par aucun autre fichier du dépôt.**
- Aucune des transitions du module `delivery_sessions` (`SelectDeliveryStop`,
  `MarkDeliveryStopArrived`, `MarkDeliveryStopDelivered`, `MarkDeliveryStopFailed`,
  `CancelDeliveryStop`) n'appelle non plus ce service — recherche exhaustive : 0 résultat.

### 2.3 `[À VÉRIFIER]` — Incertitudes documentées dans le code lui-même

- `modules/ubereats/service.go:495-499` : l'auteur du code note explicitement une incertitude
  sur l'identifiant à passer à `UpdateBYOCStatus` (`order_id` interne vs `brand_order_id`
  Uber), en s'appuyant sur une supposition tirée d'un ancien code PHP.
- Aucune constante ni doc dans le repo ne liste les valeurs de statut acceptées par
  l'endpoint Uber `/restaurantdelivery/status`.
- `order_mapper.go:53` : `FulfillmentType = &order.Type` (valeur brute Uber, sans
  validation). Le système suppose implicitement que pour du BYOC, Uber envoie
  `"DELIVERY_BY_RESTAURANT"` — coïncidant avec `models.FulfillmentTypeRestaurant` —
  mais rien dans le code, les tests ou les migrations ne le confirme. Aucune constante
  `FulfillmentTypeUberEats` n'existe (`internal/models/orders_model.go:13-14` ne définit
  que `FulfillmentTypeRestaurant` et `FulfillmentTypeDeliveroo`).

### 2.4 `[PARTIEL]` — Autres lacunes et bugs identifiés

- **Réconciliation d'état asymétrique** : `RecoverOrderState`/`FinishOrderIfDoesNotExist`
  (`modules/ubereats/service.go:418-486`) n'est appelée qu'après échec de
  `Deny`/`Cancel`/`SetOrderReady` — **jamais après échec d'`AcceptOrder`**
  (`service.go:260-262`). Si l'appel Uber d'acceptation échoue, la commande reste
  "acceptée" localement sans que rien ne tente de resynchroniser Uber.
- **Annulation entrante Uber sans garde d'état** (§4.6 ci-dessous) : `HandleOrderCanceled`
  peut écraser une commande déjà livrée/fermée.
- **Mapping produit auto pour modifier group inconnu cassé** :
  `CreateAttributeFromUberGroup` (`webhook/ubereats/repository/attribute_mapping_repo.go:22-49`)
  insère dans `configurable_attributes` sans `product_id` (colonne `NOT NULL`) → échoue
  systématiquement, confirmé par le test `repository/postgres_integration_test.go:142-156`.
  Conséquence concrète : une commande Uber Eats contenant un nouveau modifier group jamais
  vu **échoue à l'import** (500 au webhook → Uber retry → commande jamais créée tant que
  le mapping n'est pas pré-existant en base).
- **OAuth "Connecter mon compte Uber Eats" cassé** : `EnableIntegration`/`DisableIntegration`
  (`modules/ubereats/repository.go:123-164`) référencent des colonnes (`access_token`,
  `is_active`, `updated_at`) absentes de la table `integration_uber_eats` en base réelle
  (déjà documenté en commentaire dans le code source lui-même).

### 2.5 `[CRITIQUE]` — Sécurité : vérification de signature webhook inopérante

```go
// internal/webhook/ubereats/client/signature.go:9-17
func VerifySignature(body []byte, headerSignature string, secret string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := hex.EncodeToString(mac.Sum(nil))
    return true                              // toujours vrai, code mort en dessous
    return hmac.Equal([]byte(expected), []byte(headerSignature))
}
```

De plus, `cmd/api/routes.go:361-372` instancie le service webhook avec
`signatureSecret=""` et `systemToken=""` codés en dur, et `internal/config/ubereats.go`
n'a **aucun champ de config** pour un secret de signature webhook. Même en retirant le
`return true`, `handler/http_handler.go` ignore la valeur de retour de `VerifySignature`.

**Conséquence** : n'importe qui connaissant l'URL `/webhooks/uber-eats` peut poster un
événement forgé (créer une fausse commande, l'annuler, la marquer livrée) sans aucune
authentification.

### 2.6 Code mort / cosmétique (impact maintenabilité, pas fonctionnel)

- `modules/ubereats/client.go:140-143` `GetOrderByURLUsingOrderID` : nom trompeur, appelle
  en réalité `.../accept` (copie de `AcceptOrder`), non utilisé ailleurs.
- `webhook/ubereats/service/service.go:60-64` : interface `OrderLifeCycleService` obsolète,
  signature ne correspondant plus à l'implémentation réelle.
- `webhook/ubereats/models/event.go:25` `CourierTripID` décodé mais jamais lu.
- `modules/ubereats/service.go:387` : paramètre `merchantID` reçu mais jamais utilisé dans
  `SetOrderReady`.

### 2.7 Tests

Aucun test unitaire dans `internal/webhook/ubereats/service/` (0 fichier `*_test.go`). Les
seuls tests existants sont des tests d'intégration Postgres validant le SQL des repositories
— aucun scénario de bout en bout, aucune simulation de webhook signé, aucune couverture de
la branche `BrandUberEats` dans `order_life_cycle` ou `delivery_sessions`.

---

## 3. App POS Flutter (`wello_resto_flutter`) — inventaire détaillé

Le modèle `OrderDto` (`lib/models/orders/order_dto.dart:15-58`) expose correctement tous
les champs bruts nécessaires : `brand`, `brandStatus`, `brandOrderId`, `brandOrderNum`,
`fulfillmentType` (typé `FulfillmentTypeEnum?`), `orderType`, `deliveryStop`.

### 3.1 Écran des commandes en cours — `[PARTIEL]`

**Fichiers** : `lib/ui/widgets/home/order_list/order_cell.dart`,
`order_type_labelling.dart`

`OrderTypeLabelling` affiche un badge orange "Uber Eats" dès que `brand == BrandEnum.uberEats`
(`order_type_labelling.dart:29-37`) — mais **`fulfillmentType` n'est jamais lu** dans ce
widget. Une commande Uber Eats BYOC affiche exactement le même badge qu'une commande Uber
Eats livrée par un coursier Uber.

### 3.2 Écran de production — `[PARTIEL]`

**Fichiers** : `production_order_header_cell.dart`, `production_grouped_brand_layout.dart`

Le regroupement par colonnes sépare les commandes strictement par `brand`
(`RESTAURANT`/`UBER EATS`/`DELIVEROO`, `production_grouped_brand_layout.dart:28-63`). Une
commande Uber Eats BYOC finit dans la colonne "UBER EATS" au même titre qu'une commande
livrée par un coursier Uber — **aucun badge supplémentaire n'indique "livraison maison"**
au personnel en cuisine, alors que c'est précisément l'information critique pour savoir
si un livreur du restaurant doit être mobilisé. Seul le **ticket imprimé**
(`receipt_bytes_builder.dart:2048-2059`) fait cette distinction via texte, mais uniquement
sur papier, pas à l'écran.

> **Correction apportée après vérification directe du backend** : une hypothèse initiale de
> cet audit suggérait un bug bloquant — le filtre `order.brand == "WELLO_RESTO"` utilisé
> pour la colonne "RESTAURANT" (`production_grouped_brand_layout.dart:48-50`) pourrait ne
> jamais matcher si le backend renvoie `brand = null` pour les commandes maison. **Vérifié
> et infirmé** : `order_life_cycle/repository.go:1864-1865` (`setOrderDefaults`) écrit
> explicitement `Brand = "WELLO_RESTO"` par défaut à la création de toute commande sans
> marque. Le filtre Flutter est donc correct — ce n'est pas un bug.

### 3.3 Hub de livraison — `[PARTIEL]` (fonctionnel mais pas lisible)

**Fichiers** : `delivery_page.dart`, `delivery_orders_container.dart`

Le filtre déterminant les commandes dispatchables en tournée est **fonctionnellement
correct** : `delivery_page.dart:111-124` filtre sur
`fulfillmentType == DELIVERY_BY_RESTAURANT && orderType == DELIVERY`, sans exclusion de
marque. Une commande Uber Eats BYOC apparaît donc bien dans le pool et peut être ajoutée à
une tournée maison comme une commande normale — **c'est le comportement voulu et il est
correct**. En revanche, `DeliveryOrdersContainer` n'affiche **aucun logo/badge de marque**
sur les cartes commande — seul un badge de statut coloré (dérivé indirectement de
`brandStatus`, absent pour les commandes maison) donne un indice implicite, pas fiable pour
un dispatcher qui doit distinguer d'un coup d'œil.

### 3.4 Interface mobile livreur — `[ABSENT]`

**Fichiers** : `delivery_driver_page.dart`, `driver_session_in_progress_sheet.dart`,
`driver_current_stop_overlay.dart`, `customer_details_card.dart`

Aucune référence à `order.brand` dans la liste des arrêts, le panneau de l'arrêt en cours,
ou la carte client. **Le livreur n'a aucune indication visuelle qu'un arrêt donné est une
commande Uber Eats** — problématique si une procédure spécifique (preuve de remise, code,
photo) est exigée par Uber pour le BYOC.

### 3.5 Gestion des annulations côté UI — `[ABSENT]`

Aucun toast/alerte dédié quand `brandStatus` passe à `CANCELED`/`DENIED` pendant la
préparation. Une commande annulée côté Uber Eats est mise à jour silencieusement via le
flux websocket générique, sans alerte proactive au personnel.

### 3.6 Tests

Aucun dossier `test/` dans le repo Flutter — aucune couverture automatisée.

---

## 4. Questions restées ouvertes (à trancher avec l'équipe / valider en réel)

1. Quelle est la valeur réelle envoyée par Uber Eats dans `order.type` pour une commande
   BYOC ? Le mapping suppose `"DELIVERY_BY_RESTAURANT"` sans confirmation par fixture/doc.
2. `UberEatsBYOCStatusUpdate` doit-elle recevoir l'`order_id` interne ou le `brand_order_id`
   Uber ? Ambiguïté documentée dans le code source lui-même.
3. Quelles valeurs de statut Uber attend-il précisément sur `/restaurantdelivery/status` ?
4. Existe-t-il un besoin métier réel de signaler visuellement le BYOC au personnel de
   cuisine/livreur (procédure différente), ou le statu quo est-il acceptable côté produit ?
5. Le POS Flutter doit-il notifier explicitement Uber Eats via des appels dédiés, ou est-ce
   entièrement délégué au backend (comportement actuel) ?

---

## 5. Tableau des travaux à effectuer

| # | Objectif | Raison | Priorité | Criticité |
|---|---|---|---|---|
| 1 | Corriger `VerifySignature` (retirer le `return true` mort, calculer et vérifier le HMAC, configurer un vrai secret webhook côté env/config, faire échouer la requête si invalide) | Le webhook Uber Eats accepte aujourd'hui n'importe quel event forgé sans authentification — création/annulation/clôture de commandes falsifiables par un tiers | **Urgente** | **Critique — sécurité** |
| 2 | Brancher `UberEatsBYOCStatusUpdate` sur `StartDelivery` (départ livreur) et `DeliverOrder` (livraison confirmée) dans `order_life_cycle/service.go`, ainsi que sur les transitions `delivery_sessions` (select/arrived/delivered/failed/canceled) | Uber Eats n'est aujourd'hui jamais informé qu'une commande BYOC a été prise en charge/livrée par le restaurant — risque de non-conformité SLA/facturation/litige Uber | **Haute** | **Critique — fonctionnel** |
| 3 | Clarifier auprès de la doc API Uber Eats : (a) l'identifiant attendu par `/restaurantdelivery/status` (order_id vs brand_order_id), (b) les valeurs de statut acceptées, (c) la valeur réelle de `order.type` pour du BYOC | Prérequis bloquant pour fiabiliser le point #2 — le code actuel contient des suppositions non vérifiées écrites par l'auteur lui-même | **Haute** | Élevée (bloque #2) |
| 4 | Ajouter un signal visuel croisant `brand` + `fulfillmentType` (ex. petit badge "Livraison maison" superposé au logo Uber Eats) sur : badge commandes en cours, en-tête écran production, cartes du hub de livraison, tuiles/overlay de l'interface livreur mobile | Demande explicite du brief : le personnel doit distinguer d'un coup d'œil une commande livrée par l'établissement d'une commande Uber Eats via coursier Uber — actuellement absent des 4 écrans audités | **Haute** | Élevée — expérience/fiabilité opérationnelle |
| 5 | Ajouter une réconciliation d'état après échec d'`AcceptOrder` (symétrique à ce qui existe déjà pour Deny/Cancel/SetOrderReady) | Si l'appel Uber d'acceptation échoue, rien ne resynchronise Uber — risque de divergence d'état persistante | Moyenne | Moyenne |
| 6 | Ajouter une garde d'état dans `HandleOrderCanceled` (webhook `orders.cancel`) pour ignorer/logguer si la commande est déjà `CLOSED`/livrée | Une annulation Uber reçue en retard (race condition) peut aujourd'hui écraser une commande déjà livrée et payée | Moyenne | Moyenne |
| 7 | Corriger `CreateAttributeFromUberGroup` pour renseigner `product_id` (ou revoir le schéma) | L'import d'une commande Uber Eats contenant un modifier group jamais vu échoue systématiquement aujourd'hui (bug confirmé par test existant) | Moyenne | Moyenne — bloque certaines commandes entrantes |
| 8 | Ajouter une constante `FulfillmentType` dédiée + validation explicite pour les commandes Uber Eats, au lieu de propager la valeur brute d'Uber | Fiabilise le typage et évite une dépendance silencieuse à une coïncidence de valeur non documentée | Moyenne | Faible-Moyenne |
| 9 | Ajouter un signal UI (toast/alerte) en écran de production quand une commande passe à `brandStatus=CANCELED/DENIED` pendant la préparation | Le personnel n'est aujourd'hui averti d'une annulation Uber que par une mise à jour silencieuse de liste | Moyenne | Moyenne |
| 10 | Ajouter des tests unitaires côté backend (`internal/webhook/ubereats/service/*`) couvrant le mapping, la branche BYOC, et la garde d'annulation ; ajouter des tests widget côté Flutter sur le regroupement par marque/fulfillment | Aucune couverture automatisée aujourd'hui sur une logique métier sensible et déjà source de bugs constatés | Moyenne | Moyenne — dette qui freine les correctifs ci-dessus |
| 11 | Réparer ou supprimer le flux OAuth "Connecter mon compte Uber Eats" (colonnes DB manquantes, déjà documenté comme non fonctionnel dans le code) | Code actuellement mort/cassé qui laisse croire à une fonctionnalité opérationnelle | Basse | Faible |
| 12 | Nettoyage code mort : `GetOrderByURLUsingOrderID` (nom trompeur), interface `OrderLifeCycleService` obsolète, `CourierTripID` non lu, paramètre `merchantID` inutilisé dans `SetOrderReady` | Clarté/maintenabilité, aucun impact fonctionnel actuel | Basse | Faible |
| 13 | Afficher un logo/badge de marque explicite sur les cartes du hub de livraison (`DeliveryOrdersContainer`), aujourd'hui uniquement un badge de statut indirect | Améliore la lisibilité du dispatch sans dépendre d'une déduction implicite via `brandStatus` | Basse | Faible |

---

## 6. Journal d'implémentation

- **#5** (2026-08-19) : `SetOrderAccepted` appelle désormais `uberSvc.RecoverOrderState` si l'accept API Uber échoue, comme pour Deny/Cancel.
- **#6** (2026-08-19) : `CancelOrder` (webhook Uber `orders.cancel`) ignore désormais une commande déjà `state='CLOSED'` (guard `AND state = 'OPEN'`).
- **#7** (2026-08-19) : `product_id` (NOT NULL) est désormais fourni à l'INSERT de `CreateAttributeFromUberGroup`, threadé depuis `mapUberItemsToOrderProducts`.
- **#8** (2026-08-19) : `order_mapper.go` normalise `order.Type` Uber vers `FulfillmentTypeRestaurant` et logue un warning si la valeur reçue est inattendue.
- **#12** (2026-08-19) : suppression de `GetOrderByURLUsingOrderID`, de l'interface `OrderLifeCycleService` morte, du champ `systemToken` inutilisé et de `CourierTripID` non lu ; paramètre `merchantID` de `SetOrderReady` renommé `_`.
- **#2** (2026-08-19) : `StartDelivery`/`DeliverOrder`/`SelectDeliveryStop`/`MarkDeliveryStopArrived`/arrivée géofence notifient désormais Uber (`started`/`arriving`/`delivered`) ; position du livreur partagée à chaque update GPS (throttle Redis 8s). Hypothèses `order_workflow_uuid`/`restaurant_uuid` non validées en sandbox Uber — à vérifier avant prod réelle.
- **#4 + #13** (2026-08-19, wello_resto_flutter) : badge "livraison maison" (icône home) ajouté sur les 4 écrans POS quand marque Uber Eats/Deliveroo + fulfillment `DELIVERY_BY_RESTAURANT` ; logo de marque ajouté au hub de livraison (absent avant).
- **#9** (2026-08-19, wello_resto_flutter) : carte rouge + texte "Annulée par Uber Eats/Deliveroo" sur écran commandes en cours et écran production quand `brandStatus=CANCELED` pour une marque externe. Vérifié : le filtre de la liste production ne dépend pas de `brandStatus`, la commande reste visible.
- **#11** (2026-08-19) : `EnableIntegration`/`DisableIntegration` utilisent désormais les vraies colonnes (`bearer_token`/`refresh_token`/`enabled`) au lieu de colonnes inexistantes. Build OK ; non testé contre une base réelle (accès staging indisponible dans cet environnement) — à vérifier avant mise en prod.
- **#10** (2026-08-19) : tests ajoutés — Go : régression ID `UberEatsBYOCStatusUpdate` (sqlmock+httptest) + 2 tests Postgres (`product_id`, garde annulation) tagués `postgres_integration` (non exécutables ici, pas de DB). Flutter : 8 premiers tests widget du repo (badge maison, alerte annulation), tous verts.

---

## 7. Sources et documents liés

- `docs/order-lifecycle.md` — cycle de vie détaillé WELLO_RESTO (référence pour les statuts
  internes `state`/`brand_status`/`merchant_approval`), exclut explicitement Uber Eats.
- `docs/DELIVERY_API_AUDIT.md` — audit des tournées `delivery_session`/`delivery_session_order`
  et des besoins pour une future appli livreur dédiée.

# Temps réel WebSocket — menu, HACCP, statut ouvert/fermé + indicateur HACCP header

Date : 2026-08-24
Statut : plan de développement (aucun code écrit)
Périmètre : `ib-welloresto-api`, `wello_resto_flutter` (POS), `wello-kiosk` (bénéficiaire indirect)

## 1. État des lieux vérifié

### API — l'infrastructure existe déjà

| Brique | État |
|---|---|
| `Hub` WebSocket ([internal/infrastructure/websocket/hub.go](../../internal/infrastructure/websocket/hub.go)) | En place, scoping par `merchantID`, thread-safe |
| Points d'entrée | `/ws` (auth humaine) et `/ws-kiosk` (auth device), **même Hub** |
| Producteurs actuels | `NotificationService.SendNotificationAsync` (WS + FCM : commandes, réservations) et `BroadcastToMerchant` brut (`kiosk_status_changed`) |
| `menu_updated`, `availability_update`, `device_command` | **Aucun producteur côté API** |

Point important : [docs/KIOSK_DECISIONS.md](../KIOSK_DECISIONS.md) (incrément 7) affirme que le
serveur « diffusait bien ces events ». C'est factuellement faux — `menu_updated` n'apparaît nulle
part ailleurs que dans un commentaire. En revanche **la borne les consomme déjà**
(`wello-kiosk/lib/data/services/websocket_service.dart:181`) : le contrat client existe, il ne
manque que l'émetteur. Le doc kiosk est à corriger.

### Points de passage uniques déjà en place

Les mutations de menu convergent déjà vers deux endroits :

- `MenuRepository.setMenuUpdated` — **44 appels**, couche repository, met à jour
  `merchant_parameters.last_menu_update` (déjà exploité par `GET /menu?last_menu_update=`)
- `MenuService.invalidateMenuCache` — **42 appels**, couche service, purge les caches Redis
  scannorder/kiosk, + `import_commit_service.go:105`

C'est ce qui rend le chantier menu peu coûteux : un seul point d'émission à créer.

Statut ouvert/fermé : `PATCH /pos/status` → `POSRepository.UpdatePOSStatus`
(`merchant_parameters.is_open`). `GET /pos/status` renvoie un statut **composé** (is_open +
horaires + jours fériés + congés), pas le flag brut.

HACCP : `GET /haccp/hub` (`Service.GetHub`) renvoie déjà `cleaning.completed_count` /
`total_count`. Manquent les deux compteurs demandés (voir §4, lot 2). Écritures HACCP :
`temperature-readings/batch`, `cleaning-sessions`, `traceability`, `goods-receipts`.
`haccp.Service` n'a **pas** de broadcaster injecté aujourd'hui.

### POS Flutter

| Brique | État |
|---|---|
| `WebSocketService` | Fonctionnel (ping/pong, backoff), mais **parse chaque trame en `PushNotificationDataResponse` à 3 champs** (`merchant_id`, `entity_id`, `type`) — tout champ supplémentaire est perdu |
| Dispatch | `switch` unique dans `PushNotificationController._handleData` |
| Header | `CustomAppBar` → `BookingsHeaderSummary`, gaté sur `capabilities.modules.bookings` — patron exact à reproduire |
| HACCP | `HaccpController.initializeHub()` / `refreshHub()` **existent déjà**, controller fourni au niveau app (`main.dart:345`), mais le hub n'est chargé qu'à l'ouverture de `haccp_hub_screen` |
| Ouvert/fermé | `OpeningEtablishementInfo` observe `AuthSession.merchant.isOpen` ; `AuthSession.setIsOpen()` existe déjà |
| Menu | `WelloRestoMenuController.loadMenu()`, aucun polling |

## 2. Faisabilité

**Faisable, sans migration SQL, sans changement d'architecture.** L'essentiel du travail est côté
Flutter POS, pas côté Go.

Le seul vrai obstacle technique est l'enveloppe WS du POS, aujourd'hui réduite à 3 champs. Deux
sorties possibles :

- **(A, retenue)** les events restent des *notifications sans état* : le client refetch
  (`GET /menu`, `GET /haccp/hub`, `GET /pos/status`). Cohérent avec le patron existant
  commandes/réservations (fetch-by-id après event) et avec la demande (« utiliser le même endpoint
  que le hub… relancé à chaque réception de ws »).
- (B) enveloppe riche transportant l'état — plus de code, plus de risques de divergence.

## 3. Contrat d'événements proposé

| Type | Payload | Réaction POS | Réaction borne |
|---|---|---|---|
| `menu_updated` | `merchant_id`, `last_menu_update` (unix) | `loadMenu()` | déjà câblé |
| `haccp_updated` | `merchant_id`, `entity_id`, `haccp_kind` (`temperature` / `cleaning` / `traceability` / `goods_receipt`), `date` | `refreshHub()` | ignoré |
| `pos_status_changed` | `merchant_id`, `is_open` (bool brut) | `AuthSession.setIsOpen()` | ignoré |

Nommage `snake_case` : la famille SCREAMING_SNAKE (`NEW_ORDER`, `UPDATE_BOOKING`) est réservée aux
types qui ont aussi un pendant FCM. Ces trois-là sont WS-only.

## 4. Lots de développement

### Lot 0 — Socle API (0,5 j)

Constantes `WSEvent*` dans `notification_models.go`. Petite interface `RealtimeBroadcaster`
(`BroadcastToMerchant(merchantID string, payload map[string]any) bool`) que `NotificationService`
satisfait déjà, pour que `menu` / `haccp` / `pos` en dépendent sans importer `notification`.
Câblage dans `routes.go`.

### Lot 1 — `menu_updated` (1 à 1,5 j)

- Émission au **niveau service**, en étendant `invalidateMenuCache` en
  `onMenuChanged(ctx, merchantID)` = purge Redis + broadcast. Les 42 sites appellent déjà cette
  fonction : une seule édition.
- **Pas** au niveau repo `setMenuUpdated` : il tourne à l'intérieur de transactions
  (`repository.go:2357` utilise `txCtx`) — diffuser avant commit ferait refetch le client sur un
  état pas encore visible.
- Audit des écarts : les mutations qui touchent `setMenuUpdated` **sans** passer par
  `invalidateMenuCache` (display-order, sync allergènes, tâche safety-stock, sync menu
  Deliveroo/Uber) doivent être ramenées sur le même helper.
- **Coalescing obligatoire** : import de catalogue, bulk status, réordonnancement émettent des
  dizaines d'events. Sans amortissement (fenêtre glissante ~1 s par merchant, ou timer trailing
  500 ms), chaque borne et chaque POS refetch le menu complet en rafale. C'est la seule pièce non
  triviale du lot.

### Lot 2 — `haccp_updated` + compteurs hub (1 j)

- Injection du broadcaster dans `haccp.Service`, émission dans les 4 écritures.
- Extension de `GetHub` (ajout de champs, aucun champ existant modifié) :
  - `temperatures.completed_count` — nombre de relevés du jour (nouvelle requête count)
  - `ingredients_labeling.completed_count` — nombre de traçabilités du jour. **Nouvelle requête
    nécessaire** : `HasTraceabilityRecords` est un booléen *toutes dates confondues* et
    `ListTraceabilityRecords` n'a pas de filtre date.
  - nettoyage : `completed_count` / `total_count` déjà présents → « 2/5 » directement exploitable.

### Lot 3 — `pos_status_changed` (0,25 j)

Émission dans `POSService.UpdatePOSStatus` après succès. Le flag brut est diffusé, pas le statut
composé (voir décision D3).

### Lot 4 — POS Flutter, plomberie WS (0,5 à 1 j)

Élargir l'enveloppe (ajout d'un `Map<String, dynamic> raw` à `PushNotificationDataResponse` :
rayon d'impact minimal, un seul point de dispatch conservé), puis 3 `case` dans
`PushNotificationController._handleData`. Injection de `WelloRestoMenuController`,
`HaccpController` et `AuthSession` dans le controller (construit dans `main.dart`).

### Lot 5 — POS Flutter, indicateur HACCP header (1 à 1,5 j)

- `HaccpHeaderSummary`, gaté `capabilities.modules.haccp` dans `CustomAppBar`, calqué sur
  `BookingsHeaderSummary`.
- Trois pastilles : température + nombre (rouge si 0) · nettoyage + « 2/5 » · traçabilité + nombre
  (pas de rouge en v1).
- Source : `HaccpController.hub`, déjà `ChangeNotifier` et déjà fourni au niveau app. Appeler
  `initializeHub()` depuis le flux post-login au lieu du seul `haccp_hub_screen`, puis rafraîchir
  sur chaque `haccp_updated`. Pas de polling (éventuellement un filet à 15 min, comme le timer
  5 min des réservations).
- **C'est ici que part le temps de design** : le header fait `kToolbarHeight` et contient déjà
  bouton menu + résumé résas (qui occupe l'`Expanded`) + verrou + actions rapides. Deux résumés
  simultanés imposent un arbitrage de mise en page (breakpoint responsive ou pastilles compactes
  icône + nombre).

### Lot 6 — Vérification borne (0,25 j)

La borne devient temps réel « gratuitement » sur `menu_updated`. Vérifier le chemin de refetch de
son `MenuController` et l'effet du coalescing.
ScanNOrder web : pas client WS, reste sur l'invalidation Redis. Aucun changement.

### Lot 7 — Tests et documentation (0,5 à 1 j)

Tests Go sur les 3 émetteurs (hub factice) et sur le coalescing ; mise à jour de
`docs/decisions.md` ; correction de l'affirmation erronée de `KIOSK_DECISIONS.md`.

## 5. Chiffrage

API ≈ 3 j · POS Flutter ≈ 3 j · tests/doc ≈ 1 j → **6 à 7 jours**, livrables en 3 incréments
déployables indépendamment :

1. **A** — socle + `pos_status_changed` : le plus court, valide la chaîne de bout en bout
2. **B** — `menu_updated` + coalescing : bénéfice immédiat borne + POS
3. **C** — `haccp_updated` + compteurs hub + indicateur header

## 6. Décisions à acter

- **D1 — Menu actif uniquement.** Les travaux futurs permettront plusieurs menus dont un seul
  actif. `menu_updated` ne devra alors être émis **que si le menu modifié est le menu actif** : le
  point d'émission unique `onMenuChanged` devra recevoir l'id du menu touché et le comparer au menu
  actif du merchant. Sans ce garde-fou, éditer un menu brouillon ferait recharger le menu en service
  sur toutes les bornes. À rappeler en commentaire sur `onMenuChanged` **et** dans
  `docs/decisions.md`.
- **D2** — Les events sont des notifications sans état ; le client refetch. Aucun état métier ne
  transite par le WebSocket.
- **D3** — `pos_status_changed` transporte le flag brut `is_open`, pas le statut composé
  (horaires / fériés / congés / `closed_until`). La v1 ne couvre donc **pas** les bascules pilotées
  par le calendrier — un client qui affiche le statut composé continue de rafraîchir via
  `GET /pos/status`.
- **D4** — Nommage `snake_case` pour les events WS-only.
- **D5** — Coalescing par merchant obligatoire sur `menu_updated`.

## 7. Risques

- **Hub en mémoire ⇒ mono-instance.** Les events ne traversent pas les répliques. Déjà vrai
  aujourd'hui pour les commandes, mais à traiter (Redis pub/sub) avant tout scale-out.
- **`BroadcastToMerchant` désinscrit silencieusement** un client dont le buffer (256) est plein.
  Une rafale non amortie peut donc déconnecter les POS — cf. D5.
- **Rechargement du menu en pleine prise de commande** : `loadMenu()` remplace `_menu`. Vérifier
  que le panier en cours n'est pas impacté avant de brancher `menu_updated` sur le POS.
- **Charge du hub HACCP** : `GetHub` fait plusieurs requêtes (dernière session, surfaces,
  exécutions, traçabilité). Un rafraîchissement par event est acceptable au rythme réel des saisies
  HACCP, mais ne doit pas être branché sur un event à haute fréquence.

## 8. Vérifications complémentaires (2026-08-24) et arbitrages validés

### 8.1 Points vérifiés dans le code

**Panier POS non impacté par un rechargement de menu.** `OrderController` ne référence
`WelloRestoMenuController` que pour la tarification au changement de type de commande
(`order_controller.dart:584`) ; les lignes de panier sont des snapshots indépendants. Un
`loadMenu()` ne détruit donc pas une commande en cours. **En revanche deux effets de bord réels :**

1. `menu_list_view.dart:113` rend `menuController.isLoading ? LoaderIndicator() : …` **sans garde** :
   un rechargement WS viderait la grille produits et afficherait un loader en pleine saisie.
2. `updateMenuProductsPrice(orderType)` n'est appelé que par `OrderController.changeOrderType`.
   Après un `loadMenu()` frais, les prix retombent au tarif de base. Aujourd'hui c'est masqué
   (`loadMenu()` ne tourne que si `menu == null`, donc avant tout choix de mode) ; un rechargement
   WS pendant une commande À EMPORTER **réinitialiserait silencieusement les prix en SUR PLACE**.

**Écarts de purge de cache : exactement 4 méthodes.** Audit exhaustif des 41 méthodes repo qui
appellent `setMenuUpdated`, croisé avec les méthodes de `MenuService` : seules
`CreateComponent`, `UpdateComponent`, `DeleteComponent` et `SetComponentStatus` mutent le menu
sans appeler `invalidateMenuCache`. Toutes les autres mutations passent bien par le service.
Aucun autre module ne mute le menu (`upsell` n'utilise `MenuRepository` qu'en lecture).

**Même bug de tarification côté borne.** `wello-kiosk/menu_controller.dart:21` fait
`menuUpdated.listen((_) => loadMenu())` — **sans `fulfillmentType`**, alors que la docstring de
`loadMenu` indique explicitement qu'il doit être renvoyé à chaque fois. Activer `menu_updated`
ferait donc repasser en prix SUR PLACE un client borne en À EMPORTER. Le mode courant vit dans
`CartController._fulfillmentType`, que `MenuController` ne connaît pas.
Bonne nouvelle : `menu_screen.dart:81` garde son loader par `&& categories.isEmpty` — pas d'effet
loader sur la borne, seule la tarification est à corriger.

**Ordre des providers POS.** `createAuthenticationController` (`main.dart:336`) construit
`PushNotificationController`, mais `HaccpController` n'est déclaré qu'en `main.dart:344` — donc
**après**. Le provider HACCP doit remonter avant la ligne 336 ; sa seule dépendance est
`HaccpApiService` (ligne 191), le déplacement est sans risque.

**Tables HACCP pour les compteurs.** `temperature_readings` (attention : pas de préfixe `haccp_`)
et `haccp_traceability_records`, toutes deux avec `created_at` + `enabled`. `GetHub` calcule déjà
la fenêtre `startAt`/`endAt` du jour : deux `COUNT(*)` à ajouter, rien de plus.

**Back-office : pas client WebSocket.** Aucune connexion `/ws` dans `wello-back-office`. C'est
cohérent : le back-office est l'émetteur des modifications de menu, pas un récepteur.

**Header : pas de gate `isPhone` sur la tile résas.** `CustomAppBar` ne fait aucun test de largeur.
Le seul masquage mobile est `navigation_page.dart:81` : la barre **entière** disparaît sur
téléphone quand une prise de commande est active sur l'onglet 0. Hors de ce cas, la tile résas
s'affiche aussi sur téléphone.

### 8.2 Arbitrages validés

- **A1 — Header** : pastilles HACCP compactes (icône + nombre, sans libellé) en largeur fixe, à
  droite du résumé résas qui garde son `Expanded`, avant le verrou. Tile **masquée sur téléphone**
  (`isPhone`), visible tablette uniquement. *Hypothèse à confirmer : la tile résas n'ayant pas ce
  gate aujourd'hui, faut-il l'aligner sur le même comportement ?*
- **A2 — Correctifs adjacents inclus dans le chantier** : purge de cache des 4 mutations de
  composants **et** tarification borne au rechargement WS.
- **A3 — POS** : rechargement silencieux immédiat (`loadMenu(silent: true)` ne touchant pas
  `_isLoading`) suivi de la réapplication du tarif du mode de commande courant.

### 8.3 Impact sur le chiffrage

Lot 1 (+0,25 j) : les 4 méthodes composants à router vers `onMenuChanged`.
Lot 4 (+0,5 j) : variante silencieuse de `loadMenu` + réapplication tarifaire + remontée du
provider HACCP.
Lot 6 (+0,5 j) : mémoïsation du `fulfillmentType` dans le `MenuController` de la borne
(`_lastFulfillmentType`, alimenté par `loadMenu`/`loadAll`, réutilisé au rechargement WS) — évite
de coupler `MenuController` à `CartController`.

**Total révisé : 7 à 8 jours.**

## 9. Incrément A — livré (2026-08-24)

### 9.1 Écart assumé par rapport au plan : pas d'interface `RealtimeBroadcaster` partagée

Le lot 0 prévoyait une interface partagée. À la relecture, `kiosk/service.go` établit déjà un
précédent : le consommateur importe `notification`, garde un `*notification.NotificationService`
nil-gardé et utilise les constantes `notification.WSEvent*`. Aucun cycle d'import n'est possible
(`notification` ne dépend que de `websocket`, `logger`, `models`, `helpers`, `dbx`).

Décision retenue : **constantes centralisées dans `notification`** (source unique du contrat WS),
et **interface déclarée côté consommateur** (`pos.realtimeBroadcaster`) pour garder un point de
test sans hub réel. On garde la cohérence avec l'existant tout en gagnant la testabilité qui
manquait au patron kiosk.

### 9.2 Modifications

| Fichier | Nature |
|---|---|
| `internal/modules/notification/notification_models.go` | Ajout des 3 constantes WS (`pos_status_changed`, `menu_updated`, `haccp_updated`) et de la doc du contrat |
| `internal/modules/pos/service.go` | Interface `realtimeBroadcaster`, champ + paramètre de `NewPOSService`, méthode `broadcastPOSStatus`, émission dans `UpdatePOSStatus` |
| `internal/modules/pos/service_test.go` | **Nouveau** — contrat de payload, nil-safety, absence de device connecté |
| `cmd/api/routes.go` | Injection de `notificationService` dans `NewPOSService` |
| `…/pushnotification_data_response.dart` | Champ `raw` + helper `boolFromRaw` (bool WS vs String FCM) |
| `…/pushnotification_controller.dart` | Dépendance `AuthSession`, case `pos_status_changed`, `_handlePosStatusChanged` |
| `…/main.dart` | Passage de `AuthSession` au `PushNotificationController` |
| `test/data/api/responses/pushnotification_data_response_test.dart` | **Nouveau** — parsing des deux canaux |

`menu_updated` et `haccp_updated` sont déclarés mais **pas encore émis** (incréments B et C).

### 9.3 Choix d'implémentation

- **Émission après l'écriture, pas après la relecture.** `UpdatePOSStatus` diffuse dès que
  `posRepo.UpdatePOSStatus` a réussi, avant le `GetPOSStatus` final : si la relecture du statut
  composé échoue, l'événement part quand même — la bascule, elle, a bien eu lieu.
- **`is_open` reste un bool dans le payload.** Le canal FCM ne transporte que des String ; le
  helper `boolFromRaw` tolère les deux formes pour que le DTO reste utilisable sur les deux canaux.
- **`null` distinct de `false`.** Un `is_open` inexploitable fait ignorer l'événement plutôt que de
  fermer l'établissement par erreur.
- **Écho au device émetteur non filtré.** Celui qui bascule le switch reçoit son propre événement ;
  `AuthenticationController.updatePointOfSaleAvailability` a déjà appliqué la valeur en optimiste,
  la réappliquer est idempotent. `MerchantDto.copyWith` utilisant `?? this.x` partout, aucun autre
  champ du merchant n'est écrasé au passage (vérifié).

### 9.4 Validation

- `go build ./...` : OK
- `go test ./internal/modules/pos/...` : OK (3 tests, 4 sous-cas)
- `flutter test test/` : OK (15 tests, dont 7 nouveaux)
- `dart analyze` sur les fichiers touchés : 2 `info` **pré-existants**
  (`avoid_print` ligne 57, `curly_braces` ligne 100), aucun introduit.

Deux échecs de `go vet -tags postgres_integration` subsistent hors périmètre et sont
**pré-existants** (vérifié en stashant les modifications) : `pos/accounting` (`NewAccountingService`
a gagné un paramètre) et `cmd/api` (copie de lock sur `NewAuthHandler`).

### 9.5 Reste à faire sur cet axe

L'alignement des tiles résas et HACCP sur un comportement responsive commun est rattaché à
l'incrément C (c'est du header), conformément à l'arbitrage A1.

## 10. Incrément B — livré (2026-08-24)

### 10.1 `MenuChangeNotifier` : le point de passage unique

Nouveau fichier [internal/modules/menu/menu_change_notifier.go](../../internal/modules/menu/menu_change_notifier.go).
`MenuService.invalidateMenuCache` a été **renommé `onMenuChanged`** (42 sites) : garder l'ancien
nom aurait laissé une fonction qui ment sur ce qu'elle fait, puisqu'elle diffuse désormais aussi.

Instance **unique et partagée** entre `MenuService` et `ImportService`, construite dans
`routes.go` : deux notifiers auraient eu deux fenêtres d'amortissement indépendantes, et un import
lancé pendant des éditions unitaires aurait diffusé en double.

### 10.2 Amortissement : front montant + front descendant

Un simple cooldown aurait été un piège : les clients rechargent au *début* de la rafale, pendant
que les écritures continuent, et ne sont jamais prévenus de la fin — ils restent donc sur un menu
périmé, exactement ce que la fonctionnalité doit éviter. L'implémentation diffuse donc **au premier
changement** (cas courant : un produit passé en rupture, latence nulle) **et une fois en fin de
fenêtre** si d'autres changements ont suivi. Plafond : un événement par merchant et par seconde.

**La purge de cache, elle, n'est pas amortie** — la retarder laisserait une vitrine servir un menu
périmé. Seule la diffusion l'est.

Pas de `last_menu_update` dans le payload : notification sans état (D2). L'y joindre aurait coûté
une lecture en base sur un chemin amorti, pour une valeur de toute façon approximative à la
réception. Vérifié au passage : le POS n'envoie jamais `last_menu_update` sur `GET /menu`, donc
aucun risque de recevoir `no_update_required` (qui produirait un menu vide).

### 10.3 Correctifs adjacents (arbitrage A2)

**Cache des composants.** `CreateComponent`, `UpdateComponent`, `DeleteComponent` et
`SetComponentStatus` passent désormais par `onMenuChanged`. Bug de cache pré-existant corrigé au
passage : un ingrédient mis en rupture laissait le menu borne/ScanNOrder périmé jusqu'au TTL.
`SetComponentStatus` ne diffuse que si `affected > 0` — inutile de réveiller toutes les bornes pour
un `UPDATE` qui n'a touché aucune ligne.

**Tarification borne.** `wello-kiosk/menu_controller.dart` mémorise le dernier `fulfillmentType`
(`_lastFulfillmentType`, alimenté par `loadMenu`) et le rejoue au rechargement WS. Mémoïsation
plutôt qu'une dépendance vers `CartController` (seul détenteur du mode) : ce contrôleur n'en
connaît aucun autre, et chaque changement de mode passe déjà par `loadMenu` — la valeur est donc
synchrone par construction. Un retour à l'écran d'accueil appelle `loadAll()` sans argument et la
remet à `null`, ce qui est le comportement voulu.

### 10.4 POS : rechargement silencieux (arbitrage A3)

Nouvelle méthode `WelloRestoMenuController.refreshSilently()`, distincte de `loadMenu()` :

- **ne touche pas `isLoading`** — `menu_list_view.dart:113` rend un loader plein écran dessus ;
- **ne remplace pas le menu courant si le fetch a échoué.** Découvert en implémentant :
  `MenuService.fetchMenu` **avale l'exception réseau et renvoie un menu vide** au `status` vide
  (l'API répond `"ok"`). Sans ce filtre, une coupure passagère aurait vidé le menu du POS en plein
  service — une régression franche introduite par le temps réel. Les tags suivent le sort du menu :
  s'il est arrivé, le réseau est debout et une liste vide est une vraie liste vide.

`PushNotificationController._handleMenuUpdated` réapplique ensuite
`updateMenuProductsPrice(cartController.selectedOrderType)`. Ce n'est pas cosmétique : le menu
revient avec les prix de base et cette méthode n'est autrement appelée que par
`OrderController.changeOrderType`. L'opération est idempotente (elle lit `priceTakeAway` /
`priceDelivery`, jamais le `price` déjà calculé).

### 10.5 Modifications

| Fichier | Nature |
|---|---|
| `internal/modules/menu/menu_change_notifier.go` | **Nouveau** — purge + diffusion amortie |
| `internal/modules/menu/menu_change_notifier_test.go` | **Nouveau** — 9 tests (horloge simulée) |
| `internal/modules/menu/service.go` | `invalidateMenuCache` → `onMenuChanged`, champ `changes`, 4 mutations de composants raccordées, TODO D1 |
| `internal/modules/menu/import_service.go` | `InvalidateMerchantMenuCaches` retiré de `importPreviewStore`, champ + paramètre `changes` |
| `internal/modules/menu/import_commit_service.go` | Passe par `changes.Changed` |
| `internal/modules/menu/import_handler_test.go`, `…_postgres_integration_test.go` | Fakes et constructeurs alignés |
| `cmd/api/routes.go` | `menuChanges` construit une fois, injecté dans les deux services |
| `…/welloresto_menu_controller.dart` | `refreshSilently()` |
| `…/pushnotification_controller.dart` | Dépendance `WelloRestoMenuController`, case `menu_updated` |
| `…/main.dart` | Passage du `WelloRestoMenuController` |
| `test/controllers/welloresto_menu_controller_refresh_test.dart` | **Nouveau** — 4 tests |
| `wello-kiosk/…/menu_controller.dart` | `_lastFulfillmentType` rejoué au rechargement WS |

### 10.6 Validation

- `go build ./...` : OK
- `go test ./internal/modules/menu/...` : OK
- `flutter test test/` (POS) : OK, 19 tests dont 4 nouveaux
- `dart analyze` : borne **aucun problème** ; POS, les 2 `info` pré-existants déjà signalés
- 3 échecs dans `internal/modules/planning/swaps` : **pré-existants**, vérifié en stashant les
  modifications. Hors périmètre.

**Non vérifié :** le race detector n'a pas pu tourner (`-race` exige cgo, pas de gcc sur la machine)
— l'amortissement est protégé par mutex et couvert par un test de concurrence à 100 goroutines,
mais sans validation par le détecteur. Le trajet réel bout-en-bout (back-office qui édite → borne
et POS qui rechargent) n'a pas non plus été joué.

## 11. Reprise de session (2026-09-11) — vérification d'intégrité avant l'incrément C

Entre la fin de l'incrément B et le début de l'incrément C, une nouvelle session a démarré sur un
contexte compacté, ~18 jours plus tard, avec entretemps un système RBAC complet livré
(`55c4b9d feat(rbac): role-based permissions system (lots 1-9)`) et un chantier onboarding en cours
(branche `staging`, nombreux fichiers non liés modifiés). Le `git status` initial ne mentionnait ni
`internal/modules/menu/`, ni `internal/modules/pos/service.go`, ni
`internal/modules/notification/` — aucun des fichiers des incréments A et B — ce qui pouvait signifier
soit qu'ils avaient été commités, soit qu'ils avaient été perdus.

Vérification faite avant de reprendre : les trois fichiers existaient sur disque avec le contenu
attendu (`broadcastPOSStatus`, `menu_change_notifier.go`, les 3 constantes `WSEvent*`), et
`git log` a confirmé qu'ils étaient commités (`37a5225 update: analytics and stuff` pour la base,
`6cc81f5`/`55c4b9d` ensuite pour `pos/service.go` via le chantier RBAC). `go build ./...` et
`go test ./internal/modules/menu/... ./internal/modules/pos/...` étaient au vert. Côté Flutter
(POS et kiosk, dépôts git distincts), même constat : commités (`b0c1433 updates` pour le POS,
`5cacd63 feature: kiosk`), wiring intact dans `main.dart`. Aucune perte de travail — reprise sur
base saine confirmée avant d'écrire la moindre ligne de l'incrément C.

## 12. Incrément C — livré (2026-09-11)

### 12.1 Compteurs du hub HACCP

Deux nouvelles requêtes dans `haccp/repository.go`, sur le patron exact de `ListTemperatureReadings`
(fenêtre `[startAt, endAt)` déjà calculée par `GetHub`) :

- `CountTemperatureReadingsInRange` — `COUNT(*)` sur `temperature_readings`
- `CountTraceabilityRecordsInRange` — `COUNT(*)` sur `haccp_traceability_records`, **distincte**
  de `HasTraceabilityRecords` qui est un booléen toutes dates confondues et ne convenait pas au
  compteur du header.

Côté modèle : `HubTemperatures` gagne `CompletedCount` (ajout pur). `IngredientsLabeling` change de
type — `HubPlaceholder` (utilisé par `Reception`, qui reste `{enabled}` seul) ne suffisait plus une
fois qu'un compteur devait s'y ajouter ; nouveau type `HubIngredientsLabeling{Enabled, CompletedCount}`
propre à ce champ, `Reception` intact. Aucun champ existant modifié, conforme au plan initial.

### 12.2 Diffusion `haccp_updated`

Même patron que `pos.realtimeBroadcaster` et `menu.realtimeBroadcaster` : interface locale au
module, `NewService` prend un `broadcaster` optionnel (nil-safe), `broadcastHACCPUpdated` envoie
`{type, merchant_id}` — notification sans état (D2), aucune donnée métier au-delà de ce couple
(vérifié par test : `len(payload) != 2` échoue si jamais un champ de plus s'y glisse).

Branché sur les 4 écritures, chacune **après** le succès (hors transaction, même émplacement que
l'audit déjà en place sur les deux premières) :
- `CreateTemperatureReadingsBatch` — après `auditService.LogChange`
- `CreateCleaningSession` — après `auditService.LogChange`
- `CreateGoodsReceipt` — après le retour du repo (pas d'audit existant sur ce chemin)
- `CreateTraceabilityRecord` — après la résolution des URLs de photos

Câblage dans `routes.go` : `haccpService := haccpModule.NewService(haccpRepo, auditService,
selectedDB, r2Client, notificationService)`.

### 12.3 POS Flutter — consommation et header

`HaccpController` (déjà existant) gagne `requestOpenHaccpHub()` / `consumeShouldOpenHaccpHub()`,
même patron « consume once » que `BookingsController.requestOpenBookingsList` — nécessaire car
`CustomAppBar` n'a pas accès à l'index d'onglet de `NavigationPage`. `NavigationPage` écoute les
deux contrôleurs de façon symétrique (`_onHaccpControllerChanged` bascule sur l'index 4, un
littéral déjà présent et stable dans `_selectContentPage`/`_canAccessIndex`, au même titre que
l'index 8 pour les résas).

`PushNotificationController` gagne le case `haccp_updated` → `haccpController.refreshHub()` (déjà
robuste aux échecs : `_hub` n'est réassigné qu'en cas de succès).

`AuthenticationController._applyLoginData` appelle désormais `HaccpController.initializeHub()`
une fois à la connexion (fire-and-forget, gaté sur `capabilities.modules.haccp`, même style que
`_fetchPrinterConfigs()`) au lieu du seul accès à `haccp_hub_screen` — c'est ce qui alimente
l'indicateur du header dès l'ouverture de l'app plutôt qu'au premier tour sur l'onglet HACCP.

**Ordre des providers corrigé** (identifié comme blocage en §8.1) : `createHaccpController` est
remonté avant `createAuthenticationController` dans `main.dart`, puisque ce dernier construit
`PushNotificationController`, qui dépend désormais de `HaccpController`.

Modèles Flutter : `HaccpTemperatureHub.completedCount` (ajout), nouveau type
`HaccpIngredientsLabelingHub{enabled, completedCount}` distinct de `HaccpSimpleFeatureHub`
(toujours utilisé par `reception`) — miroir exact de la bifurcation côté Go. Régénérés via
`dart run build_runner build` (project équipé de `json_serializable`), pas d'édition manuelle du
`.g.dart`.

### 12.4 Header — alignement des deux tiles (arbitrage utilisateur du jour)

Vérification faite avant modification : contrairement à ce qui était supposé lors de l'arbitrage
initial (§8.1), **aucune des deux tiles n'avait de garde-fou `isPhone`** — `CustomAppBar` ne faisait
aucun test de largeur, le seul masquage existant étant celui de la barre entière pendant une prise
de commande active sur téléphone (`navigation_page.dart`, sans rapport avec HACCP/résas).

Décision appliquée : **les deux tiles sont masquées sur téléphone** (`isPhone`, seuil 600px de
`ResponsiveHelper`), visibles uniquement tablette — comportement désormais identique pour les deux,
conforme à la demande explicite du jour. `HaccpHeaderSummary` (nouveau) reprend le patron visuel de
`BookingsHeaderSummary` (pilule `InkWell`, bordure bleue) : trois pastilles compactes icône+nombre
(🌡 rouge si 0, 🧽 `completed/total`, 📋 neutre), rendu vide tant que `hub == null` (pas de pastille
à 0 trompeuse avant la première réponse serveur). `CustomAppBar` la positionne en largeur fixe
avant le verrou, le résumé résas gardant l'`Expanded`.

### 12.5 Modifications

| Fichier | Nature |
|---|---|
| `internal/modules/haccp/repository.go` | **Nouveau** — 2 requêtes de comptage |
| `internal/modules/haccp/model.go` | `HubTemperatures.CompletedCount`, nouveau `HubIngredientsLabeling` |
| `internal/modules/haccp/service.go` | Compteurs branchés dans `GetHub`, `realtimeBroadcaster`, `broadcastHACCPUpdated`, 4 sites d'appel |
| `internal/modules/haccp/service_broadcast_test.go` | **Nouveau** — 3 tests |
| `cmd/api/routes.go` | `NewService` reçoit `notificationService` |
| `…/haccp_models.dart` + `.g.dart` | `completedCount`, `HaccpIngredientsLabelingHub` (régénéré) |
| `…/haccp_controller.dart` | `requestOpenHaccpHub()` / `consumeShouldOpenHaccpHub()` |
| `…/pushnotification_controller.dart` | Dépendance `HaccpController`, case `haccp_updated` |
| `…/authentication_controller.dart` | `_initializeHaccpHubIfEnabled()` sur `_applyLoginData` |
| `…/navigation_page.dart` | Écoute symétrique `HaccpController`, bascule index 4 |
| `…/main.dart` | Provider HACCP remonté, `haccpController` passé au `PushNotificationController` |
| `…/haccp_header_summary.dart` | **Nouveau** — 3 pastilles, tap → hub |
| `…/custom_appbar.dart` | Gate `haccpEnabled`, `isPhone` sur les deux tiles |

### 12.6 Validation

- `go build ./...` : OK
- `go test ./internal/modules/haccp/... ./internal/modules/menu/... ./internal/modules/pos/... ./internal/modules/notification/...` : OK
- `go test ./internal/...` : échecs uniquement dans `planning/employees`, `planning/leave`,
  `planning/swaps`, `ubereats` — confirmés **hors périmètre** (`git status --porcelain` sur ces
  répertoires : aucun fichier modifié par ce chantier)
- `flutter test test/` (POS) : OK, 19 tests
- `dart analyze` sur tous les fichiers touchés : uniquement des avertissements **pré-existants**
  (confirmés via `git show HEAD:<fichier>` sur les lignes concernées) — aucun introduit
- `flutter analyze` (kiosk, aucun changement dans cet incrément) : « No issues found »

**Non vérifié :** le trajet réel bout-en-bout (saisie HACCP sur un device → hub et header
rafraîchis sur un autre) n'a pas été joué, ni le rendu visuel du header sur tablette réelle.

## 13. Bug rapporté — tile HACCP absente (2026-09-11)

### 13.1 Diagnostic

Symptôme : la tile résas s'affiche, pas la tile HACCP. Cause trouvée par lecture de code + état git,
sans instrumentation supplémentaire nécessaire.

`lib/environment.dart` ne pointe **que** vers Render (`welloresto-api-staging`/`-prod`) — l'app
Flutter ne parle jamais à un serveur local. Or les changements Go de l'incrément C
(`internal/modules/haccp/{model,repository,service}.go`) étaient encore **non commités** en local,
et le HEAD local lui-même avait 2 commits d'avance sur `origin/staging` (`c113a46` vs `885898e`,
vérifié via `git fetch` + `git log origin/staging -- internal/modules/haccp/model.go`). L'API
réellement servie renvoie donc encore l'ancienne forme de `GET /haccp/hub` : pas de
`completed_count` sur `temperatures`, et `ingredients_labeling` toujours `{enabled}` seul.

Le code généré par `json_serializable` pour ces deux champs faisait
`(json['completed_count'] as num).toInt()` — un cast qui **lève** sur `null`. `HaccpController.
initializeHub()` capture l'exception dans son `catch`, `_hub` reste `null`, et
`HaccpHeaderSummary` — conçue pour ne jamais afficher une pastille à 0 trompeuse avant la première
réponse serveur — retourne `SizedBox.shrink()` indéfiniment. Échec totalement silencieux, aucune
trace visible, exactement le symptôme rapporté. Les résas n'appellent pas ce endpoint, d'où
l'absence d'impact.

### 13.2 Décision : parsing tolérant plutôt que déploiement immédiat

Deux options proposées, l'utilisateur a choisi le correctif côté client uniquement (pas de
commit/push vers `staging` pour l'instant — cohérent avec la consigne de ne jamais committer sans
demande explicite à ce moment précis).

`@JsonKey(defaultValue: 0)` ajouté sur les deux champs récents :
`HaccpTemperatureHub.completedCount` et `HaccpIngredientsLabelingHub.completedCount`. Régénéré via
`dart run build_runner build` : le code généré passe de `(json['completed_count'] as num).toInt()`
à `(json['completed_count'] as num?)?.toInt() ?? 0`. **`HaccpCleaningHub.completedCount` non
touché** : ce champ existait déjà côté API déployée avant l'incrément C, aucun risque de régression
dessus.

Effet : le hub se charge normalement même contre l'API actuelle, l'indicateur température affiche
`0` (rouge, comportement voulu pour « aucun relevé »), l'indicateur traçabilité affiche `0` — la
valeur réelle du jour ne remontera qu'une fois l'incrément C effectivement déployé. Ce correctif
ne remplace pas le déploiement, il évite seulement qu'un décalage de version fasse échouer tout le
hub plutôt que le seul champ concerné — utile aussi pour toute fenêtre de rollout future.

### 13.3 Modifications

| Fichier | Nature |
|---|---|
| `…/haccp_models.dart` | `@JsonKey(defaultValue: 0)` sur les 2 champs récents |
| `…/haccp_models.g.dart` | Régénéré — cast nullable + fallback sur ces 2 champs uniquement |
| `test/core/models/haccp_models_test.dart` | **Nouveau** — 5 tests, dont un reproduisant exactement la forme actuellement servie par l'API déployée |

### 13.4 Validation

- `dart analyze` : mêmes 4 warnings pré-existants qu'en §12.6, rien de nouveau
- `flutter test test/` : OK, 24 tests (5 nouveaux)

### 13.5 Reste à faire

Le compteur température/traçabilité restera à 0 sur le header tant que l'incrément C n'est pas
déployé sur `origin/staging` (commit + push, jamais fait sans demande explicite). La tile
elle-même sera visible dès ce correctif — c'est la donnée qui reste figée à 0 jusqu'au déploiement.

# Audit — réutilisation de `brand_status` pour le statut de paiement Kiosk

> Audit du 2026-07-15. Aucune modification de code. Objectif : vérifier si
> `orders.brand_status` est un candidat sûr pour porter un statut de paiement
> carte borne (Kiosk, Stripe Terminal), avant toute décision d'implémentation.
>
> Rappel de contexte déjà actée (voir `docs/KIOSK_DECISIONS.md`, incrément
> "paiement carte borne") : le statut d'attente de paiement carte Kiosk est
> **déjà implémenté aujourd'hui**, mais via `merchant_approval =
> "PENDING_CARD_PAYMENT"` (`models.MerchantApprovalPendingCardPayment`,
> `internal/models/orders_model.go:25`), pas via `brand_status`. Le Kiosk
> n'écrit et ne lit `brand_status` nulle part (`grep` négatif sur tout
> `internal/modules/kiosk/`). Cet audit répond donc à la question : *si on
> voulait maintenant réutiliser `brand_status` pour un signal de paiement
> distinct*, quel est le risque ?

---

## 1. Usage actuel de `brand_status` (lecture + écriture, tous modules)

`brand_status` est **le champ pilote du cycle de vie métier** pour les trois
canaux existants (WELLO_RESTO/POS, UberEats, Deliveroo). Ce n'est pas un champ
au repos disponible pour un nouvel usage — il est massivement lu et écrit,
côté backend et côté deux clients frontend distincts.

### Écritures — WELLO_RESTO (ScanNOrder + POS)

| Fichier:ligne | Fonction | Valeur écrite |
|---|---|---|
| `internal/modules/order_life_cycle/repository.go:1646` | `insertOrderBase` | depuis `req.Order.BrandStatus` (`'PENDING'` ou `'ONLINE_PAYMENT_PENDING'`) |
| `internal/modules/order_life_cycle/repository.go:567` | `SetOrderAcceptedLocal` | `'PENDING'` |
| `internal/modules/order_life_cycle/repository.go:587-589` | `MarkOrderAsDeliveryStarted` | `'EN_ROUTE_TO_DROPOFF'` |
| `internal/modules/order_life_cycle/repository.go:621` | `DenyOrderLocal` | `'DENIED'` |
| `internal/modules/order_life_cycle/repository.go:384-390` | `SetDistributedProducts` | CASE : `'READY_FOR_HANDOFF'` / `'READY_FOR_TAKE_AWAY'` / `'DONE'` / `'PENDING'` |
| `internal/modules/order_life_cycle/repository.go:496-502` | `MarkProductsBackToProduction` | CASE : `'READY_FOR_HANDOFF'` / `'READY_FOR_TAKE_AWAY'` / `'CLOSED'` / `'PENDING'` |
| `internal/modules/order_life_cycle/repository.go:671-676` | `SetReadyForDistribution` | `'READY_FOR_HANDOFF'` / `'READY_FOR_TAKE_AWAY'` |
| `internal/modules/order_life_cycle/repository.go:755` | `DeleteOrderLocal` | `'CANCELED'` |
| `internal/modules/order_life_cycle/repository.go:765` | annulation (variante) | `'CANCELED'` |
| `internal/modules/order_life_cycle/repository.go:833` | `SetDeliveredLocal` | `'CLOSED'` |
| `internal/webhook/stripe/repository.go:132` | `UpdateOrderDetails` | `'PENDING_APPROVAL'` (`checkout.session.completed`) |
| `internal/modules/delivery_sessions/repository.go:295,383,435,1058` | session de livraison | `'EN_ROUTE_TO_DROPOFF'` / `'READY_FOR_HANDOFF'` (rollback) / `'DONE'` (force-close) / `'DELIVERY_FAILED'`/`'DELIVERY_CANCELED'` |

Détail complet des transitions et pièges déjà documentés dans
`docs/order-lifecycle.md` (11 scénarios de bout en bout, 11 incohérences
répertoriées P1-P11 — notamment **P5** : deux valeurs terminales différentes
selon le chemin de clôture, **P6** : deux fonctions qui écrivent des valeurs
différentes pour la même situation métier, **P8** : aucune constante Go, tout
en chaîne brute SQL).

### Écritures — UberEats (canal séparé, mêmes colonnes)

| Fichier:ligne | Valeur écrite |
|---|---|
| `internal/modules/ubereats/repository.go:156` | `'ACCEPTED'` |
| `internal/modules/ubereats/repository.go:199` | `'DENIED'` |
| `internal/modules/ubereats/repository.go:208` | `'CANCELED'` |
| `internal/modules/ubereats/repository.go:219` | `'READY_FOR_HANDOFF'` |
| `internal/modules/ubereats/repository.go:247` | valeur dynamique (paramètre) |
| `internal/modules/ubereats/repository.go:280` | CASE `'READY_FOR_HANDOFF'` → `'CLOSED'` sinon `'CANCELED'` |
| `internal/webhook/ubereats/repository/orders_repo.go:43,93,125` | `'CANCELED'`, `'EN_ROUTE_TO_DROPOFF'`, `'FAILED'` |
| `internal/webhook/ubereats/service/order_mapper.go:52` | `BrandStatus: order.CurrentState` — **passthrough brut de l'état UberEats**, aucune traduction |

### Écritures — Deliveroo (canal séparé, vocabulaire **minuscule** différent)

| Fichier:ligne | Valeur écrite |
|---|---|
| `internal/webhook/deliveroo_orders/repository.go:219` | valeur dynamique |
| `internal/webhook/deliveroo_orders/repository.go:248` | `'accepted'` ou `'scheduled'` (CASE) |
| `internal/webhook/deliveroo_orders/repository.go:254` | `'accepted'` |
| `internal/webhook/deliveroo_orders/repository.go:268` | `'confirmed'` |
| `internal/webhook/deliveroo_orders/service.go:381` | `BrandStatus: brandStatus` — passthrough |

Point notable : Deliveroo écrit des valeurs **en minuscules** (`'accepted'`,
`'confirmed'`, `'scheduled'`) alors que WELLO_RESTO/UberEats écrivent en
majuscules. `brand_status` est donc déjà un champ à la casse et au vocabulaire
non uniformes selon la marque — la colonne encode `brand + statut`, pas un
statut normalisé unique.

### Lectures backend (filtrage de listes, jobs)

| Fichier:ligne | Usage |
|---|---|
| `internal/modules/orders/repository.go:32` | `WHERE state IN ('OPEN') AND brand_status NOT IN ('ONLINE_PAYMENT_PENDING')` — **filtre la liste des commandes ouvertes visibles côté POS/back-office** |
| `internal/modules/orders/repository.go:234` | filtre dynamique `brand_status IN (...)` (recherche/rapport) |
| `internal/modules/orders/orders_fetcher_builder.go:587,591` | `brand_status` sélectionné et renvoyé dans chaque `Order` (POS, back-office, ScanNOrder) |
| `internal/tasks/orders.go:37,91` | cron `DenyOrders`/analogues — filtre sur valeurs précises (`READY_FOR_HANDOFF`, `READY_FOR_TAKE_AWAY`, `PENDING_APPROVAL`) — **cron désactivé en prod** (`cmd/api/tasks.go:17`), mais le code reste actif au sens logique |
| `internal/tasks/payments.go:22,39` | jobs de capture/annulation paiement — filtre `NOT IN ('DENIED','CANCELED')` / `IN ('DENIED','CANCELED')` |

### Lectures frontend — client 1 : `wello-resto-scannorder` (page de suivi commande, exposée au **client final**)

`brand_status` est **le champ pilote unique** de toute la timeline affichée au
client qui a scanné le QR code de sa commande :

- `src/components/tracking/orderStatusSteps.ts:107` — `const status =
  order?.brand_status ?? "PENDING"`, puis un grand switch/lookup
  (`ERROR_STATUSES`, `SUCCESS_TERMINAL`, `PRE_CONFIRMATION`,
  `READY_FOR_TAKE_AWAY`, `READY_FOR_HANDOFF`, `EN_ROUTE_TO_DROPOFF`) qui pilote
  le texte de statut, le sous-texte et l'étape courante de la timeline.
- `src/hooks/useOrder.ts:9` — `TERMINAL_BRAND_STATUSES.has(order.brand_status)`
  détermine si le polling s'arrête.
- `src/hooks/useDeliveryTracking.ts:109-110` — mêmes ensembles
  `ERROR_TERMINAL`/`SUCCESS_TERMINAL` sur `order.brand_status`.
- `src/lib/api/types.ts:407` — `brand_status: string` (contrat typé, pas de
  validation d'enum côté client).

**Fallback observé** (`orderStatusSteps.ts:181-212`) : toute valeur non
reconnue tombe silencieusement sur la branche par défaut → `statusText: "En
préparation"`, `currentIndex: 1`. Aucune erreur, aucun log — juste un mauvais
affichage.

### Lectures frontend — client 2 : `wello_resto_flutter` (POS + app livreur)

- `lib/models/orders/brand_status_enum.dart` — `BrandStatusEnum` fermé (12
  valeurs), `fromServerValue` retourne `null` si la valeur serveur ne
  correspond à aucun cas.
- `lib/models/orders/order_dto.dart:280,283` — `statusLabel`/`statusColor`
  dérivés de l'enum ; `null` en cas de valeur inconnue → repli implicite sur
  `AppColor.brandStatusUnknownColor` (blanc) côté UI badge.
- `lib/controllers/order/order_network_manager.dart:82,142-143` — logique de
  synchronisation de la liste des commandes ouvertes : exclut explicitement
  `BrandStatusEnum.onlinePaymentPending`.
- `lib/controllers/history_controller.dart:62`,
  `lib/ui/views/cash_register/daily_summary/daily_summary_view.dart:84,141`,
  `lib/ui/views/cash_register/history/history_item_cell.dart:26` — filtrage/
  affichage de l'historique caisse basé sur `brandStatus == canceled`.
  `order_history_details_dialog.dart:68-69,161,272` — résolution complète
  d'un badge de statut pour l'historique.
- `lib/ui/widgets/delivery/*.dart`,
  `lib/ui/pages/delivery/delivery_driver_page.dart:286` — logique de l'app
  livreur (masquage de bouton, tri de tournée) basée sur
  `BrandStatusEnum.enRouteToDropOff`.

**Conclusion §1** : `brand_status` est utilisé activement par **3 canaux
marchands** (WELLO_RESTO, UberEats, Deliveroo — vocabulaires différents), **2
crons** (désactivés mais logiquement actifs), **2 requêtes de filtrage de
listes** POS/back-office, et **2 clients frontend séparés** (page de suivi
client ScanNOrder, app POS+livreur Flutter). Ce n'est en aucun cas un champ
libre.

---

## 2. Usage actuel de `merchant_approval` (lecture + écriture, tous modules)

### Écritures

| Fichier:ligne | Fonction | Valeur |
|---|---|---|
| `internal/modules/order_life_cycle/repository.go:1646,1752-1753` | `insertOrderBase` | `'ACCEPTED'` (défaut POS/IN) ou `'PENDING_APPROVAL'` (ScanNOrder TAKE_AWAY) |
| `internal/modules/order_life_cycle/repository.go:568` | `SetOrderAcceptedLocal` | `'ACCEPTED'` |
| `internal/modules/order_life_cycle/repository.go:622,632` | `DenyOrderLocal` | `'DENIED'` |
| `internal/webhook/stripe/repository.go:133` | `UpdateOrderDetails` | `'PENDING_APPROVAL'` — **écrase toute valeur précédente**, y compris `'ACCEPTED'` (voir P7 ci-dessous) |
| `internal/modules/kiosk/service.go:1508,1782` | `CreateOrder`/annulation | `PENDING_CARD_PAYMENT` (carte) ou `ACCEPTED` (comptoir) ; `UpdateOrderMerchantApproval` remet `PENDING_APPROVAL` lors du switch carte→comptoir |
| `internal/modules/kiosk/repository.go:738` | `UpdateOrderMerchantApproval` | valeur libre passée par le service (scopée `merchant_id`) |
| `internal/webhook/ubereats/service/order_mapper.go:69` | mapping UberEats | `MerchantApprovalPendingApproval` |
| `internal/webhook/deliveroo_orders/service.go:380` | mapping Deliveroo | `MerchantApprovalPendingApproval` |
| `internal/webhook/deliveroo_orders/repository.go:219,249,254,268` | webhook Deliveroo | `'DENIED'` / `'ACCEPTED'` |
| `internal/modules/ubereats/repository.go:156,247` | webhook UberEats | `'ACCEPTED'` / dynamique |

### Lectures — décisions métier

| Consommateur | Fichier:ligne | Décision prise |
|---|---|---|
| **Cuisine / production (KDS-équivalent POS Flutter)** | `wello_resto_flutter/lib/controllers/production_controller.dart:124` | `order.merchantApproval == MerchantApprovalEnum.accepted` — **seules les commandes `ACCEPTED` apparaissent en production**. Toute autre valeur (y compris `PENDING_CARD_PAYMENT`) en est exclue par construction. |
| **File d'approbation restaurateur (overlay POS)** | `wello_resto_flutter/lib/controllers/order/order_network_manager.dart:97-98,159-160,237,248` | `merchantApproval == MerchantApprovalEnum.pendingApproval` bascule la commande dans l'overlay "à valider/refuser" |
| **Garde annulation client (ScanNOrder)** | `internal/modules/scannorder/service.go:730` | `if orderResp.MerchantApproval == "ACCEPTED"` → `ErrOrderAlreadyAccepted` (409), bloque l'auto-annulation client |
| **Auto-refus (cron, désactivé)** | `internal/tasks/orders.go:91-92` | `WHERE brand_status='PENDING_APPROVAL' AND merchant_approval='PENDING_APPROVAL'` |
| **Kiosk (confirmation paiement comptoir, switch carte→comptoir)** | `internal/modules/kiosk/service.go:1560,1618-1629,1725,1770` | vérifie `PENDING_APPROVAL`/`PENDING_CARD_PAYMENT` avant chaque transition |
| **Mapping statut client Kiosk** | `internal/modules/kiosk/service.go:1414-1426` (`mapMerchantApprovalToKioskStatus`) | traduit `merchant_approval` → statut JSON kiosk (`pending_counter_payment`, `pending_card_payment`, `accepted`, ou passthrough minuscule) |
| **NF525/chaînage** | — | **aucune lecture** de `merchant_approval` par le code de hash/signature (voir §4) |

### `PENDING_CARD_PAYMENT` — traité correctement ou ignoré ?

Résultat vérifié explicitement (grep + lecture code, confirmant l'analyse déjà
faite dans `docs/KIOSK_DECISIONS.md` "Blast radius vérifié") :

- **Backend** : traite `PENDING_CARD_PAYMENT` correctement partout où c'est
  nécessaire — `orders_fetcher_builder.go` le scanne en `string` libre (pas de
  switch qui pourrait planter), le cron `DenyOrders` ne le capture pas
  volontairement (comportement voulu), `mapMerchantApprovalToKioskStatus` a un
  `case` dédié.
- **POS Flutter (`wello_resto_flutter`)** : **`MerchantApprovalEnum` ne contient
  que deux valeurs, `accepted` et `pendingApproval`**
  (`lib/models/orders/merchant_approval_enum.dart:1-3`). `PENDING_CARD_PAYMENT`
  ne matche aucun des deux → `fromServerValue` retourne `null`. Conséquence
  pratique : une commande carte-en-attente n'apparaît **ni** en production
  (`production_controller.dart:124`, qui exige `== accepted`) **ni** dans
  l'overlay d'approbation (`order_network_manager.dart:97-98`, qui exige `==
  pendingApproval`). C'est le comportement *souhaité* aujourd'hui, mais il
  repose sur une absence de `case` plutôt que sur un traitement explicite — le
  jour où le POS voudra afficher "en attente de carte" quelque part
  (badge caisse, historique), il faudra ajouter la valeur à l'enum fermé, elle
  n'y est pas.
- **`wello-resto-scannorder`** : n'expose aucune lecture de `merchant_approval`
  dans le code source consulté — la page de suivi client ne pilote que sur
  `brand_status`. Non concerné par `PENDING_CARD_PAYMENT`.

**Conclusion §2** : `merchant_approval` est strictement scopé au workflow
d'acceptation cuisine/restaurateur (3 valeurs actives : `ACCEPTED`,
`PENDING_APPROVAL`, `DENIED`, +1 valeur Kiosk `PENDING_CARD_PAYMENT`). Il n'est
lu par aucun consommateur de paiement ou de suivi de commande client — son
usage est net et actuellement bien isolé.

---

## 3. Risque de collision si `brand_status` est réutilisé pour le paiement

**Risque réel et élevé — déconseillé.**

Si le Kiosk écrivait par exemple `brand_status = "AWAITING_CARD_PAYMENT"` :

1. **`wello-resto-scannorder` (client final)** — `orderStatusSteps.ts:205-212`
   ferait tomber cette valeur inconnue sur le repli par défaut : la commande
   afficherait **"En préparation"** alors qu'elle n'a même pas encore été
   acceptée ni payée. C'est trompeur pour le client, pas juste cosmétique — le
   commentaire en tête de fichier (`orderStatusSteps.ts:9-11`) affirme
   explicitement que "`brand_status` est le pilote métier" de cette page ;
   toute valeur ajoutée sans mise à jour de ce fichier casse silencieusement
   l'affichage.
   *(Cela dit : le Kiosk n'utilise pas aujourd'hui la page ScanNOrder pour le
   suivi client — mais si `brand_status` devient un canal de statut partagé,
   ce couplage devient un risque permanent à chaque évolution de l'un ou
   l'autre côté.)*
2. **`wello_resto_flutter` (POS)** — `BrandStatusEnum.fromServerValue` retourne
   `null` pour une valeur non listée → badge de statut blanc/vide
   (`brandStatusUnknownColor`) dans l'historique caisse et les listes. Pas de
   crash, mais un statut illisible pour le staff.
3. **Filtrage POS `orders/repository.go:32`** —
   `WHERE brand_status NOT IN ('ONLINE_PAYMENT_PENDING')` ne connaît que cette
   seule exclusion. Une nouvelle valeur `AWAITING_CARD_PAYMENT` **passerait ce
   filtre** et apparaîtrait dans la liste des commandes "ouvertes" retournée au
   POS/back-office, alors que la commande n'est ni acceptée ni prête — à moins
   d'ajouter explicitement cette valeur à l'exclusion (nouveau point de
   maintenance couplé).
4. **Trois canaux marchands se partagent déjà la colonne avec des vocabulaires
   incompatibles** (majuscules WELLO_RESTO/UberEats vs minuscules Deliveroo,
   passthrough brut de l'état UberEats `order_mapper.go:52`). Ajouter un
   quatrième vocabulaire (paiement Kiosk) dans la même colonne aggrave un
   champ déjà identifié comme fragile (`docs/order-lifecycle.md`, P5/P6/P8 :
   incohérences déjà connues entre fonctions qui écrivent des valeurs
   différentes pour la même situation).
5. Le webhook UberEats écrit `BrandStatus: order.CurrentState` en passthrough
   direct — si un jour une valeur de paiement Kiosk collisionne
   textuellement avec un état UberEats natif (peu probable mais non vérifié
   par un enum), le comportement des lectures ci-dessus serait indéterminé.

À l'inverse, `merchant_approval` a déjà absorbé un ajout de valeur
(`PENDING_CARD_PAYMENT`) de façon contrôlée, avec un blast-radius vérifié et
documenté (`docs/KIOSK_DECISIONS.md`). C'est la preuve que ce champ *peut*
absorber un état supplémentaire proprement — mais **sa sémantique reste
"décision restaurateur/cuisine"**, pas "état de paiement" : y encoder un
distinguo plus fin (carte en attente / carte échouée / basculé caisse / payé)
mélangerait à nouveau deux préoccupations dans un seul champ à enum fermé côté
POS Flutter.

**Conclusion §3** : `brand_status` n'est **pas** un candidat libre — c'est un
champ à trois canaux, deux frontends, vocabulaire déjà hétérogène. Le
réutiliser pour un statut de paiement Kiosk introduirait un risque de
régression d'affichage concret et vérifié dans au moins deux frontends.

---

## 4. Impact NF525/chaînage d'une mise à jour de statut post-création

**Aucun impact.** Le chaînage fiscal ne dépend ni de `brand_status` ni de
`merchant_approval`.

Le hash de clôture (`internal/modules/order_life_cycle/repository.go:740-773`
et `:822-855`) est calculé **une seule fois**, au moment de la clôture
(`DeleteOrderLocal`/`SetDeliveredLocal`), à partir de :

```go
payload := fmt.Sprintf("%s|%s|%d|%s", prevHash.String, deliveredOn, currentPrice, orderID)
newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
signature := security.SignHash(newHash)
```

Composants du payload : hash précédent de la chaîne (dernière commande
`CLOSED` du merchant), timestamp de clôture, prix final, ID de commande.
**Ni `brand_status` ni `merchant_approval` n'entrent dans ce calcul.** Le même
mécanisme existe pour les paiements (`repository.go:159-174`, chaîne
séparée sur `payments.hash`/`previous_hash`), également indépendant de ces deux
colonnes.

Conséquence directe pour le scénario "carte → caisse sur la même commande" :
toute transition intermédiaire (`PENDING_CARD_PAYMENT → PENDING_APPROVAL →
ACCEPTED → ... → CLOSED`, avec ou sans changement de `brand_status` en
parallèle) **ne touche ni ne recalcule** le hash — celui-ci n'est écrit
qu'à l'écriture terminale (`SetDeliveredLocal`/`DeleteOrderLocal`), sur les
seules colonnes `previous_hash`/`hash`/`signature`, en une seule opération.
Le nombre de mises à jour de statut avant clôture n'a aucune incidence sur
l'intégrité de la chaîne : il n'y a qu'un seul maillon par commande, posé une
seule fois, quel que soit le nombre de statuts traversés avant.

**Conclusion §4** : aucune contre-indication NF525 à faire transiter une
commande par plusieurs statuts de paiement avant sa clôture, quel que soit le
champ choisi pour les porter.

---

## 5. Recommandation : `brand_status` réutilisable, ou nouveau champ dédié ?

**Nouveau champ dédié — ne pas réutiliser `brand_status`.**

`brand_status` échoue le test de "champ libre d'usage réel" posé en
introduction : 3 canaux marchands actifs qui y écrivent avec des vocabulaires
distincts, 2 frontends qui le lisent pour piloter de l'UI utilisateur final
(page de suivi client, badges POS/historique), 2 requêtes de filtrage
backend qui excluent des valeurs précises par nom, et un historique
d'incohérences déjà documenté (P5/P6/P8 dans `docs/order-lifecycle.md`) qui
montre que ce champ souffre déjà d'un manque de rigueur d'enum — y ajouter un
axe sémantique de plus (paiement) au lieu de corriger l'existant serait
aggraver un problème connu plutôt que le contenir.

Répartition des responsabilités proposée :

- **`merchant_approval`** — reste strictement le **workflow d'acceptation
  cuisine/restaurateur** : `PENDING_APPROVAL` / `ACCEPTED` / `DENIED` (+
  `PENDING_CARD_PAYMENT` déjà existant, qui est à la frontière — voir note
  ci-dessous).
- **Nouveau champ, ex. `orders.payment_status`** (colonne dédiée, `VARCHAR`
  nullable, absente aujourd'hui — vérifié par `grep -i payment_status` sur
  tout le repo, aucune occurrence en base ni en Go) — porte **uniquement**
  l'état de paiement en cours : ex. `AWAITING_CARD_PAYMENT`, `CARD_FAILED`,
  `PAID_AT_COUNTER`, `NULL` (pas de paiement en attente / hors périmètre
  carte). Ce champ ne serait lu par **aucun** consommateur existant tant
  qu'il n'est pas explicitement branché — zéro risque de collision par
  construction, contrairement à `brand_status`.

**Note sur `PENDING_CARD_PAYMENT` existant** : cette valeur, actuellement dans
`merchant_approval`, est elle-même à cheval entre les deux préoccupations
("le restaurateur n'a pas encore vu la commande" ET "le paiement carte n'est
pas confirmé"). Elle a été un choix pragmatique documenté et vérifié
(`docs/KIOSK_DECISIONS.md`, "Blast radius vérifié") qui fonctionne parce que
`merchant_approval` n'admet qu'un seul état à la fois : *le restaurateur ne
doit voir la commande que si le paiement est réglé*, donc superposer les deux
dans un seul champ binaire (vu/pas vu) reste défendable. Un futur champ
`payment_status` séparé n'oblige pas à migrer cette valeur immédiatement — il
sert pour des distinctions **plus fines** que `merchant_approval` ne peut pas
porter sans casser son enum fermé côté POS Flutter (ex. différencier "carte en
attente" de "carte refusée, en attente de bascule caisse", deux états qui
aujourd'hui se confondraient tous les deux sous `PENDING_CARD_PAYMENT`).

---

## 6. Plan de migration proposé

1. **Colonne** — ajouter `orders.payment_status VARCHAR(30) NULL` (migration
   SQL dédiée, `internal/migrations/` ou `migrations/todo/` selon la
   convention du repo). `NULL` = comportement actuel pour tous les canaux
   existants (WELLO_RESTO/UberEats/Deliveroo ne posent jamais ce champ).
2. **Modèle Go** — ajouter `PaymentStatus *string \`json:"payment_status,omitempty"\``
   à `models.Order` (`internal/models/orders_model.go:195-238`), et des
   constantes nommées (contrairement à `brand_status`, ne pas répéter l'erreur
   P8) : `PaymentStatusAwaitingCard`, `PaymentStatusCardFailed`,
   `PaymentStatusPaidAtCounter`.
3. **Écriture — création commande Kiosk** — `internal/modules/kiosk/service.go`
   (zone `CreateOrder`, ~ligne 1481-1508) : poser `PaymentStatus =
   PaymentStatusAwaitingCard` en plus de (pas à la place de)
   `MerchantApproval = PENDING_CARD_PAYMENT`.
4. **Écriture — webhook Stripe Terminal** (`internal/webhook/stripe/service.go`,
   `HandlePaymentIntentSucceeded`/`payment_intent.payment_failed`) : basculer
   `payment_status` à `NULL` (paiement résolu) ou `PaymentStatusCardFailed`,
   en parallèle de la transition `merchant_approval` déjà en place.
5. **Écriture — bascule caisse** (`internal/modules/kiosk/service.go:1782`,
   `switch-to-counter-payment`) : poser `PaymentStatus =
   PaymentStatusPaidAtCounter` ou `NULL` selon la décision produit (à trancher
   séparément — hors scope de cet audit).
6. **Lecture — `mapMerchantApprovalToKioskStatus`** (`kiosk/service.go:1414`)
   n'a pas besoin de changer immédiatement (déjà correct sur
   `merchant_approval` seul) ; à terme, elle pourrait combiner
   `merchant_approval` + `payment_status` si le contrat JSON Kiosk veut
   distinguer plus finement les cas.
7. **Aucun changement requis** sur `orders/repository.go:32`,
   `orders_fetcher_builder.go`, `wello-resto-scannorder`,
   `wello_resto_flutter` — c'est précisément l'intérêt de ne pas toucher
   `brand_status` : ces consommateurs continuent de fonctionner à l'identique
   tant qu'ils ne lisent pas `payment_status`. Le POS Flutter/back-office
   pourra ajouter la lecture de `payment_status` plus tard, à son rythme,
   sans risque de régression sur l'existant.
8. **Documentation** — consigner la nouvelle colonne dans
   `docs/order-lifecycle.md` (nouvelle section "Table `orders` —
   `payment_status`") pour éviter de reproduire l'absence de documentation
   constatée sur `brand_status` (P8).

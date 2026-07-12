# Cycle de vie des commandes — WelloResto ScanNOrder

> Audit backend du 2026-06-30. Réfère-toi à ce document avant de modifier toute logique
> liée au statut, à l'affichage de progression, ou au polling de commande.
>
> Scope : `brand='WELLO_RESTO'` uniquement (ScanNOrder QR + app POS tablet).
> UberEats et Deliveroo ont leurs propres chemins dans `internal/webhook/` et ne sont pas couverts ici.

---

## Champs d'état

### Table `orders`

| Champ | Type SQL | Valeurs possibles | Rôle |
|---|---|---|---|
| `state` | VARCHAR | `'OPEN'`, `'CLOSED'` | Gate binaire pour toute mutation (guard `OrderStillOpen`). Ne trace pas les états intermédiaires. |
| `brand_status` | VARCHAR | voir tableau ci-dessous | Champ principal du cycle de vie côté métier. |
| `merchant_approval` | VARCHAR | `'ACCEPTED'`, `'PENDING_APPROVAL'`, `'DENIED'` | Décision du restaurateur sur la commande entrante. |
| `isPaid` | TINYINT | `0` / `1` | Calculé : `price <= SUM(payments WHERE enabled=1)`. Pas un enum métier. |
| `isDistributed` | TINYINT | `0` / `1` | Agrégat dérivé de `orderitems.isDistributed`. |
| `delivery_start` | DATETIME | NULL ou timestamp | Horodatage démarrage livreur. Écrit **uniquement** en flux mono-commande (`MarkOrderAsDeliveryStarted`). NULL pour les commandes en session de livraison. |
| `delivered_on` | DATETIME | NULL ou timestamp | Horodatage de clôture fiscale (NF525). |
| `deletion_reason_id` | FK | id motif | Écrit uniquement lors d'un refus (`DenyOrderLocal`) ou annulation (`DeleteOrderLocal`). |
| `deletion_comment` | VARCHAR | texte libre | Idem. |

**Aucune constante Go** n'est définie pour `brand_status` — toutes les valeurs sont des chaînes brutes dans le SQL. Seule exception : `MerchantApprovalPendingApproval = "PENDING_APPROVAL"` dans `internal/models/orders_model.go:17`.

#### Valeurs de `brand_status` (WELLO_RESTO)

| Valeur | Contexte d'apparition | Écrit par |
|---|---|---|
| `'PENDING'` | Commande créée (auto-acceptée, POS/IN) ou post-acceptation | `setOrderDefaults`, `SetOrderAcceptedLocal` |
| `'ONLINE_PAYMENT_PENDING'` | ScanNOrder avec paiement en ligne, Stripe non complété | `setOrderDefaults` (via `insertOrderBase`) |
| `'PENDING_APPROVAL'` | Paiement Stripe reçu, en attente acceptation restaurateur | `UpdateOrderDetails` (webhook Stripe) |
| `'READY_FOR_HANDOFF'` | Commande DELIVERY prête pour le livreur | `SetReadyForDistribution`, `SetDistributedProducts`, `CancelDeliverySession` (rollback) |
| `'READY_FOR_TAKE_AWAY'` | Commande TAKE_AWAY prête pour le client | `SetReadyForDistribution`, `SetDistributedProducts` |
| `'EN_ROUTE_TO_DROPOFF'` | Livreur en route | `MarkOrderAsDeliveryStarted` (mono), `StartDeliverySession` (session) |
| `'DELIVERY_FAILED'` | Stop de livraison échoué (session multi) | `terminalizeDeliveryStop` |
| `'DELIVERY_CANCELED'` | Stop de livraison annulé par le livreur (session multi) | `terminalizeDeliveryStop` |
| `'DONE'` | Clôture via force-close manager de session OU distribution totale IN | `CloseDeliverySession`, `SetDistributedProducts` (IN fully distributed) |
| `'CLOSED'` | Clôture standard : livraison confirmée, paiement validé | `SetDeliveredLocal` |
| `'DENIED'` | Refus restaurateur | `DenyOrderLocal` |
| `'CANCELED'` | Annulation par le staff | `DeleteOrderLocal` |

### Table `delivery_session`

| Champ | Valeurs possibles | Signification |
|---|---|---|
| `status` | `'active'`, `'done'`, `'canceled'` | État de la tournée |
| `current_order_id` | FK `orders.order_id` ou NULL | Stop sur lequel le livreur est actuellement focalisé |
| `start_date` | DATETIME | Timestamp de démarrage de la session |
| `end_date` | DATETIME NULL | Timestamp de clôture manuelle (`CloseMyDeliverySession`) |

### Table `delivery_session_order` (FSM par stop)

| Champ | Valeurs possibles |
|---|---|
| `status` | `'pending'` → `'en_route'` → `'arrived'` → `'delivered'` / `'failed'` / `'canceled'` |
| `arrived_at` | DATETIME NULL — écrit par `MarkDeliveryStopArrived` |
| `delivered_at` | DATETIME NULL — écrit par `FinalizeDeliveredStop` |
| `failed_at` / `canceled_at` | DATETIME NULL — écrits par `terminalizeDeliveryStop` |
| `fail_reason` | VARCHAR NULL — écrit par `terminalizeDeliveryStop` |

---

## Sites d'écriture — référence rapide

### `orders.brand_status`

| Fichier:ligne | Fonction | Valeur écrite | Condition |
|---|---|---|---|
| `order_life_cycle/repository.go:1646` | `insertOrderBase` | depuis `req.Order.BrandStatus` (défauté par `setOrderDefaults`) | INSERT création |
| `order_life_cycle/repository.go:567` | `SetOrderAcceptedLocal` | `'PENDING'` | Acceptation |
| `order_life_cycle/repository.go:587-589` | `MarkOrderAsDeliveryStarted` | `'EN_ROUTE_TO_DROPOFF'` | Démarrage mono-livraison |
| `order_life_cycle/repository.go:621` | `DenyOrderLocal` | `'DENIED'` | Refus restaurateur |
| `order_life_cycle/repository.go:384-390` | `SetDistributedProducts` | CASE : `'READY_FOR_HANDOFF'` / `'READY_FOR_TAKE_AWAY'` / `'DONE'` (IN) / `'PENDING'` (partiel) | Distribution produits |
| `order_life_cycle/repository.go:496-502` | `MarkProductsBackToProduction` | CASE : `'READY_FOR_HANDOFF'` / `'READY_FOR_TAKE_AWAY'` / `'CLOSED'` (IN) / `'PENDING'` | Retour en production |
| `order_life_cycle/repository.go:671-676` | `SetReadyForDistribution` | `'READY_FOR_HANDOFF'` (DELIVERY) / `'READY_FOR_TAKE_AWAY'` (TAKE_AWAY) | Prêt à distribuer |
| `order_life_cycle/repository.go:755` | `DeleteOrderLocal` | `'CANCELED'` | Suppression staff |
| `order_life_cycle/repository.go:833` | `SetDeliveredLocal` | `'CLOSED'` | Clôture fiscale |
| `webhook/stripe/repository.go:131` | `UpdateOrderDetails` | `'PENDING_APPROVAL'` | `checkout.session.completed` |
| `delivery_sessions/repository.go:295` | `StartDeliverySession` | `'EN_ROUTE_TO_DROPOFF'` | Création session (toutes commandes du lot) |
| `delivery_sessions/repository.go:383` | `CancelDeliverySession` | `'READY_FOR_HANDOFF'` | Annulation session (rollback EN_ROUTE uniquement) |
| `delivery_sessions/repository.go:435` | `CloseDeliverySession` | `'DONE'` | Force-close manager |
| `delivery_sessions/repository.go:1058` | `terminalizeDeliveryStop` | `'DELIVERY_FAILED'` ou `'DELIVERY_CANCELED'` | Stop terminal (échec/annulation) |

### `orders.merchant_approval`

| Fichier:ligne | Fonction | Valeur écrite |
|---|---|---|
| `order_life_cycle/repository.go:1646` | `insertOrderBase` | depuis `req.Order.MerchantApproval` (`'ACCEPTED'` défaut POS, `'PENDING_APPROVAL'` ScanNOrder TAKE_AWAY) |
| `order_life_cycle/repository.go:568` | `SetOrderAcceptedLocal` | `'ACCEPTED'` |
| `order_life_cycle/repository.go:622` | `DenyOrderLocal` | `'DENIED'` |
| `webhook/stripe/repository.go:132` | `UpdateOrderDetails` | `'PENDING_APPROVAL'` |

### `orders.state`

| Fichier:ligne | Fonction | Valeur écrite |
|---|---|---|
| `order_life_cycle/repository.go:72` | `ReopenClosedOrder` | `'OPEN'` |
| `order_life_cycle/repository.go:566` | `SetOrderAcceptedLocal` | `'OPEN'` (réaffirmation) |
| `order_life_cycle/repository.go:623` | `DenyOrderLocal` | `'CLOSED'` |
| `order_life_cycle/repository.go:754` | `DeleteOrderLocal` | `'CLOSED'` |
| `order_life_cycle/repository.go:834` | `SetDeliveredLocal` | `'CLOSED'` |
| `delivery_sessions/repository.go:436` | `CloseDeliverySession` | `'CLOSED'` |

`insertOrderBase` n'insère pas `state` explicitement — la colonne est `'OPEN'` par défaut DB.

---

## Timelines par scénario

### 1. IN — paiement en caisse

Commande passée depuis un QR code sur table, règlement au comptoir.

```
[1] Création
    Endpoint : POST /scannorder/{slug}/order
    Handler  : Handler.CreateOrderSNO (scannorder/handler.go:112)
    Service  : service.CreateOrderSNO (scannorder/service.go:667)
    ↳ scannorder/service.go:722-723 :
        order.MerchantApproval = "ACCEPTED"
        order.BrandStatus      = "PENDING"
    ↳ setOrderDefaults (order_life_cycle/repository.go:1728) :
        OnlinePayment=false → brand_status reste "PENDING"
        IsPaid = (0 == TTC) = false   (Payments=[])
    SQL : insertOrderBase (order_life_cycle/repository.go:1641)
    Pas de session Stripe (action='get_order', table présente).
    → state=OPEN (défaut DB), brand_status='PENDING', merchant_approval='ACCEPTED',
      isPaid=0, isDistributed=0

[2] Distribution cuisine (avant ou après paiement)
    Endpoint : POST /orders/{id}/set-distributed
    Handler  : SetDistributedProducts (order_life_cycle/handler.go:144)
    SQL      : SetDistributedProducts (repository.go:384-390)
    → Pour IN, fully distributed : brand_status='DONE', isDistributed=1, delivered_on=now
    → state=OPEN inchangé

[3] Paiement en caisse
    Endpoint : POST /orders/{id}/payments
    SQL      : AddPaymentAndReturnID (order_life_cycle/repository.go:209)
    → isPaid = (price <= SUM(enabled_payments)) → 1 quand montant total atteint

[4] Clôture
    Endpoint : POST /orders/{id}/set-delivered
    Handler  : SetDelivered (order_life_cycle/handler.go:306)
    SQL      : SetDeliveredLocal (repository.go:767, 830-843)
    → brand_status='CLOSED', state='CLOSED', isPaid=1, isDistributed=1, delivered_on=now
    → Génération reçu fiscal NF525, déduction stocks, reset fidélité, suppression QR

[TERMINAL] state='CLOSED', brand_status='CLOSED', merchant_approval='ACCEPTED',
           isPaid=1, isDistributed=1
```

---

### 2. IN — paiement en ligne (Stripe)

> **Note préliminaire** : Dans ScanNOrder, les commandes IN ont une table associée (location),
> donc `action='get_order'` — aucune session Stripe n'est créée. En pratique, ce flux
> n'existe pas via SNO. Le chemin ci-dessous ne s'applique que si `OnlinePayment=true` est
> forcé explicitement (configuration POS particulière).

```
[1] Création avec OnlinePayment=true
    Mêmes handlers que scénario 1, mais avec OnlinePayment=true dans le payload.
    ↳ setOrderDefaults (repository.go:1768) : brand_status='ONLINE_PAYMENT_PENDING'
    ↳ merchant_approval='PENDING_APPROVAL' (forcé par l'appelant)
    ↳ action='payment' → session Stripe créée, URL retournée au client
    → state=OPEN, brand_status='ONLINE_PAYMENT_PENDING', merchant_approval='PENDING_APPROVAL',
      isPaid=0

[2] checkout.session.completed (webhook Stripe)
    Handler : HandleCheckoutSessionCompleted (webhook/stripe/service.go:93)
    SQL A   : UpdateOrderPaymentStatus (stripe/repository.go:108-124)
              → isPaid = (price <= total_paid) → 1
    SQL B   : UpdateOrderDetails (stripe/repository.go:126-137)
              → brand_status='PENDING_APPROVAL', merchant_approval='PENDING_APPROVAL'
              ⚠ Écrase merchant_approval='ACCEPTED' s'il existait à la création
    SQL C   : UpdateOrderCreationDate (stripe/repository.go:151)
              → creation_date = UTC_TIMESTAMP() (reset horodatage)
    Check auto-accept : pour IN, aucun flag auto-accept ne s'applique.
    → brand_status='PENDING_APPROVAL', merchant_approval='PENDING_APPROVAL', isPaid=1

[3] Acceptation manuelle restaurateur
    Endpoint : POST /orders/{id}/accept
    SQL      : SetOrderAcceptedLocal (repository.go:563-569)
    → state='OPEN', brand_status='PENDING', merchant_approval='ACCEPTED'

[4-5] Distribution + Clôture
    Identique scénario 1 étapes [2] et [4].

[TERMINAL] state='CLOSED', brand_status='CLOSED', merchant_approval='ACCEPTED',
           isPaid=1, isDistributed=1
```

---

### 3. TAKE_AWAY — paiement en caisse (POS uniquement)

> ScanNOrder impose `OnlinePayment=true` pour tout TAKE_AWAY. Ce flux n'est accessible
> que depuis l'app POS authentifiée.

```
[1] Création via POS
    Endpoint : POST /orders (authentifié, pos/create_handler.go)
    ↳ setOrderDefaults : OnlinePayment=false → brand_status='PENDING',
      merchant_approval défaut 'ACCEPTED'
    → state=OPEN, brand_status='PENDING', merchant_approval='ACCEPTED', isPaid=0

[2] Prêt pour retrait
    Endpoint : POST /orders/{id}/ready_for_distribution
    SQL      : SetReadyForDistribution (repository.go:671-676)
    → Pour TAKE_AWAY : brand_status='READY_FOR_TAKE_AWAY'

[3] Paiement en caisse
    SQL : AddPaymentAndReturnID (repository.go:209) → isPaid=1

[4] Clôture
    SQL : SetDeliveredLocal (repository.go:830-843)
    → state='CLOSED', brand_status='CLOSED', isPaid=1, isDistributed=1

[TERMINAL] state='CLOSED', brand_status='CLOSED', merchant_approval='ACCEPTED',
           isPaid=1, isDistributed=1
```

---

### 4. TAKE_AWAY — paiement en ligne (parcours ScanNOrder standard)

C'est le **chemin nominal** de toutes les commandes ScanNOrder TAKE_AWAY.

```
[1] Création
    Endpoint : POST /scannorder/{slug}/order
    Service  : service.CreateOrderSNO (scannorder/service.go:667)
    ↳ scannorder/service.go:765-773 :
        order.OnlinePayment    = true
        order.MerchantApproval = "PENDING_APPROVAL"
    ↳ setOrderDefaults (repository.go:1768) : brand_status='ONLINE_PAYMENT_PENDING'
    SQL : insertOrderBase (repository.go:1641)
    Retour : action='payment', URL Stripe checkout
    → state=OPEN, brand_status='ONLINE_PAYMENT_PENDING', merchant_approval='PENDING_APPROVAL',
      isPaid=0, isDistributed=0

[2] checkout.session.completed (webhook Stripe)
    Handler : HandleCheckoutSessionCompleted (webhook/stripe/service.go:93)
    SQL A   : UpdateOrderPaymentStatus (stripe/repository.go:108-124) → isPaid=1
    SQL B   : UpdateOrderDetails (stripe/repository.go:126-137)
              → brand_status='PENDING_APPROVAL', merchant_approval='PENDING_APPROVAL'
    SQL C   : UpdateOrderCreationDate (stripe/repository.go:151) → creation_date reset
    Check auto-accept : if merchant_parameters.auto_accept_sno_take_away_orders=true
              → SetOrderAccepted appelé post-commit (passe directement à l'étape [3])
    → brand_status='PENDING_APPROVAL', merchant_approval='PENDING_APPROVAL', isPaid=1

[3] Acceptation (manuelle ou auto)
    Endpoint : POST /orders/{id}/accept
    SQL      : SetOrderAcceptedLocal (repository.go:563-569)
    → state='OPEN', brand_status='PENDING', merchant_approval='ACCEPTED'

[4] Cuisine → Prêt pour retrait
    Endpoint : POST /orders/{id}/ready_for_distribution
    SQL      : SetReadyForDistribution (repository.go:671-676)
    → brand_status='READY_FOR_TAKE_AWAY'

[5] Retrait client → Clôture
    Endpoint : POST /orders/{id}/set-delivered
    SQL      : SetDeliveredLocal (repository.go:830-843)
    → state='CLOSED', brand_status='CLOSED', isPaid=1, isDistributed=1
    Ou : auto-clôture cron CloseOrders après 12h si isPaid=1 AND isDistributed=1
         (cron désactivé — voir §Pièges P4)

[TERMINAL] state='CLOSED', brand_status='CLOSED', merchant_approval='ACCEPTED',
           isPaid=1, isDistributed=1
```

---

### 5. DELIVERY mono-commande

Un livreur dédié à une seule commande, sans session de livraison.

```
[1] Création + Acceptation
    Idem scénario 3 ou 4 selon le mode de paiement.
    Pour DELIVERY, setOrderDefaults suit la même logique (PENDING si cash, ONLINE_PAYMENT_PENDING
    si online). L'order_type=DELIVERY ne change pas la logique de création initiale.
    → brand_status='PENDING' (cash) ou 'ONLINE_PAYMENT_PENDING' (online)
    → merchant_approval='ACCEPTED' (cash) ou 'PENDING_APPROVAL' (online)

[2] Cuisine → Prêt pour livraison
    Endpoint : POST /orders/{id}/ready_for_distribution
    SQL      : SetReadyForDistribution (repository.go:671-676)
    → Pour DELIVERY : brand_status='READY_FOR_HANDOFF'

    OU via distribution produits :
    SQL      : SetDistributedProducts (repository.go:384-390)
    → Pour DELIVERY fully distributed : brand_status='READY_FOR_HANDOFF', isDistributed=1

[3] Démarrage livraison (livreur part)
    Endpoint : POST /orders/{id}/start_delivery?user_id=X
    Handler  : StartDelivery (order_life_cycle/handler.go:203)
    SQL      : MarkOrderAsDeliveryStarted (repository.go:579-611)
    → brand_status='EN_ROUTE_TO_DROPOFF'
    → delivery_start = UTC_TIMESTAMP  ← seul point d'écriture de ce champ (WELLO_RESTO)
    → responsible = userID

[4] Livraison confirmée
    Endpoint : POST /orders/{id}/delivered
    Handler  : SetDelivered (order_life_cycle/handler.go:306)
    SQL      : SetDeliveredLocal (repository.go:767, 830-843)
    → brand_status='CLOSED', state='CLOSED', isPaid=1, isDistributed=1, delivered_on=now
    → Fiscal NF525 (hash/signature chain)
    → Auto-clôture session si cette commande était la dernière OPEN d'une session
      (repository.go:866-881)

[TERMINAL] state='CLOSED', brand_status='CLOSED', isPaid=1, isDistributed=1,
           delivery_start=<timestamp>
           SESSION : N/A
```

---

### 6. DELIVERY multi-commandes (delivery_session / tournée)

```
[Pré-requis] Plusieurs commandes en brand_status='READY_FOR_HANDOFF', state='OPEN'.

[1] Création de session (dispatcher / manager)
    Endpoint : POST /delivery_sessions/start
    Handler  : handler.go:47 → StartDeliverySession
    SQL (delivery_sessions/repository.go:246-327) :
        INSERT delivery_session (status='active', start_date=UTC_TIMESTAMP, ...)
        INSERT delivery_session_order (delivery_session_id, order_id, priority)
            → status='pending' (DEFAULT colonne)
        UPDATE orders SET brand_status='EN_ROUTE_TO_DROPOFF'
            WHERE order_id IN (commandes du lot)   ← atomique sur tout le lot
    ⚠ delivery_start N'EST PAS écrit sur les commandes (contrairement au flux mono)
    → SESSION : status='active'
    → COMMANDES : brand_status='EN_ROUTE_TO_DROPOFF', state='OPEN'

[2] Livreur sélectionne un stop (prochaine destination)
    Endpoint : PATCH /delivery_sessions/me/stops/{order_id}/select
    Handler  : SelectDeliveryStop (delivery_sessions/handler.go:147)
    SQL (repository.go:795-815) :
        UPDATE delivery_session_order SET status='pending'   ← autres stops en_route/arrived → pending
            WHERE delivery_session_id=? AND order_id!=? AND status IN ('en_route','arrived')
        UPDATE delivery_session_order SET status='en_route'  ← stop cible
            WHERE delivery_session_id=? AND order_id=?
        UPDATE delivery_session SET current_order_id=?
    → STOP CIBLE : status='en_route'
    → COMMANDE : brand_status inchangé ('EN_ROUTE_TO_DROPOFF')

[3] Livreur arrive chez le client (optionnel)
    Endpoint : PATCH /delivery_sessions/me/stops/{order_id}/arrived
    SQL (repository.go:851-854) :
        UPDATE delivery_session_order SET status='arrived', arrived_at=UTC_TIMESTAMP()
    → STOP : status='arrived'
    → COMMANDE : brand_status inchangé

[4a] Stop livré (terminal positif)
    Endpoint : PATCH /delivery_sessions/me/stops/{order_id}/delivered
    Handler  : MarkDeliveryStopDelivered (delivery_sessions/handler.go:193)
    Appelle  : SetDeliveredLocal (order_life_cycle/repository.go:767) :
        → COMMANDE : brand_status='CLOSED', state='CLOSED', isPaid=1, isDistributed=1
    Puis : FinalizeDeliveredStop (delivery_sessions/repository.go:931-934) :
        → STOP : status='delivered', delivered_at=UTC_TIMESTAMP
        → UPDATE payments SET user_id=ds.user_id  (paiements réassignés au livreur)
    Puis : advanceCurrentStop (repository.go:978-982) :
        → prochain stop pending → status='en_route'
        → SESSION : current_order_id=prochain stop
    Auto-clôture si dernier order OPEN (repository.go:866-881) :
        → SESSION : status='done'

[4b] Stop échoué
    Endpoint : PATCH /delivery_sessions/me/stops/{order_id}/failed
    SQL      : terminalizeDeliveryStop (delivery_sessions/repository.go:1039-1050)
    → STOP : status='failed', failed_at=UTC_TIMESTAMP, fail_reason=...
    → COMMANDE : brand_status='DELIVERY_FAILED'
    → advanceCurrentStop automatique

[4c] Stop annulé par le livreur
    Endpoint : PATCH /delivery_sessions/me/stops/{order_id}/canceled
    SQL      : terminalizeDeliveryStop (repository.go:1039-1050)
    → STOP : status='canceled', canceled_at=UTC_TIMESTAMP
    → COMMANDE : brand_status='DELIVERY_CANCELED'

[5] Clôture session

    A — Auto (quand dernier order OPEN livré, inside SetDeliveredLocal) :
        repository.go:866-881 : UPDATE delivery_session SET status='done'

    B — Manuelle livreur :
        Endpoint : PATCH /delivery_sessions/me/close
        Handler  : CloseMyDeliverySession (delivery_sessions/handler.go:275)
        SQL (repository.go:1116-1120) :
            UPDATE delivery_session SET status='done', end_date=UTC_TIMESTAMP()
        Guard : ErrSessionHasPendingStops si des stops ne sont pas en état terminal.

    C — Force-close manager :
        Endpoint : POST /delivery_sessions/{id}/close
        Handler  : CloseDeliverySession (delivery_sessions/handler.go:96)
        SQL (repository.go:435) :
            UPDATE orders SET brand_status='DONE', state='CLOSED'   ← 'DONE', pas 'CLOSED' !
            UPDATE delivery_session SET status='done'
            UPDATE payments SET user_id=ds.user_id

[TERMINAL — parcours normal par-stop]
    COMMANDE : brand_status='CLOSED', state='CLOSED', isPaid=1, isDistributed=1
    SESSION  : status='done'

[TERMINAL — force-close manager]
    COMMANDE : brand_status='DONE', state='CLOSED'   ← incohérence, voir §Pièges P5
    SESSION  : status='done'
```

---

### 7. Annulation par le client (fenêtre 60 secondes)

> ⚠ **Cet endpoint est un no-op côté backend.**

```
[Tentative d'annulation]
    Endpoint : DELETE /scannorder/{slug}/order/{id}
    Handler  : Handler.CancelOrderSNO (scannorder/handler.go:95)
    Service  : service.CancelOrderSNO (scannorder/service.go:618)

[Vérification du délai (scannorder/service.go:654-656)]
    calc := now - order.creation_date.Unix()
    if calc > 60 → return ErrTooLateToDeleteOrder

    Dans la fenêtre ≤ 60s :
        → retourne payload debug {calc, now, creation_date}
        → AUCUNE mise à jour DB
        → AUCUN appel Stripe (pas de remboursement)
        → AUCUN changement d'état

    Hors fenêtre > 60s :
        → erreur HTTP
        → AUCUNE mise à jour DB

[Résultat]
    La commande reste dans son état au moment de l'appel.
    - Non payée : brand_status='ONLINE_PAYMENT_PENDING', merchant_approval='PENDING_APPROVAL'
    - Payée     : brand_status='PENDING_APPROVAL', merchant_approval='PENDING_APPROVAL'
    state='OPEN' dans les deux cas.

    Le cleanup éventuel dépend du webhook Stripe checkout.session.expired (→ scénario 10).

[TERMINAL] Aucun — l'endpoint ne produit aucun état terminal.
```

---

### 8. Refus par le restaurateur (DENIED)

```
[Refus]
    Handler : OrdersLifeCycleHandler.DenyOrder (order_life_cycle/handler.go:225)
    Service : SetOrderDenied (service.go:774 → 711)
    SQL A   : DenyOrderLocal (repository.go:614-648)
              UPDATE orders SET
                  brand_status='DENIED',
                  merchant_approval='DENIED',
                  state='CLOSED',
                  deletion_reason_id=?,
                  deletion_comment=?,
                  last_update=UTC_TIMESTAMP
              WHERE order_id=?
    SQL B   : DisablePayments (service.go:721) :
              UPDATE payments SET enabled=0 WHERE order_id=?
              ⚠ Désactive les enregistrements en DB — NE déclenche PAS de remboursement Stripe
    SQL C   : customer_rewards reset :
              UPDATE customer_rewards SET is_used=false, usage_date=null,
                used_on_order_id=null WHERE used_on_order_id=?
    Puis    : redis cache invalidé, notification async

    Si commande déjà payée (isPaid=1) au moment du refus :
        → Paiements désactivés en DB (enabled=0)
        → Montant Stripe NON remboursé automatiquement
        → Remboursement manuel nécessaire (cron CancelPayments — actuellement désactivé)

[TERMINAL] state='CLOSED', brand_status='DENIED', merchant_approval='DENIED'
```

---

### 9. Annulation par le restaurateur après acceptation

Il n'existe **pas d'endpoint dédié** "annulation post-acceptation". Cependant, les deux endpoints suivants fonctionnent malgré tout sur une commande acceptée car le guard `OrderStillOpen` ne vérifie que `state='OPEN'`, et une commande acceptée reste `state='OPEN'`.

```
[Via DenyOrder]
    Même chemin que scénario 8.
    La condition OrderStillOpen passe (state='OPEN' après SetOrderAcceptedLocal).
    TERMINAL : state='CLOSED', brand_status='DENIED', merchant_approval='DENIED'

[Via DeleteOrder]
    Handler : handler.go:275
    SQL     : DeleteOrderLocal (repository.go:749-763)
              UPDATE orders SET brand_status='CANCELED', state='CLOSED',
                delivered_on=UTC_TIMESTAMP, ...
    TERMINAL : state='CLOSED', brand_status='CANCELED'

    Note : DeleteOrderLocal écrit aussi hash/signature NF525 (chaîne fiscale),
    et deletion_reason_id/deletion_comment.
```

---

### 10. Paiement en ligne — timeout ou échec Stripe

```
[État initial]
    brand_status='ONLINE_PAYMENT_PENDING', state='OPEN', isPaid=0
    Session Stripe créée, client n'a pas encore payé.

[Chemin 1 — checkout.session.expired (webhook Stripe)]
    Handler : HandleCheckoutSessionCanceled (webhook/stripe/service.go:232)
    Appelle : orderlifecycle.SetOrderDenied(ctx, orderID,
                deletionReasonID="43",
                comment="Session de paiement expirée ou annulée")
    → même chemin que scénario 8 (DenyOrderLocal)
    TERMINAL : state='CLOSED', brand_status='DENIED', merchant_approval='DENIED'

[Chemin 2 — cron DenyOrders (tasks/orders.go:61)]
    Requête : WHERE brand_status='PENDING_APPROVAL' AND merchant_approval='PENDING_APPROVAL'
    ⚠ Ne correspond PAS à 'ONLINE_PAYMENT_PENDING'
    → Les commandes non payées ne sont PAS prises en charge par ce cron
    ⚠ De plus : toutes les tâches cron sont désactivées (cmd/api/tasks.go:17 → return immédiat)

[Si le webhook Stripe n'arrive jamais]
    → Aucun cleanup automatique côté backend
    → Commande reste en ONLINE_PAYMENT_PENDING / state=OPEN indéfiniment
    → ÉTAT ORPHELIN CONFIRMÉ (voir §Pièges P3)

[TERMINAL si webhook reçu] state='CLOSED', brand_status='DENIED', merchant_approval='DENIED'
[TERMINAL si webhook absent] Aucun — commande bloquée indéfiniment.
```

---

### 11. Rollback de session de livraison (annulation en cours de tournée)

```
[Pré-requis]
    Session active (status='active'), commandes en brand_status='EN_ROUTE_TO_DROPOFF'.
    Certaines commandes peuvent déjà être 'CLOSED' (livrées avant l'annulation).

[Annulation session]
    Endpoint : DELETE ou POST /delivery_sessions/{id}/cancel
    Handler  : CancelDeliverySession (delivery_sessions/handler.go:74)
    Service  : service.go:354
    SQL A (repository.go:379-386) — rollback commandes non livrées :
        UPDATE orders SET brand_status='READY_FOR_HANDOFF'
        WHERE orders INNER JOIN delivery_session_order
              AND brand_status='EN_ROUTE_TO_DROPOFF'
              AND delivery_session_id=?
        Guard : seules les commandes encore à 'EN_ROUTE_TO_DROPOFF' sont rollbackées
    SQL B (repository.go:395-402) — clôture session :
        UPDATE delivery_session SET status='canceled'
        WHERE id=? AND status='active'

[Champs NON modifiés lors de l'annulation]
    - orders.state          → reste 'OPEN' (commandes re-dispatchables)
    - orders.isPaid         → inchangé
    - orders.isDistributed  → inchangé
    - orders.delivery_start → inchangé (était NULL pour les commandes de session)
    - delivery_session_order.status → inchangé (stops conservent leur état courant)

[Commandes déjà livrées au moment de l'annulation]
    brand_status='CLOSED' → NON touchées (filtre WHERE brand_status='EN_ROUTE_TO_DROPOFF')

[Après annulation]
    Commandes rollbackées :
        brand_status='READY_FOR_HANDOFF', state='OPEN'
        → Même état qu'une commande fraîchement prête pour dispatch
        → Peuvent être réassignées à une nouvelle session immédiatement

[TERMINAL SESSION]               status='canceled'
[TERMINAL commandes rollbackées] brand_status='READY_FOR_HANDOFF', state='OPEN'
[TERMINAL commandes déjà livrées] brand_status='CLOSED', state='CLOSED' (non affectées)
```

---

## Pièges et incohérences identifiés

### P1 — `state` ne modélise pas les états intermédiaires (champ partiellement fiable)

`orders.state` n'a que deux valeurs réelles en pratique : `'OPEN'` (défaut DB à l'INSERT) et `'CLOSED'` (terminal). Tout le cycle de vie intermédiaire est porté par `brand_status`.

La valeur `'DONE'` apparaît dans des guards de requêtes en lecture (`tasks/orders.go:67`, `scannorder/service.go:643`, `stats/repository.go:253`, `cash_registers/repository.go:278`, `locations/repository.go:43`) mais n'est **jamais écrite** dans `orders.state` par aucun code actif. Ce sont des guards défensifs hérités — la branche `state='DONE'` est du code mort.

### P2 — ~~`CancelOrderSNO` est un no-op : l'annulation client ne fonctionne pas~~ (corrigé le 2026-06-30)

**Corrigé.** `CancelOrderSNO` implémente désormais `DeleteOrder` complet + remboursement Stripe conditionnel + garde combinée sur `merchant_approval` et délai 60s (alignée sur la politique Uber Eats).

Comportement post-correction :
- `merchant_approval == "ACCEPTED"` → `ErrOrderAlreadyAccepted` (HTTP 409) — le restaurant a déjà accepté.
- `calc > 60s` → `ErrTooLateToDeleteOrder` (HTTP 403) — fenêtre dépassée.
- Sinon → `DeleteOrder` complet (state='CLOSED', brand_status='CANCELED', NF525, rewards, QR, payments, cache Redis). Si un paiement Stripe actif existe, `RefundOrCancelAsync` est déclenché en async après le commit DB. Si la lecture du paiement Stripe échoue, l'annulation se poursuit sans remboursement (loggé en erreur).

Fichiers modifiés :
- `internal/models/responses_models.go` — ajout de `ErrOrderAlreadyAccepted`
- `internal/modules/scannorder/repository.go` — ajout de `GetStripePaymentForOrder`
- `internal/modules/scannorder/service.go` — implémentation complète de `CancelOrderSNO`

### P3 — `ONLINE_PAYMENT_PENDING` est un état orphelin

Les commandes SNO avec paiement en ligne démarrent à `brand_status='ONLINE_PAYMENT_PENDING'`. La seule sortie garantie est le webhook Stripe :
- `checkout.session.completed` → `'PENDING_APPROVAL'`
- `checkout.session.expired` → `'DENIED'` (via `SetOrderDenied`)

Le cron `DenyOrders` (`tasks/orders.go:61`) filtre sur `brand_status='PENDING_APPROVAL'` — il ne couvre **pas** `'ONLINE_PAYMENT_PENDING'`. Si le webhook Stripe n'arrive pas (panne, endpoint mal configuré), la commande reste bloquée à `ONLINE_PAYMENT_PENDING` / `state='OPEN'` indéfiniment.

### P4 — Toutes les tâches cron sont désactivées en production

`cmd/api/tasks.go:17` contient un `return` immédiat après `cron.New()`. Les tâches suivantes **ne s'exécutent jamais** :
- `DenyOrders` — auto-refus après 15 min sans acceptation (cherche `PENDING_APPROVAL`)
- `CapturePayments` — capture des paiements différés Stripe
- `CancelPayments` — annulation des paiements en attente (`REQUIRES_CONFIRMATION`)
- `CloseOrders` — auto-clôture des TAKE_AWAY après 12h (cherche `isPaid=1 AND isDistributed=1`)

L'intégralité du cycle de vie repose sur des actions HTTP explicites et les webhooks Stripe.

### P5 — `DONE` vs `CLOSED` : deux valeurs terminales pour la livraison

La valeur terminale de `brand_status` diffère selon le chemin de clôture :

| Chemin | brand_status final |
|---|---|
| Per-stop `MarkDeliveryStopDelivered` → `SetDeliveredLocal` | `'CLOSED'` |
| Mono-commande `SetDelivered` → `SetDeliveredLocal` | `'CLOSED'` |
| Force-close manager `CloseDeliverySession` | `'DONE'` |

Toute requête cherchant les commandes livrées doit inclure les deux : `brand_status IN ('CLOSED', 'DONE')`. Les queries de stats (`stats/repository.go:253, 361, 477`) font déjà ce double-check.

### P6 — `SetDistributedProducts` vs `MarkProductsBackToProduction` : valeurs inconsistantes pour les commandes IN

Pour une commande IN entièrement distribuée :
- `SetDistributedProducts` (`repository.go:384-390`) → `brand_status='DONE'`
- `MarkProductsBackToProduction` (`repository.go:496-502`) → `brand_status='CLOSED'`

Même condition sémantique, deux valeurs différentes. `'CLOSED'` via `MarkProductsBackToProduction` est sémantiquement incohérent ("retour en production" qui résulte en un état "clôturé").

### P7 — Le webhook Stripe écrase `merchant_approval='ACCEPTED'`

`UpdateOrderDetails` (`stripe/repository.go:126-137`) écrit systématiquement `merchant_approval='PENDING_APPROVAL'` lors de `checkout.session.completed`, **quelle que soit la valeur précédente**. Pour les commandes IN — qui ont `merchant_approval='ACCEPTED'` dès la création — si un paiement en ligne est déclenché, le webhook réinitialise la décision, forçant une ré-acceptation manuelle.

### P8 — Aucune constante Go pour `brand_status`

Toutes les valeurs de `brand_status` sont des chaînes brutes dans le SQL. La seule constante nommée est `MerchantApprovalPendingApproval = "PENDING_APPROVAL"` (`models/orders_model.go:17`). Toute typo dans une valeur de `brand_status` est silencieuse à la compilation.

### P9 — `delivery_start` absent des commandes en session de livraison

`MarkOrderAsDeliveryStarted` (`order_life_cycle/repository.go:588`) est le seul point d'écriture de `delivery_start` pour WELLO_RESTO. Il n'est appelé qu'en flux mono-commande. `StartDeliverySession` (`delivery_sessions/repository.go:246-327`) met les commandes à `'EN_ROUTE_TO_DROPOFF'` sans toucher `delivery_start`. Pour toutes les commandes d'une tournée, `delivery_start` reste `NULL` même pendant la livraison.

### P10 — Refus sans remboursement Stripe automatique

`DenyOrderLocal` désactive les paiements en DB (`payments.enabled=0`) mais n'appelle pas l'API Stripe pour rembourser. Si une commande est refusée après que le client a payé (`isPaid=1`), le montant reste capturé chez Stripe. Le remboursement doit être déclenché manuellement (ou via `CancelPayments` cron — actuellement désactivé, voir P4).

### P11 — Valeurs `merchant_approval` potentiellement attendues côté client sous les mauvaises formes

Le backend écrit exclusivement `"PENDING_APPROVAL"` (jamais `"PENDING"`) et `"DENIED"` (jamais `"REFUSED"`). Aucune occurrence de ces variantes alternatives n'a été trouvée dans `internal/`. Si un client (app mobile, frontend) compare `merchant_approval == "PENDING"` ou `== "REFUSED"`, ces comparaisons ne matcheront jamais.

---

## Schéma récapitulatif

```
IN (en caisse)
  → PENDING ──[cuisine]──► DONE ──[paiement]──► CLOSED
    state: OPEN ─────────────────────────────────────── CLOSED

IN (online, cas rare)
  → ONLINE_PAYMENT_PENDING ──[stripe]──► PENDING_APPROVAL ──[accept]──► PENDING ──[cuisine]──► DONE ──► CLOSED

TAKE_AWAY (caisse, POS seulement)
  → PENDING ──[prêt]──► READY_FOR_TAKE_AWAY ──[paiement + clôture]──► CLOSED

TAKE_AWAY (online, SNO standard)
  → ONLINE_PAYMENT_PENDING ──[stripe]──► PENDING_APPROVAL ──[accept]──► PENDING
      ──[cuisine]──► READY_FOR_TAKE_AWAY ──[retrait]──► CLOSED
  Annulation Stripe : ONLINE_PAYMENT_PENDING ──[checkout.session.expired]──► DENIED
  Auto-refus (cron désactivé) : PENDING_APPROVAL ──[15min]──► DENIED

DELIVERY mono-commande
  → PENDING ──[prêt]──► READY_FOR_HANDOFF ──[démarrage]──► EN_ROUTE_TO_DROPOFF ──[livré]──► CLOSED

DELIVERY session (tournée)
  [toutes commandes] → READY_FOR_HANDOFF
  [session start]    → EN_ROUTE_TO_DROPOFF (lot atomique)
  [stop livré]       → CLOSED       (via SetDeliveredLocal)
  [stop échoué]      → DELIVERY_FAILED
  [stop annulé]      → DELIVERY_CANCELED
  [force-close mgr]  → DONE         ← différent de CLOSED !
  [annulation session]→ READY_FOR_HANDOFF (rollback des EN_ROUTE uniquement, state=OPEN)

REFUS / ANNULATION
  (state=OPEN) ──[DenyOrder]──►   DENIED,   state=CLOSED
  (state=OPEN) ──[DeleteOrder]──► CANCELED, state=CLOSED
```

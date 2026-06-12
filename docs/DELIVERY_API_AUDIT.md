# Audit API Livraison — État de l'existant

> Objectif : recenser ce qui existe déjà dans le backend (`ib-welloresto-api`) pour préparer la conception d'une **API/app livreur** (suivi de tournée, position en temps réel, encaissement à la livraison, etc.), et lister précisément ce qui manque.

## ⚠️ Avertissement méthodologique

- Les tables "historiques" suivantes **n'ont aucun `CREATE TABLE` dans `migrations/`** (le dossier commence à `001`, ces tables sont antérieures) : `orders`, `customer`, `delivery_session`, `delivery_session_order`, `users`, `users_rights`, `merchant`, `merchant_parameters`, `payments`, `receipts`, `restaurant_ticket`, `stripe_payments`, `user_status_view`.
- Les schémas ci-dessous sont donc **reconstruits à partir des requêtes SQL Go** (`SELECT`/`INSERT`/`UPDATE`) trouvées dans les repositories. **Seules les colonnes effectivement référencées par le code sont documentées** — d'autres colonnes peuvent exister en base sans être utilisées.
- **Convention de migrations** observée : `001`–`025` = un seul fichier `NNN_description.sql` ; `026` et au-delà = paire `NNN_description.up.sql` / `NNN_description.down.sql`. La plus récente est `031_add_pin_hash_to_users_rights` (PIN). **Le prochain numéro disponible est `032`**.
- Tags utilisés dans ce document :
  - `[EXISTE]` — fonctionnalité présente et opérationnelle, réutilisable telle quelle.
  - `[PARTIEL]` — la brique de base existe mais ne couvre pas (ou pas complètement) le besoin.
  - `[ABSENT]` — rien d'équivalent trouvé, à construire.
  - `[À VÉRIFIER]` — hypothèse qui doit être confirmée en base ou en lisant du code non audité ici.
  - `[DETTE TECHNIQUE]` — problème existant indépendant du sujet "livraison" mais qui impactera le travail.

---

## 1. Commandes (`orders` / `customer`)

### 1.1 Structure `Order` (`internal/models/orders_model.go:148-187`)

| Champ Go | Type | Pertinence livraison |
|---|---|---|
| `OrderID` | `string` | PK séquentielle (`LastInsertId`), **pas un identifiant opaque** |
| `OrderNum` | `*string` | Numéro d'affichage cuisine, compteur rotatif 1-99 (pas un identifiant unique global) |
| `DeliverySessionID` | `*string` | FK vers `delivery_session.id` |
| `DeliveryPriority` | `*int` | = `delivery_session_order.priority` (rang dans la tournée) |
| `Brand` | `*string` | `UBER_EATS`, `DELIVEROO`, ou nul (commande maison) |
| `BrandOrderID` / `BrandOrderNum` | `*string` | identifiants côté plateforme externe |
| `BrandStatus` | `*string` | statut "miroir" — voir 1.3, **champ partagé entre plusieurs mécanismes** |
| `OrderType` | `*string` | `IN` / `TAKE_AWAY` / `DELIVERY` |
| `FulfillmentType` | `*string` | `DELIVERY_BY_RESTAURANT` / `DELIVEROO` |
| `State` | `*string` | état du cycle de vie interne (piloté par `order_life_cycle`) |
| `CutleryNotes` | `*string` | note "couverts", **pas une instruction de livraison** |
| `DeliveryFees` | `*int64` | frais de livraison |
| `Customer` | `*Customer` | voir 1.2 |
| `Responsible` | `*OrderUser` | employé responsable |
| `Priority` | `*int` | priorité **file cuisine** (≠ `DeliveryPriority`) |
| `CreationDate` / `LastUpdate` | `int64` | timestamps Unix |
| `DeliverySession` | `*DeliverySession` | objet imbriqué (voir §2 — struct dupliquée) |
| `Payments` | `[]Payment` | voir §7 |
| `IsPaid`, `IsDistributed`, `IsSNO`, `IsDelivery`, `MerchantApproval` | divers | flags de statut |

### 1.2 Structure `Customer` & adresse de livraison (`internal/models/orders_model.go:112-146`)

Deux jeux de champs adresse coexistent :

- **Adresse "permanente"** : `CustomerAddress *string`, `CustomerLat/CustomerLng *float64`, `CustomerFloorNumber`, `CustomerDoorNumber`, `CustomerAdditionalAddress`, `CustomerZoneCode`.
- **Adresse "temporaire"** : `CustomerTemporaryAddress`, `CustomerTemporaryLat/Lng` (⚠ **typés `*string`**, pas `*float64` — incohérence avec l'adresse permanente, à convertir côté consommateur), `CustomerTemporaryDoorNumber/FloorNumber/AdditionalAddress`.
- Le **switch** entre les deux est porté par `Order` (et non `Customer`) : `UseCustomerTemporaryAddress bool` (`internal/models/create_order_models.go:37`, colonne `orders.use_customer_temporary_address`, lue/écrite dans `internal/modules/order_life_cycle/repository.go:1599-1605,1845-1856`).
- `CustomerAdditionalInfo *string` — texte libre, **le plus proche d'une note de livraison** (digicode, étage...) mais générique, pas dédié.
- `CustomerTel *string`, `CustomerTemporaryPhone/PhoneCode *string` (numéro masqué type plateforme de livraison).
- Autres : `CustomerBrand`, `CustomerBusinessName`, `CustomerBirthdate`, `AdvertisingConsent`, stats (`CustomerNbOrders`, `CustomerNbBookings`, `CustomerTotalSpent`, `MatchScore`).

### 1.3 Énumérations & statuts

```go
OrderTypeIn       = "IN"
OrderTypeTakeAway = "TAKE_AWAY"
OrderTypeDelivery = "DELIVERY"

FulfillmentTypeRestaurant = "DELIVERY_BY_RESTAURANT"  // tournée maison (delivery_session)
FulfillmentTypeDeliveroo  = "DELIVEROO"               // coursier Deliveroo

MerchantApprovalPendingApproval = "PENDING_APPROVAL"
```
*(`internal/models/orders_model.go:9-17`)*

Pas de `FulfillmentType` dédié pour Uber Eats : la livraison Uber Eats est gérée par le coursier Uber, hors `delivery_session`.

**`order.State` vs `order.BrandStatus`** — deux champs distincts à ne pas confondre :

- **`State`** : cycle de vie *interne*, piloté par les endpoints `/orders/{order_id}/...` (`SetDelivered`, `StartDelivery`, `SetReadyForDistribution`, `SetDistributedProducts`, etc. — module `order_life_cycle`).
- **`BrandStatus`** : champ "miroir" réutilisé par **trois mécanismes différents** :
  1. **Intégration Uber Eats** (`internal/webhook/ubereats/service/event_delivery_status.go`) : `SCHEDULED`, `EN_ROUTE_TO_PICKUP`, `ARRIVED_AT_PICKUP`, `EN_ROUTE_TO_DROPOFF`, `ARRIVED_AT_DROPOFF`, `FINISHED`, `COMPLETED`, `FAILED`.
  2. **Webhook Stripe** (`internal/webhook/stripe/repository.go:131-132`) : `PENDING_APPROVAL` (avec `merchant_approval='PENDING_APPROVAL'`).
  3. **`delivery_session` maison** (voir §2.3) : `READY_FOR_HANDOFF` → `EN_ROUTE_TO_DROPOFF` (démarrage tournée) → `DONE` (clôture), retour à `READY_FOR_HANDOFF` si annulation.
  
  ⚠ Une appli livreur qui lirait `BrandStatus` doit filtrer/ignorer les valeurs issues des deux premiers mécanismes.

### 1.4 Établissement (point de départ / pickup) — `[EXISTE]`

Disponible directement via le login (`UserLoginRow`, jointure `users → users_rights → merchant → merchant_parameters`, `internal/modules/users/repository.go:42-123`) :

- `MerchantLat`, `MerchantLng float64`
- `MerchantAddress string` = `CONCAT(street_number,' ',street,', ',zip_code,' ',city,', ',country)`
- `TimeZone string`
- `MerchantName`, `MerchantTel`

→ Pas d'appel supplémentaire nécessaire : une appli livreur a le point de départ de chaque tournée dès l'authentification.

### 1.5 Manques identifiés

- **`[ABSENT]` Identifiant public non-séquentiel** : `order_id` est une PK auto-incrémentée séquentielle, `order_num` un compteur d'affichage 1-99 non unique globalement. Si l'appli livreur doit exposer/partager une référence "commande" (lien de suivi client par ex.) sans révéler un ID de base de données séquentiel, il faudra soit accepter ce risque (faible mais réel), soit générer un token dédié.
- **`[ABSENT]` Champ "instructions de livraison" dédié** : ni `Order` ni `Customer` n'ont de champ spécifique (digicode, étage, "sonner chez X"...). Le candidat le plus proche est `Customer.CustomerAdditionalInfo`, mais il est générique et potentiellement déjà utilisé à d'autres fins.

---

## 2. Sessions de livraison (`delivery_session` / `delivery_session_order`)

### 2.1 Schéma reconstruit

**`delivery_session`**

| Colonne | Type déduit | Notes |
|---|---|---|
| `id` | PK auto-increment | |
| `user_id` | FK → `users` | livreur assigné |
| `merchant_id` | FK → `merchant` | |
| `start_date` | DATETIME | = `UTC_TIMESTAMP` à la création |
| `end_date` | DATETIME (nullable) | référencée dans une requête de nettoyage mais **jamais écrite** — voir §2.5 |
| `distance` | string | fournie par le client à la création, **non recalculée serveur** |
| `duration` | string | idem |
| `status` | string | valeurs observées : `'1'`, `'PENDING'`, `'FINISHED'`, `'CANCELED'`, `'DONE'` (+ `'-1'`/`'CLOSED'` référencées dans des gardes `NOT IN` mais jamais écrites) |

**`delivery_session_order`**

| Colonne | Notes |
|---|---|
| `delivery_session_id` | FK → `delivery_session.id` |
| `order_id` | FK → `orders.order_id` |
| `priority` | `int`, 0-based = ordre d'insertion = **ordre de la tournée** |

→ `priority` est mappé dans `Order.DeliveryPriority` lors de la reconstruction par `ordersFetcher.FetchAndBuildOrders`.

### 2.2 Endpoints existants — `[EXISTE]` mais **réservés au dispatcher**

| Méthode | Route | Handler | Permission |
|---|---|---|---|
| GET | `/delivery_sessions/pending` | `GetPendingDeliverySessions` | `user.ManageDelivery` |
| GET | `/delivery_sessions/{delivery_session_id}` | `GetDeliverySession` | `user.ManageDelivery` |
| POST | `/delivery_sessions/start` | `StartDeliverySession` | `user.ManageDelivery` |
| PATCH | `/delivery_sessions/{delivery_session_id}/close` | `CloseDeliverySession` | `user.ManageDelivery` |
| DELETE | `/delivery_sessions/{delivery_session_id}` | `CancelDeliverySession` (= **annulation**, pas suppression) | `user.ManageDelivery` |

Toutes sous `authMiddleware` ; la permission est vérifiée **dans le service** via `middleware.UserFromContext(ctx)` + `if !user.ManageDelivery { return models.ErrForbidden }` — pas via `middleware.RequirePermission` au niveau routes.

### 2.3 Flux détaillés

**`POST /delivery_sessions/start`** — body `DeliverySessionRequest{merchant_id, distance, duration, delivery_man:{user_id}, orders:[{order_id}, ...]}`

1. Auto-clôture des sessions du livreur où `status IN ('1','PENDING') AND end_date < UTC_TIMESTAMP` → `status='FINISHED'` (**inopérant**, voir §2.5).
2. Refuse si une session `status IN ('1','PENDING')` existe déjà pour ce livreur → `ErrDeliverySessionAlreadyActive` (= **1 session active à la fois par livreur**, déjà appliqué côté serveur).
3. `INSERT delivery_session` (`status='PENDING'`, `start_date=UTC_TIMESTAMP`, `distance`/`duration` tels que fournis par le client).
4. `INSERT delivery_session_order` pour chaque commande, `priority` = index 0-based dans le tableau `orders` reçu.
5. `UPDATE orders SET brand_status='EN_ROUTE_TO_DROPOFF'` pour toutes les commandes de la session.
6. Notification merchant-wide `"UPDATE_DELIVERY_SESSION"` (WS + push).

**`DELETE /delivery_sessions/{id}`** (annulation)

1. `UPDATE orders SET brand_status='READY_FOR_HANDOFF' WHERE ... AND brand_status='EN_ROUTE_TO_DROPOFF'` (rollback du statut).
2. `UPDATE delivery_session SET status='CANCELED' WHERE id=? AND status >= 0` (comparaison varchar/int qui fonctionne par coercion implicite MySQL).
3. Notification. `CANCELED` ∉ `('1','PENDING')` → le livreur est libéré et peut redémarrer une session.

**`PATCH /delivery_sessions/{id}/close`**

1. `UPDATE orders SET brand_status='DONE', state='CLOSED' WHERE ... AND ds.status NOT IN ('-1','CLOSED','CANCELED')`.
2. `UPDATE delivery_session SET status='DONE'`.
3. **`UPDATE payments SET user_id = delivery_session.user_id`** pour toutes les commandes de la session → **réattribution rétroactive des encaissements au livreur** (clé pour la réconciliation de caisse en fin de tournée).
4. Notification, puis retourne la session complète via `GetDeliverySession`.

**`GET /delivery_sessions/pending`** — sessions `status IN ('1','PENDING')` du merchant, avec position/infos livreur lues depuis **`users`** (pas `user_status_view`, voir §4.2) et commandes associées (via `ordersFetcher`, filtré sur les `order_id` de `delivery_session_order`).

**`GET /delivery_sessions/{id}`** — une session avec position+statut **live** du livreur (via `user_status_view`).

### 2.4 Manques pour une appli livreur — `[ABSENT]`

- **Aucun endpoint accessible à un livreur** (`AccessDelivery`) pour consulter SA session active, SES arrêts ordonnés, ou marquer un arrêt comme livré. Tout est verrouillé derrière `ManageDelivery` (dispatcher/back-office).
- **Pas de statut par-commande** dans `delivery_session_order` — uniquement `priority` (ordre). Impossible de savoir quels arrêts d'une tournée multi-commandes sont déjà livrés sans regarder `order.state`/`brand_status` individuellement (mis à jour en masse uniquement à la clôture totale de la session).
- **Pas de notion de "prochain arrêt" / "arrêt courant"**.
- ✅ **Déjà acquis** : la règle "1 session active par livreur à la fois" est appliquée côté serveur et réutilisable telle quelle.

### 2.5 Dette technique — `[DETTE TECHNIQUE]`

- **`DeliverySession` dupliquée à l'identique** : `internal/models/delivery_sessions_models.go:18` et `internal/modules/delivery_sessions/models.go:20` (la seconde semble morte — `repository.go` retourne `models.DeliverySession`).
- **`DeliverySessionRequest` dupliquée de la même façon** (mêmes deux fichiers, lignes 3 et 5).
- **`delivery_session.end_date` n'est jamais renseigné** (ni à l'`INSERT`, ni au `close`/`cancel`) → la requête de nettoyage des sessions périmées dans `StartDeliverySession` (`end_date < UTC_TIMESTAMP`) ne matche jamais (`NULL < x` = `NULL` en SQL).
- **`status` mélange valeurs numériques-en-chaîne historiques** (`'1'`, `'-1'`) et valeurs textuelles (`PENDING`/`DONE`/`CANCELED`/`FINISHED`/`CLOSED`) — `CancelDeliverySession` s'appuie sur la coercion implicite `'PENDING' >= 0` → `0 >= 0` = vrai. Fragile.

---

## 3. Utilisateurs / Livreurs

### 3.1 Rôles & permissions

- **`AccessDelivery`** (rôle "livreur") : colonne `users_rights.access_wrdelivery` → `UserLoginRow.AccessDelivery` → `middleware.HasAccessDelivery` (`internal/middleware/permissions.go:24`) : `user.HasAccessDelivery() || user.IsAdmin()`. **C'est le flag qui qualifierait un compte pour une appli livreur.**
- **`ManageDelivery`** (capacité "gérer les tournées") : `merchant_parameters.manage_delivery` → `UserLoginRow.ManageDelivery`. **C'est le flag réellement vérifié par tous les endpoints `/delivery_sessions/*` aujourd'hui.**

> ⚠️ **`[À ARBITRER]`** Avec le modèle actuel, un compte "livreur pur" (`AccessDelivery=true`, `ManageDelivery=false`) **n'a accès à aucun endpoint `/delivery_sessions/*`**. Pour une appli mobile livreur, deux options :
> - **(a)** accorder `ManageDelivery` aux comptes livreurs — mais `ManageDelivery` donne accès à **toutes** les sessions du merchant (`GetPendingDeliverySessions`/`GetDeliverySession` ne filtrent pas par `user_id` du token) ;
> - **(b)** créer de nouveaux endpoints "self-scoped" (ex. `/delivery_sessions/me`) gardés par `AccessDelivery` + filtrage `WHERE ds.user_id = <user du token>` — **recommandé**.

### 3.2 Authentification — `[EXISTE]`

- **Token opaque + Redis** : token extrait via `helpers.ExtractToken`, résolu par `UsersRepository.GetUserByToken` (jointure `users`/`users_rights`/`merchant`/`merchant_parameters`/...), exposé aux services via `middleware.UserFromContext`. Scoping multi-tenant = `user.MerchantID`.
- **PIN** (migration `031_add_pin_hash_to_users_rights.up.sql`) :
  - `users_rights.pin_hash VARCHAR(64) NULL`, hash **HMAC-SHA256** (`security.HashPIN`).
  - Index unique `(merchant_id, pin_hash)` — un PIN est unique par merchant (NULLs multiples autorisés).
  - Routes (toutes sous `/auth`) :
    - `POST /auth/pin` (`authMiddleware` requis) → `authH.AuthPIN` — ré-authentifie/identifie un utilisateur du même merchant via PIN. **Nécessite déjà un token valide** : ce n'est donc **pas** un login "from scratch", plutôt un changement rapide d'employé actif sur device partagé — sémantique exacte à confirmer dans `internal/modules/auth`.
    - `POST /auth/pin/set` (`authMiddleware`) → `authH.SetPIN` — définit son propre PIN.
    - `POST /auth/pin/reset` (`authMiddleware` + `HasUserManagementAccess`) → `authH.ResetPIN` — un admin réinitialise le PIN d'un autre utilisateur.
  - Pertinent pour une appli livreur sur tablette partagée (un livreur "se connecte" rapidement par PIN sans ressaisir son mot de passe).

---

## 4. Géolocalisation (positions) — `[EXISTE]`

### 4.1 Endpoints

| Méthode | Route | Handler | Effet |
|---|---|---|---|
| PATCH | `/users/location` | `SetUserLocation` | body `{lat, lng}` — le `user_id` du body est **ignoré**, écrasé par celui du token (pas de spoofing possible) → `UPDATE user_status_view SET lat=?, lng=? WHERE user_id=?` |
| GET | `/users/{user_id}/location` | `GetUserLocation` | retourne `{user_id, first_name, last_name, lat, lng, status}`, scopé par `merchant_id` + `users_rights.enabled=TRUE` |

Les deux sous `authMiddleware`, **sans permission supplémentaire** : n'importe quel utilisateur authentifié peut lire la position de n'importe quel collègue de son merchant et écrire la sienne.

### 4.2 `user_status_view` — `[À VÉRIFIER EN BASE]`

- Le nom suggère une **vue SQL**, mais elle reçoit des `UPDATE` directs (`SetUserLocation`) → soit une vue *updatable* mono-table, soit une table régulière au nom trompeur. À clarifier via `SHOW CREATE TABLE/VIEW user_status_view` avant de construire dessus.
- Colonnes connues par l'usage : `user_id, first_name, last_name, lat, lng, status` (+ probablement `profile_picture`, `planning_color`).
- `status` **n'est écrit nulle part** dans le code Go consulté → soit calculé par la vue (ex. en ligne/hors ligne selon un timestamp d'activité), soit maintenu par un autre processus. À documenter avant de l'exposer côté livreur.
- ⚠️ Noter que `delivery_sessions.fetchDeliverySessions` (§2.1, `GET /delivery_sessions/pending`) lit `lat, lng, planning_color, profile_picture` depuis **`users`** directement, **pas** `user_status_view` — deux chemins de lecture de position coexistent.

### 4.3 Consommateurs existants

- `pos.GetDeliveryMen(merchantID)` (`GET /pos/delivery_men`, `GET /pos/users`) — liste tous les `users_rights.enabled=TRUE` du merchant avec position+statut. ⚠️ **ne filtre pas sur `access_wrdelivery`** malgré son nom — peut retourner du personnel non-livreur.
- `delivery_sessions.GetDeliverySession` — position+statut live du livreur d'une session.

### 4.4 Manques — `[ABSENT]`

- **Historique de position** (uniquement la position courante, écrasée à chaque update).
- **Aucune validation "session active requise"** avant d'accepter une mise à jour de position — `PATCH /users/location` fonctionne pour n'importe quel utilisateur authentifié à tout moment.
- **Filtrage par `access_wrdelivery`** dans `GetDeliveryMen`.

### 4.5 Dette technique

- **`DeliveryMan` dupliquée à l'identique** : `internal/models/pos_models.go:33` et `internal/modules/pos/models.go:39`.

---

## 5. WebSocket & Notifications push (FCM)

- `Hub.BroadcastToMerchant(merchantID string, message []byte) bool` (`internal/infrastructure/websocket/hub.go:75`) — diffuse à **tous** les clients WS connectés pour un merchant (pas de ciblage par utilisateur).
- Route `GET /ws` (`cmd/api/routes.go:1091`) → `websocket.ServeWS(wsHub, w, r)`.
- `notification.NotificationService` (`internal/modules/notification/notification_service.go`) :
  - `SendNotificationAsync(merchantID, entityID, type string) error` — diffuse un message WS minimal `{type, entity_id, merchant_id}` à tout le merchant.
  - `SendNotificationAsyncWithPayload(merchantID, type, entityID string, payload map[string]interface{}) error` — variante avec payload custom + lookup des device tokens FCM (`s.repo.GetDeviceTokens(merchantID)`) pour le fallback push.
- **`[À VÉRIFIER]`** `GetDeviceTokens` : scope actuel = merchant entier ou par utilisateur ? `UserLoginRow.DeliveryDeviceToken` (device token "livraison" par utilisateur) existe déjà — bonne base pour cibler un livreur individuellement en push, mais le `Hub` WS reste merchant-wide.

---

## 6. SMS (Brevo) — `[EXISTE]`

- `BrevoSMS.SendSMSAsync(senderID, phoneNumber, message string)` (`internal/infrastructure/brevo_sms/service.go:36`) — envoi générique, asynchrone (goroutine fire-and-forget). **Réutilisable tel quel** pour notifier un client ("votre livreur arrive").
- `BrevoSMS.SendOTP(tel, otp string)` (`:115`) — code de vérification.
- `BrevoSMS.SendOrderConfirmationSMS(senderID, phoneNumber string, data sms.OrderConfirmationSMSData)` (`:48`) — gabarit de confirmation avec lien de suivi (`TrackingURL`) — **modèle direct** pour un futur SMS "lien de suivi livreur en temps réel".

---

## 7. Paiement / NF525 / TPE / Avoirs

### 7.1 Chaîne de paiement NF525 — `[EXISTE]`

`AddPaymentAndReturnID(ctx, payment models.Payment) (int64, error)` (`internal/modules/order_life_cycle/repository.go:90`) :

1. Vérifie `SUM(payments.amount) + nouveau montant <= orders.price` (sinon `OrderNotFullyPaidError`).
2. `SELECT hash FROM payments WHERE merchant_id=? ORDER BY payment_date DESC LIMIT 1 FOR UPDATE` → `prevHash` (verrou pessimiste pour le chaînage).
3. `newHash = SHA256(prevHash | paymentDate(RFC3339) | amount | mop | orderID)`, `signature = security.SignHash(newHash)`.
4. `INSERT INTO payments(merchant_id, cash_register_id, order_id, amount, mop, comment, payment_date, user_id, status_check, previous_hash, hash, signature, operation_type)`.
5. Effets de bord : `mop=TicketResto` → `INSERT INTO restaurant_ticket(merchant_id, payment_id, barcode)` ; `mop=Stripe` → `INSERT INTO stripe_payments(...)` ; recalcul de `orders.isPaid = (price <= SUM(payments.amount))`.
6. `AddPayment` = wrapper qui ignore l'ID retourné (compat ascendante).

### 7.2 Modèle `Payment` (`internal/models/payment_models.go:10-29`)

```go
OrderID, CashRegisterID, PaymentID, MOP string
Amount int
PaymentDate int64
MerchantID, UserID string
Enabled bool
IntentID, AccountID *string
OperationType string // "SALE" ou "REFUND"
Comment, StatusCheck, Code *string
PaymentIntentID, CheckoutSessionID, CustomerEmail *string
```

### 7.3 Remboursements / Avoirs — `[EXISTE]`

- `StripeManager.RefundOrCancelAsync` / `RefundAsync` (`internal/infrastructure/stripe/service.go:53,164`) — appels Stripe asynchrones.
- `StripeWebhookService.HandleRefund` (`internal/webhook/stripe/service.go:306`) — synchronise le remboursement reçu côté webhook.
- `OrdersLifeCycleHandler.HandleRefund` → `OrdersLifeCycleService.ProcessRefund(ctx, models.RefundRequest)` (`internal/modules/order_life_cycle/handler.go:321`, `service.go:859`) — point d'entrée HTTP back-office.
- **Module ticket/avoir : `internal/modules/receipt/service.go`** — `GenerateFiscalReceipt`, `GenerateRefundReceipt` (= avoir), `GetReceiptByOrderID`, `generateNextReceiptNumber`. Service interne instancié dans `routes.go` (~241-263) et consommé par `order_life_cycle` ; **aucune route HTTP dédiée** trouvée pour exposer un reçu/avoir directement (ex. `GET /orders/{id}/receipt`).

### 7.4 TPE (terminal de paiement physique) — `[ABSENT]`

Aucune intégration Stripe Terminal (ou autre) pilotant un terminal de paiement physique. L'enregistrement d'un paiement carte se fait manuellement via `AddPaymentAndReturnID` (`mop=carte`), sans pilotage matériel. Si l'appli livreur doit déclencher un paiement CB sans contact sur le terrain, cette brique est **entièrement à construire** (ou intégrer un SDK Stripe Terminal côté mobile + appeler `AddPayment` une fois la transaction confirmée).

### 7.5 Implications pour le paiement à la livraison (COD)

- `AddPaymentAndReturnID(mop=cash, user_id=<livreur>)` est **directement utilisable** pour un encaissement espèces/carte par le livreur lui-même (sous réserve de lui exposer un endpoint protégé par `AccessDelivery`).
- Alternative déjà existante : encaisser au moment de la commande (peu importe qui), puis `CloseDeliverySession` réattribue automatiquement `payments.user_id` au livreur — utile pour la réconciliation de caisse même sans accès direct au paiement pour le livreur.

---

## 8. Routing & cache géographique

- `GET /external/routes` (`authMiddleware`) → `googlemaps.RouteHandler.HandleGetRoute` (`internal/modules/googlemaps/{client,handler,models,repository,service}.go`, `routes.go:445-449`) — **proxy transparent** vers l'API Google Routes, clé API = `cfg.Google.APIKey`. Pas de cache, pas de transformation de la réponse. `[EXISTE]`
- `[ABSENT]` OSRM ou tout moteur d'itinéraire auto-hébergé/alternatif.
- `[ABSENT]` Cache de géocodage ou de géométrie de route — chaque appel retape l'API Google (coût + latence).
- **Pattern de cache Redis directement réutilisable** : `internal/ai/cache.Cache` (`Get/Set/Delete/DeleteByPattern` avec TTL, `internal/ai/cache/redis.go`) — conçu pour le cache de réponses LLM mais générique. Un cache `route:{origin}:{destination}` ou `geocode:{address_hash}` avec TTL suivrait exactement le même pattern.

---

## 9. Récapitulatif — Réutiliser / Adapter / Créer

| Besoin (appli livreur) | Statut | Action proposée |
|---|---|---|
| Login livreur (tablette partagée) | `[EXISTE]` PIN, `/auth/pin*` | Réutiliser tel quel |
| Identifier un compte comme "livreur" | `[EXISTE]` `AccessDelivery` / `access_wrdelivery` | Réutiliser |
| "Ma session active" (livreur) | `[ABSENT]` | Créer `GET /delivery_sessions/me`, scopé `user_id` du token, gardé par `AccessDelivery` |
| Liste ordonnée des arrêts d'une session | `[PARTIEL]` `delivery_session_order.priority` + `Order.DeliveryPriority` existent | Réutiliser le tri ; adapter la sérialisation pour l'appli livreur |
| Statut par arrêt (en route / arrivé / livré) | `[ABSENT]` | Créer colonne(s) sur `delivery_session_order` (`status`, `arrived_at`, `delivered_at`) — migration **032** |
| Marquer un arrêt comme livré | `[PARTIEL]` `order.state` via `/orders/{id}/delivered` existe au niveau commande | Adapter pour mettre à jour aussi `delivery_session_order` |
| Position du livreur (lecture/écriture) | `[EXISTE]` `PATCH`/`GET /users/.../location`, `user_status_view` | Réutiliser ; clarifier nature de `user_status_view` (§4.2) |
| Historique de position | `[ABSENT]` | Créer table `user_position_history` (migration 032+) si besoin de tracking/replay |
| Notifier le merchant (nouvelle tournée, etc.) | `[EXISTE]` `NotificationService.SendNotificationAsync` + `Hub` | Réutiliser |
| Notifier UN livreur précis (push) | `[PARTIEL]` `delivery_device_token` existe par user, mais `SendNotificationAsync` est merchant-wide | Adapter : nouvelle fonction `SendNotificationToUser` |
| SMS client "livreur en route" | `[EXISTE]` `BrevoSMS.SendSMSAsync` / gabarit `OrderConfirmationSMS` | Réutiliser/adapter le gabarit |
| Itinéraire / distance / durée | `[EXISTE]` proxy `/external/routes` | Réutiliser |
| Cache géocodage/itinéraire | `[ABSENT]` | Créer en s'inspirant de `internal/ai/cache.Cache` |
| Encaissement à la livraison (espèces/CB) | `[EXISTE]` `AddPaymentAndReturnID`, NF525 | Réutiliser ; exposer un endpoint scopé livreur |
| Pilotage TPE physique | `[ABSENT]` | À construire entièrement (hors scope si paiement déjà fait en amont) |
| Adresse de livraison + instructions | `[PARTIEL]` adresse + lat/lng existent, pas de champ "instructions" dédié | Adapter : ajouter `delivery_instructions` sur `orders` (migration 032), ou réutiliser `CustomerAdditionalInfo` |
| ID public de commande pour le client | `[PARTIEL]` `order_id` séquentiel existe, pas d'ID opaque | À arbitrer (§11) |

---

## 10. Conventions pour les nouveaux développements

- **Modules** : `handler.go` / `service.go` / `repository.go` / `models.go`, DI par constructeur, branchement dans `cmd/api/routes.go` (voir `delivery_sessions`, `users`, `order_life_cycle` comme modèles directement audités ici).
- **Permissions** : deux patterns coexistent —
  - `middleware.RequirePermission(middleware.HasXxx)` au niveau route (ex. `/auth/pin/reset`) ;
  - vérification manuelle dans le service via `middleware.UserFromContext(ctx)` + `if !user.XxxFlag { return models.ErrForbidden }` (ex. tout `delivery_sessions`).
  
  Pour un nouveau module "driver", le premier pattern (déclaratif, au niveau route) est préférable pour la lisibilité, sauf si le filtrage dépend de données chargées dynamiquement (ex. "cette session m'appartient-elle ?").
- **Migrations** : `001`-`025` = fichier `.sql` unique ; `026`+ = paire `.up.sql`/`.down.sql` (`NNN_description.up.sql` / `.down.sql`). **Prochain numéro disponible : `032`**.
- **Tables historiques** (`orders`, `customer`, `delivery_session`, `delivery_session_order`, `users`, `users_rights`, `merchant`, `merchant_parameters`, `payments`, `receipts`, `user_status_view`, ...) : pas de `CREATE TABLE` dans `migrations/` — toute évolution doit passer par une migration `ALTER TABLE` (cf. `031_add_pin_hash_to_users_rights.up.sql` comme modèle).

---

## 11. Questions ouvertes / décisions à arbitrer

1. **Modèle de permission livreur** : étendre `ManageDelivery` aux comptes `AccessDelivery`, ou créer des endpoints "self-scoped" séparés (recommandé, §3.1) ?
2. **`user_status_view`** : vue ou table ? Quelles colonnes exactes, qui maintient `status` ?
3. Faut-il un **statut par arrêt** dans une tournée multi-commandes, ou le modèle "1 commande = 1 livraison = 1 session" reste-t-il la norme (auquel cas `delivery_session_order.priority` suffit) ?
4. **`/auth/pin`** : sert-il à changer d'employé actif sur device partagé, ou à un login PIN complet sans token préalable ? (la route actuelle exige `authMiddleware`, donc ce n'est **pas** un login "from scratch" en l'état)
5. Faut-il un **identifiant de commande non-séquentiel** pour l'affichage client (lien de suivi, etc.) ?
6. Champ **"instructions de livraison"** : nouveau champ dédié, ou réutilisation de `Customer.CustomerAdditionalInfo` ?
7. **Paiement à la livraison** : le livreur encaisse-t-il lui-même (`AddPayment` exposé), ou la réattribution a posteriori via `CloseDeliverySession` suffit-elle ?

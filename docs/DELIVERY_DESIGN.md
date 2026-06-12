# Conception — Module Livraison (API livreur)

> Document de conception. Référence l'audit [`docs/DELIVERY_API_AUDIT.md`](DELIVERY_API_AUDIT.md) (vérité terrain de l'existant). **Aucune implémentation Go** dans cette étape — uniquement ce document + les migrations `032`/`033`.

---

## 0. Points de vigilance / contradictions

### 0.1 `users.heading` — *(corrigé)* la colonne existe déjà

Une première version de ce document affirmait, sur la base d'un `grep` Go négatif, que `heading`
n'existait nulle part. **C'est faux** : la définition de `user_status_view` sélectionne `u.heading`
depuis `users` — une vue MySQL ne pouvant référencer une colonne inexistante, **`users.heading`
existe déjà en base**. Le `grep` Go montrait seulement qu'**aucun code applicatif ne l'écrit/le lit
encore**.

➡️ **Conséquence** : la migration `032` **n'ajoute PAS** `users.heading` (sinon `Duplicate column
name 'heading'`). Elle ajoute uniquement `users.last_position_at`.

### 0.2 Écriture de la position : `SetUserLocation` migrera vers `users` directement

`SetUserLocation` (`internal/modules/users/repository.go:22-35`) écrit aujourd'hui :
```sql
UPDATE user_status_view SET lat = ?, lng = ? WHERE user_id = ?
```
La conception **spécifie** (sans l'implémenter ici) que cet endpoint écrira désormais
**directement sur la table de base** :
```sql
UPDATE users SET lat = ?, lng = ?, heading = ?, last_position_at = ? WHERE user_id = ?
```
`user_status_view` reste un chemin de **lecture** (elle expose déjà `heading`, cf. §0.1) ; cela
évite toute question d'updatabilité d'une vue potentiellement multi-table (l'audit §4.2 ne tranche
pas si `user_status_view` est une vue mono-table updatable ou une table distincte — écrire sur
`users` directement contourne complètement cette ambiguïté). `accuracy`/`speed` ne sont pas
persistés sur `users` (non demandés sur cette table) — uniquement dans `delivery_position` (§3.7
effet 2).

### 0.3 `delivery_session.status` peut déjà valoir `'0'` (auto-clôture existante)

Toujours d'actualité (non remis en cause par cette passe). `SetDeliveredLocal`
(`internal/modules/order_life_cycle/repository.go:725-840`, appelé par `PATCH
/orders/{order_id}/delivered`) bascule **automatiquement** `delivery_session.status = '0'`
(chaîne, pas `'DONE'`) quand la commande livrée était la dernière `state='OPEN'` de sa tournée.
`delivery_session.status` a donc **6 valeurs réellement atteignables** : `'1'`, `'PENDING'`
(actif), `'CANCELED'`, `'DONE'` (close manuelle dispatcher), `'0'` (auto-clôture), `'FINISHED'`
(mort) — + `'-1'`/`'CLOSED'` référencées mais jamais écrites. Voir §2 et §7.

### 0.4 Convention de nommage des statuts (cette passe)

| Colonne | Convention | Valeurs |
|---|---|---|
| `delivery_session_order.status` (**nouvelle colonne**, migration 032) | snake_case | `pending`, `en_route`, `arrived`, `delivered`, `failed`, `canceled` |
| `orders.brand_status` (existante, partagée avec les intégrations) | UPPER (inchangé) | nouvelle valeur `DELIVERY_FAILED` (en plus des valeurs existantes type `EN_ROUTE_TO_DROPOFF`, `CLOSED`, `CANCELED`, `FAILED`...) |
| `delivery_session.status` (existante) | **legacy, non touchée par cette passe** — cf. §0.3/§7 | `'1'`/`'PENDING'`/`'0'`/`'DONE'`/`'CANCELED'`/`'FINISHED'` |

Deux conventions volontairement différentes coexistent : `brand_status` reste UPPER (colonne
partagée avec les intégrations, valeurs établies type `EN_ROUTE_TO_DROPOFF`/`FAILED`) — `FAILED`
étant déjà réservé au vocabulaire Uber Eats, le module livraison utilise `DELIVERY_FAILED` pour
éviter toute collision sémantique. `delivery_session_order.status`, en tant que **colonne
entièrement nouvelle**, adopte directement le nouveau snake_case (pas de données legacy à migrer).
`delivery_session.status`, lui, **reste en l'état** dans cette passe — sa normalisation vers ce
même snake_case (`active`/`done`/`canceled`) est spécifiée séparément en §7.

### 0.5 ⚠️ Risque d'ordonnancement : `/delivery_sessions/me/close` écrit `status='done'`

Le nouvel endpoint `/delivery_sessions/me/close` (§3.8) écrit `delivery_session.status='done'`
(nouvelle convention snake_case, §0.4). Or §0.3/§2.1 recensent déjà **6 valeurs legacy**
distinctes pour cette même colonne, et §7 (normalisation, **non faite dans cette passe**) prévoit
de migrer ces 6 valeurs vers `active`/`done`/`canceled`.

**Tant que la normalisation §7 n'a pas été appliquée**, `'done'` constituerait une **7ᵉ valeur
distincte** de `delivery_session.status`, aggravant la dette déjà documentée (audit §2.5).

➡️ **Signalé plutôt que tranché** : `/delivery_sessions/me/close` est ici **spécifié** (pas
implémenté). Au moment de son implémentation effective, deux options :
- (a) l'implémenter **après** (ou dans la même release que) l'étape *Migrate* de §7, pour que
  `'done'` soit la seule et unique valeur de clôture restante ;
- (b) si `/me/close` doit sortir **avant** §7, harmoniser temporairement cette écriture sur
  `'DONE'` (legacy, déjà utilisé par `CloseDeliverySession`) en attendant la normalisation, puis la
  faire évoluer vers `'done'` lors de l'étape *Contract*.

Ce choix relève de la planification de release, pas de ce document de conception.

---

## 1. FSM par arrêt — `delivery_session_order.status`

### 1.1 États

| État | Sens | Terminal ? |
|---|---|---|
| `pending` | Arrêt pas encore pris en charge | non |
| `en_route` | Arrêt sélectionné comme **arrêt courant** (`delivery_session.current_order_id`), livreur en route | non |
| `arrived` | Livreur arrivé sur place (manuel ou geofence) | non |
| `delivered` | Livré avec succès | **oui** |
| `failed` | Échec de livraison (motif dans `fail_reason`) — commande **re-dispatchable** | **oui** |
| `canceled` | Commande annulée pendant la tournée (ex. établissement fermé) | **oui** |

Valeur par défaut (migration 032) : `pending` pour toute nouvelle ligne `delivery_session_order`.

### 1.2 Diagramme des transitions

```
                  (sélection livreur, "désordre permis")
   ┌─────────┐ ──────────────────────────────────────► ┌──────────┐
   │ pending │                                          │ en_route │
   └─────────┘ ◄────────────(ré-sélection              └──────────┘
        │ │       d'un AUTRE arrêt comme courant)         │   │
        │ │                                                │   │ arrivée
        │ │                                                │   │ (manuel ou geofence)
        │ │                                                │   ▼
        │ │                                                │ ┌──────────┐
        │ │                                                │ │ arrived  │
        │ │                                                │ └──────────┘
        │ │                                       échec    │   │ confirmation
        │ │                                    (sans être  │   │ (paiement OK
        │ │                                      arrivé)   │   │  via /delivered)
        │ ▼                                                ▼   ▼
        │ ┌──────────┐  ◄──────────────────────────────────── ┌────────────┐
        │ │  failed  │                                         │ delivered  │
        │ └──────────┘                                         └────────────┘
        │  (terminal, re-dispatchable)                          (terminal)
        │
        │ annulation (établissement fermé, etc.)
        ▼
   ┌──────────┐
   │ canceled │  (terminal — depuis pending / en_route / arrived)
   └──────────┘
```

### 1.3 Table des transitions

| # | Transition | Déclencheur | Pré-condition | Effets sur `delivery_session_order` | Effets sur `orders` / `payments` | `current_order_id` | Événement WS (`type`, `entity_id`) |
|---|---|---|---|---|---|---|---|
| **1** | `pending → en_route`<br>(sélection de l'arrêt courant) | **Livreur** (`PATCH /delivery_sessions/me/stops/{order_id}/select`) | Session active (`status IN ('1','PENDING')`), `order_id` appartient à la session, stop visé `status='pending'`. Si un autre arrêt est `en_route`/`arrived`, il est repassé à `pending` (cf. col. 5). | Stop ciblé → `status='en_route'`. Si un autre stop était `en_route`/`arrived` (≠ ce stop) : → `status='pending'`, `arrived_at=NULL`. | aucun | `current_order_id = order_id` | `UPDATE_DELIVERY_SESSION` / `delivery_session_id` *(réutilisé)* |
| **2** | `en_route → arrived` | **Livreur** (`PATCH .../stops/{order_id}/arrived`) **ou Système (geofence)** déclenché lors d'une mise à jour de position (`PATCH /users/location`, §3.7) | `order_id == current_order_id`, stop `status='en_route'`. Pour le geofence : position reçue ≤ `GEOFENCE_RADIUS_M` (constante à définir, ex. 100 m) de l'adresse de livraison. | `status='arrived'`, `arrived_at = UTC_TIMESTAMP()` | aucun (`orders.brand_status` reste `EN_ROUTE_TO_DROPOFF`) | inchangé | `UPDATE_DELIVERY_SESSION` / `delivery_session_id` *(réutilisé)* |
| **3** | `en_route` ou `arrived` `→ delivered` | **Livreur** (`PATCH .../stops/{order_id}/delivered`) | `order_id == current_order_id`, stop `status IN ('en_route','arrived')`. Paiement déjà complet (sinon `OrderNotFullyPaidError` 409 — utiliser d'abord `POST /orders/{order_id}/payments/create`, existant). | `status='delivered'`, `delivered_at = UTC_TIMESTAMP()` | **Réutilise `OrdersLifeCycleService.SetDelivered`** (`order_life_cycle/service.go:283` → `SetDeliveredLocal`) : `orders.state='CLOSED'`, `brand_status='CLOSED'`, `isPaid=1`, `isDistributed=1`, `delivered_on=now`, hash NF525. **Effet de bord existant** : si dernière commande `state='OPEN'` de la session → `delivery_session.status='0'` (auto-clôture, §0.3). **Nouveau** : `UPDATE payments SET user_id=<livreur> WHERE order_id=<cette commande>` (réassignation immédiate). | avance vers le prochain stop `status='pending'` trié par `priority ASC` → passe à `en_route` ; sinon `current_order_id=NULL` | `UPDATE_ORDER` / `order_id` *(auto via `SetDelivered`)* **+** `UPDATE_DELIVERY_SESSION` / `delivery_session_id` *(réutilisé)* |
| **4** | tout état non-terminal `→ failed` | **Livreur** (`PATCH .../stops/{order_id}/failed`, body `{"reason": "..."}`) | `order_id == current_order_id`, stop `status NOT IN ('delivered','failed','canceled')`. `reason` requis (≤ 255 car.). | `status='failed'`, `failed_at=UTC_TIMESTAMP()`, `fail_reason=<reason>` | `orders.brand_status='DELIVERY_FAILED'` *(nouvelle valeur, §0.4 — pas `'FAILED'`, réservé Uber Eats)*. `orders.state` **inchangé** (commande **re-dispatchable** : nouvelle tournée, ou triage dispatcher via `/orders/{id}/refund`/`/orders/{id}/cancel`). | avance vers le prochain `pending` (même logique que #3) ; sinon `current_order_id=NULL` | `UPDATE_DELIVERY_SESSION` + `UPDATE_ORDER` *(réutilisés — `brand_status` a changé)* |
| **5** | `pending`/`en_route`/`arrived` `→ canceled` | **Livreur** (`PATCH .../stops/{order_id}/cancel`, body `{"reason_id": "...", "comment": "..."}`) | `order_id == current_order_id`, stop `status NOT IN ('delivered','failed','canceled')`. | `status='canceled'`, `canceled_at=UTC_TIMESTAMP()` | **`[A VERIFIER]`** — deux chemins existants candidats pour "annuler la commande" coté `orders`, à trancher à l'implémentation : <br>**(a)** `PATCH /orders/{order_id}/cancel` (`DeleteOrder`/`SetOrderDeleted` → `DeleteOrderLocal`, `order_life_cycle/repository.go:677-723`, perm `IsEmailVerified` — donc accessible au livreur) : `state='CLOSED'`, `brand_status='CANCELED'`, hash NF525, `DisablePayments` (**aucun remboursement réel** — adapté si rien n'a encore été encaissé, ex. COD pas encore pris). <br>**(b)** `POST /orders/{order_id}/refund` (`ProcessRefund`, `order_life_cycle/service.go:859`, audit §7.3) : remboursement réel (paiement négatif + avoir fiscal NF525) mais **nécessite `device_id`/registre de caisse actif** (`GetActiveCashRegisterID`) — notion absente du contexte "app livreur" — et ne touche ni `state` ni `brand_status`. <br>Le choix dépend probablement de `paidAmount` au moment de l'annulation (rien encaissé → (a) ; déjà encaissé → (b), potentiellement (a)+(b) combinés). | avance vers le prochain `pending` (même logique que #3) ; sinon `current_order_id=NULL` | `UPDATE_DELIVERY_SESSION` + `UPDATE_ORDER` |

### 1.4 État dégradé : "session active sans position fraîche"

Ce n'est **pas** une valeur de `delivery_session_order.status` ni de `delivery_session.status` —
c'est une **condition dérivée**, calculée à la lecture, qui sert d'alerte dispatcher :

```
session.status IN ('1','PENDING')
  AND (users.last_position_at IS NULL
       OR users.last_position_at < UTC_TIMESTAMP() - INTERVAL 5 MINUTE)
```

- Seuil proposé : **5 minutes** (constante `STALE_POSITION_THRESHOLD`, à définir lors de
  l'implémentation).
- Exposition : `[À ADAPTER]` — ajouter `last_position_at` (et/ou un booléen `position_stale`) à
  `models.OrderUser` / `DeliveryMan` dans les réponses **existantes** `GET /delivery_sessions/pending`
  et `GET /delivery_sessions/{id}` (aucune nouvelle route).
- N'affecte **pas** la transition geofence (#2) : celle-ci est évaluée sur la position
  **reçue à l'instant T**, fraîche par définition.

### 1.5 Transition de clôture de session — `/delivery_sessions/me/close`

Pas une valeur de `delivery_session_order.status` (les statuts par arrêt sont déjà figés à ce
stade) mais une transition **session-level** déclenchée par le livreur lui-même. Voir §1.3
(échec/annulation) pour le constat clé : **les transitions #4 (`failed`) et #5 (`canceled`,
chemin (a)) ne déclenchent PAS l'auto-clôture existante de §0.3** (celle-ci ne se déclenche que
depuis `SetDeliveredLocal`, donc uniquement via la transition #3). Si le **dernier** arrêt traité
par le livreur est `failed` ou `canceled` (pas `delivered`), `delivery_session.status` resterait
indéfiniment à `'1'`/`'PENDING'` sans `/me/close`. Détail complet en §3.8.

---

## 2. FSM de session — `delivery_session.status` et relation avec la FSM par arrêt

### 2.1 États existants (rappel)

| Valeur | Origine | Signification |
|---|---|---|
| `'1'` / `'PENDING'` | `StartDeliverySession` (création) | Session **active** — seules ces deux valeurs sont vérifiées par `StartDeliverySession` (1 session active/livreur), `GetPendingDeliverySessions`, `GetDeliverySessions`. Synonymes legacy/courant — `[DETTE TECHNIQUE]` (audit §2.5). |
| `'CANCELED'` | `CancelDeliverySession` (dispatcher, `DELETE /delivery_sessions/{id}`) | Annulée manuellement |
| `'DONE'` | `CloseDeliverySession` (dispatcher, `PATCH /delivery_sessions/{id}/close`) | Clôturée manuellement (avec réassignation paiements, audit §2.3) |
| `'0'` | **`SetDeliveredLocal`** (auto, via `PATCH /orders/{order_id}/delivered`) — §0.3 | Auto-clôturée car dernière commande `OPEN` livrée |
| `'FINISHED'` | `StartDeliverySession` (nettoyage) | **Mort** — condition jamais vraie (`end_date` jamais écrit, audit §2.5) |
| `'-1'` / `'CLOSED'` | gardes `NOT IN` de `CancelDeliverySession`/`CloseDeliverySession` | Référencées mais jamais écrites |
| `'done'` *(nouveau, §0.5)* | `/delivery_sessions/me/close` (§3.8, **spec seule**) | Clôturée par le livreur — **7ᵉ valeur potentielle**, cf. §0.5/§7 |

**Aucune valeur existante de `delivery_session.status` n'est renommée/migrée par cette passe**
(normalisation = §7, future, séparée).

### 2.2 Relation avec la FSM par arrêt

> "Session active tant qu'au moins un arrêt n'est pas terminal" se traduit, **avec le code
> existant + les ajouts de cette passe**, de la façon suivante :

- À la création (`StartDeliverySession`, inchangé), tous les `delivery_session_order.status =
  'pending'` (défaut colonne) et `delivery_session.current_order_id = NULL`.
- **Tous les arrêts terminés en `delivered`** : la transition #3 réutilise `SetDelivered`, dont
  l'effet de bord existant bascule `delivery_session.status='0'` dès que la **dernière** commande
  `state='OPEN'` est livrée → **auto-clôture déjà fonctionnelle, sans code nouveau**.
- **Au moins un arrêt `failed`** (transition #4, `orders.state` reste `'OPEN'`) : la condition
  `NOT EXISTS (... state='OPEN')` de `SetDeliveredLocal` reste fausse pour les commandes livrées en
  parallèle → **pas d'auto-clôture**.
- **Au moins un arrêt `canceled`** (transition #5) : selon le chemin choisi (§1.3 #5), `orders.state`
  peut devenir `'CLOSED'` (chemin a) sans pour autant déclencher l'auto-clôture (celle-ci n'est
  câblée que sur `SetDeliveredLocal`/transition #3).
- **Dans ces deux derniers cas — et plus généralement dès que le DERNIER arrêt traité n'est PAS un
  `delivered`** — l'auto-clôture existante ne se déclenche jamais. Le livreur appelle alors
  `/delivery_sessions/me/close` (§1.5/§3.8) une fois **tous** les arrêts terminaux
  (`delivered`/`failed`/`canceled`), ce qui pose `delivery_session.status='done'` (§0.5).
- Le dispatcher conserve en parallèle `PATCH /delivery_sessions/{id}/close` (existant, inchangé,
  `status='DONE'`) comme filet de sécurité manuel, quel que soit l'état des arrêts.

### 2.3 Recommandations Go futures (hors scope de cette phase)

1. `CloseDeliverySession` (`delivery_sessions/repository.go:358-420`) : ajouter `end_date =
   UTC_TIMESTAMP()` à l'`UPDATE delivery_session ... SET status='DONE'`.
2. `CancelDeliverySession` (`delivery_sessions/repository.go:307-356`) : ajouter `end_date =
   UTC_TIMESTAMP()` à l'`UPDATE delivery_session ... SET status='CANCELED'`.
3. `StartDeliverySession` (`delivery_sessions/repository.go:224-305`) : initialiser
   `current_order_id` au premier `order_id` (par `priority ASC`) et passer son
   `delivery_session_order.status` à `'en_route'` (les autres restent `'pending'`).
4. La normalisation complète de `delivery_session.status` (incluant l'harmonisation du
   `SET ds.status = 0` de `SetDeliveredLocal` vers `'done'`+`end_date`, et le sort de
   `/me/close`'s `'done'`) est spécifiée en **§7** — pas de mini-fix isolé ici pour éviter
   d'ajouter une 7ᵉ/8ᵉ valeur de plus (§0.5).

Tant que (3) n'est pas fait, `GET /delivery_sessions/me` (§3.1) doit gérer
`current_order_id IS NULL` en calculant dynamiquement le premier stop `pending` par
`priority ASC` comme "arrêt courant" de fait.

---

## 3. Contrats d'endpoints

Toutes les routes ci-dessous (sauf `/track/{ref}`, phase 2) sont sous `authMiddleware`, dans le
groupe `/delivery_sessions` existant (`cmd/api/routes.go:956-965`).

### 3.1 `GET /delivery_sessions/me` — **CRÉER**

Confort : résout la session active de l'appelant via le token, sans connaître son ID. **Pas de
garde de permission supplémentaire** (tout utilisateur authentifié peut interroger SA propre
session — symétrique à `PATCH /users/location`).

- **Permission** : `authMiddleware` uniquement.
- **Body** : aucun.
- **Logique** : `SELECT ... FROM delivery_session WHERE user_id=<user.UserID> AND
  merchant_id=<user.MerchantID> AND status IN ('1','PENDING') ORDER BY start_date DESC LIMIT 1`,
  puis assemble comme `GetDeliverySession` (`delivery_sessions/repository.go:422-514`).
- **Réponse 200** :
```json
{
  "status": "success",
  "delivery_session": {
    "delivery_session_id": "123",
    "user_id": "u_42",
    "merchant_id": "m_1",
    "status": "PENDING",
    "current_order_id": 456,
    "distance": "4.2",
    "duration": "12",
    "delivery_man": { "user_id": "u_42", "first_name": "...", "lat": 48.85, "lng": 2.35, "heading": 87.5 },
    "orders": [
      {
        "order_id": "456",
        "order_num": "27",
        "customer": { "...": "adresse, tel, delivery_notes (cf. §3.7)" },
        "delivery_stop": {
          "priority": 0,
          "status": "en_route",
          "arrived_at": null,
          "delivered_at": null,
          "failed_at": null,
          "canceled_at": null,
          "fail_reason": null
        }
      }
    ]
  }
}
```
  `delivery_stop.status` ∈ `pending|en_route|arrived|delivered|failed|canceled` (§1.1).
- **Réponse 404** : `{"status":"error","message":"no_active_delivery_session"}`
- **Réutilise** : `GetDeliverySession` (assemblage), `models.DeliverySession`/`models.Order`/`models.OrderUser`.
- **Crée** : route, handler, service, repo `GetActiveDeliverySessionForUser(ctx, merchantID, userID)` ;
  champ `CurrentOrderID` sur `DeliverySession` ; sous-objet `delivery_stop` (les 6 colonnes ajoutées
  par la migration 032) sur `models.Order`.

### 3.2 `PATCH /delivery_sessions/me/stops/{order_id}/select` — **CRÉER** (transition 1)

- **Permission** : `authMiddleware` + vérification service `delivery_session.user_id ==
  user.UserID`.
- **Body** : aucun.
- **Réponse 200** : session mise à jour, même forme que §3.1.
- **Erreurs** : `404 stop_not_found`, `409 stop_not_pending`, `409 session_not_active`.
- **Réutilise** : notification `"UPDATE_DELIVERY_SESSION"` (`delivery_sessions/service.go:52,71,103`).
- **Crée** : route, handler, service, repo (UPDATE des 2 lignes `delivery_session_order` +
  `delivery_session.current_order_id`).

### 3.3 `PATCH /delivery_sessions/me/stops/{order_id}/arrived` — **CRÉER** (transition 2, voie manuelle)

- **Permission** : idem 3.2.
- **Body** : `{}`.
- **Réponse 200** : session mise à jour.
- **Erreurs** : `409 stop_not_en_route`, `409 not_current_stop`.
- **Crée** : route, handler, service, repo.

> La voie **système (geofence)** de la même transition est documentée en §3.7 (effet de bord de
> `PATCH /users/location`), pas une route séparée.

### 3.4 `PATCH /delivery_sessions/me/stops/{order_id}/delivered` — **CRÉER** (transition 3)

- **Permission** : idem 3.2.
- **Body** : `{}`.
- **Logique** :
  1. Vérifier `order_id == current_order_id` et `delivery_session_order.status IN
     ('en_route','arrived')`.
  2. Appeler **`OrdersLifeCycleService.SetDelivered(ctx, orderID)`** (réutilisé tel quel — gère le
     check de paiement complet, le hash NF525, `state='CLOSED'`, l'auto-clôture éventuelle de
     session §0.3, et émet `UPDATE_ORDER`).
  3. Si (2) retourne `OrderNotFullyPaidError` → `409 order_not_fully_paid` (le client doit
     d'abord appeler `POST /orders/{order_id}/payments/create`, existant, réutilisé tel quel).
  4. `UPDATE delivery_session_order SET status='delivered', delivered_at=UTC_TIMESTAMP() WHERE ...`
  5. `UPDATE payments SET user_id=<user.UserID> WHERE order_id=<order_id>` (réassignation
     immédiate — même requête que `CloseDeliverySession` step 3,
     `delivery_sessions/repository.go:391-401`, scopée à une commande).
  6. Avancer `current_order_id` (§1.3 #3).
  7. Émettre `"UPDATE_DELIVERY_SESSION"`.
- **Réponse 200** : session mise à jour (`current_order_id` avancé, `delivery_session.status`
  potentiellement `'0'` si auto-clôturée — §0.3/§2.2).
- **Erreurs** : `409 order_not_fully_paid`, `409 stop_not_active`, `409 not_current_stop`.
- **Réutilise** : `SetDelivered` (paiement NF525, hash, auto-close), pattern SQL de réassignation
  paiement, notifications `UPDATE_ORDER` + `UPDATE_DELIVERY_SESSION`.
- **Crée** : route, handler, service, repo (étapes 4-6).

### 3.5 `PATCH /delivery_sessions/me/stops/{order_id}/failed` — **CRÉER** (transition 4)

- **Permission** : idem 3.2.
- **Body** : `{"reason": "client absent"}` (`reason` requis, ≤ 255 caractères → `fail_reason`).
- **Logique** :
  1. Vérifier `order_id == current_order_id` et `delivery_session_order.status NOT IN
     ('delivered','failed','canceled')`.
  2. `UPDATE delivery_session_order SET status='failed', failed_at=UTC_TIMESTAMP(),
     fail_reason=? WHERE ...`
  3. `UPDATE orders SET brand_status='DELIVERY_FAILED' WHERE order_id=?` *(§0.4 — pas `'FAILED'`)*
  4. Avancer `current_order_id` (§1.3 #3).
  5. Émettre `"UPDATE_DELIVERY_SESSION"` + `notification.NotificationTypeOrderUpdate`
     (`brand_status` a changé).
- **Réponse 200** : session mise à jour.
- **Erreurs** : `400 reason_required`, `409 stop_already_terminal`, `409 not_current_stop`.
- **Réutilise** : notifications existantes.
- **Crée** : route, handler, service, repo (étapes 2-4), nouvelle requête `UPDATE orders SET
  brand_status='DELIVERY_FAILED'`.

### 3.6 `PATCH /delivery_sessions/me/stops/{order_id}/cancel` — **CRÉER** (transition 5)

- **Permission** : idem 3.2.
- **Body (indicatif — `[A VERIFIER]` cf. §1.3 #5)** : `{"reason_id": "...", "comment": "Restaurant fermé à l'arrivée"}`.
  Si le chemin (b) (remboursement, `ProcessRefund`) s'avère nécessaire en plus du chemin (a), des
  champs additionnels (`device_id`, `mop`, `amount`) seraient requis — **non figés ici**, à
  trancher à l'implémentation selon `paidAmount`.
- **Logique** :
  1. Vérifier `order_id == current_order_id` et `delivery_session_order.status NOT IN
     ('delivered','failed','canceled')`.
  2. **`[A VERIFIER]`** Router vers le chemin d'annulation existant — (a) `PATCH
     /orders/{order_id}/cancel` (`DeleteOrder`) et/ou (b) `POST /orders/{order_id}/refund`
     (`ProcessRefund`) — cf. §1.3 #5 pour le détail des deux candidats et leurs effets respectifs
     sur `orders.state`/`brand_status`/`payments`.
  3. `UPDATE delivery_session_order SET status='canceled', canceled_at=UTC_TIMESTAMP() WHERE ...`
  4. Avancer `current_order_id` (§1.3 #3).
  5. Émettre `"UPDATE_DELIVERY_SESSION"` + `notification.NotificationTypeOrderUpdate`.
- **Réponse 200** : session mise à jour.
- **Erreurs** : `409 stop_already_terminal`, `409 not_current_stop`, + erreurs propagées du
  chemin d'annulation choisi (ex. `ErrReceiptNotFound`, `ErrRefoundMustBeLowerThanOriginalReceipt`
  si chemin (b)).
- **Réutilise** : notifications existantes ; chemin d'annulation `[A VERIFIER]`.
- **Crée** : route, handler, service, repo (étapes 3-4), câblage vers le chemin choisi.

### 3.7 `PATCH /users/location` — **ADAPTER** (transition 2, voie geofence + historique position)

Endpoint **existant** (`internal/modules/users/handler.go`, `service.go:54-65`,
`repository.go:22-35`). Conserve sa route, sa permission (`authMiddleware`, anti-spoofing
`req.UserID = user.UserID`).

- **Body étendu** :
```json
{
  "lat": 48.8566,
  "lng": 2.3522,
  "heading": 87.5,
  "accuracy": 5.2,
  "speed": 1.4
}
```
  `heading`, `accuracy`, `speed` optionnels (`*float64`, `omitempty`).
- **Effets** :
  1. *(adapté, §0.2)* `UPDATE users SET lat=?, lng=?, heading=?, last_position_at=UTC_TIMESTAMP()
     WHERE user_id=?` — écriture **directe sur `users`**, plus via `user_status_view` (qui reste
     un chemin de lecture exposant déjà `heading`, §0.1). `accuracy`/`speed` ne sont **pas**
     persistés sur `users`, seulement dans `delivery_position` (effet 2).
  2. *(nouveau)* Si une session `delivery_session.status IN ('1','PENDING') AND
     user_id=<user.UserID>` existe : `INSERT INTO delivery_position (user_id,
     delivery_session_id, lat, lng, heading, accuracy, speed, recorded_at) VALUES (?, ?, ?, ?, ?,
     ?, ?, UTC_TIMESTAMP())`.
  3. *(nouveau, geofence)* Si (2) a eu lieu et que `delivery_session.current_order_id` pointe vers
     un stop `status='en_route'` : calculer la distance entre `(lat,lng)` et l'adresse de
     livraison de cette commande (`customer.use_customer_temporary_address ?
     (customer_temporary_lat,customer_temporary_lng) : (customer_lat,customer_lng)` — cf. audit
     `[DETTE TECHNIQUE]` sur le typage `*string` de ces colonnes). Si distance ≤
     `GEOFENCE_RADIUS_M` → appliquer transition #2 (`en_route → arrived`).
- **Réponse** : inchangée (`{"status":"success"}`).
- **Réutilise** : route, permission, anti-spoofing.
- **Crée/Adapte** : `models.UpdateLocationRequest` (+`Heading`, `Accuracy`, `Speed *float64`),
  `SetUserLocation` (repository + service, écriture `users` au lieu de `user_status_view`),
  insertion conditionnelle `delivery_position`, calcul de distance + transition #2.

### 3.8 `PATCH /delivery_sessions/me/close` — **CRÉER** (clôture pilotée par le livreur, §1.5)

- **Permission** : `authMiddleware` + vérification service `delivery_session.user_id ==
  user.UserID` (idem 3.2).
- **Body** : `{}`.
- **Pré-condition** : **tous** les arrêts de la session sont terminaux (`delivery_session_order.status
  IN ('delivered','failed','canceled')` pour 100% des lignes) — sinon `409
  session_has_pending_stops`.
- **Effets** :
  - `UPDATE delivery_session SET status='done', end_date=UTC_TIMESTAMP() WHERE id=? AND
    user_id=? AND status IN ('1','PENDING')` *(cf. §0.5 pour la valeur `'done'` — risque de 7ᵉ
    valeur tant que §7 n'est pas fait)*.
  - **Ne réassigne PAS** les paiements (déjà fait par commande à `delivered`, transition #3).
  - **Ne force PAS** les commandes `failed`/`canceled` restantes en `state='CLOSED'` (elles
    restent telles que laissées par les transitions #4/#5, pour re-dispatch ultérieur).
  - Ne touche pas aux statuts par arrêt (déjà figés).
- **Réponse 200** : session mise à jour (`status='done'`).
- **Erreurs** : `409 session_has_pending_stops`, `404 session_not_found` (si déjà clôturée par
  ailleurs — `0` lignes affectées).
- **Réutilise** : notification `"UPDATE_DELIVERY_SESSION"`.
- **Crée** : route, handler, service, repo.

---

## 4. Annexe — Phase 2 : `GET /track/{ref}` (non implémenté)

Page de suivi **client final**, publique (pas de `authMiddleware`). Documentée pour information,
**pas implémentée**.

- **`ref` = `orders.public_id`** (migration `033`, nouvelle colonne `VARCHAR(45)`, unique,
  générée par trigger `trg_orders_public_id` à l'`INSERT` sous la forme
  `order-<32 hex aléatoires>`). Remplace l'`order_id` séquentiel comme référence partagée
  côté client — l'identifiant est non-séquentiel et non-énumérable par construction.
- **Note** : les commandes créées **avant** la migration `033` ont `public_id IS NULL` (le
  backfill est laissé en commentaire dans `033_orders_public_id.up.sql`, à exécuter séparément
  selon le volume). Sans backfill, `/track/{ref}` ne fonctionnera que pour les commandes créées
  après la migration — acceptable car le suivi ne concerne que des tournées **en cours**.
- **Réponse 200** (commande dont la session est active) :
```json
{
  "order": {
    "order_num": "27",
    "state": "OPEN",
    "delivery_status": "en_route"
  },
  "driver": {
    "lat": 48.8566,
    "lng": 2.3522,
    "heading": 87.5,
    "last_position_at": "2026-06-11T10:32:00Z"
  },
  "stop_progress": {
    "current_stop_index": 2,
    "total_stops": 4
  },
  "remaining_stops": [
    { "lat": 48.8580, "lng": 2.3510 },
    { "lat": 48.8590, "lng": 2.3530 }
  ]
}
```
- **`stop_progress`** : `current_stop_index`/`total_stops` recalculés à **chaque requête** sur la
  base des arrêts `status NOT IN ('delivered','failed','canceled')`, ordonnés par `priority`
  (ordre d'**affichage**, pas garanti comme ordre réel de visite — la livraison hors-ordre est
  permise, §1).
- **`remaining_stops`** : arrêts intermédiaires **anonymisés** (lat/lng uniquement, aucune donnée
  commande/client).
- **404** : dès que la session passe en état non-actif (`status NOT IN ('1','PENDING')`, §2.1) —
  y compris auto-clôture `'0'` ou clôture livreur `'done'`.
- **Recalcul serveur** : aucune donnée de progression stockée ; tout est dérivé de
  `delivery_session`/`delivery_session_order`/`delivery_position` à la volée.

---

## 5. Tableau récapitulatif — Réutiliser / Adapter / Créer

| Élément | Statut avant | Action | Référence |
|---|---|---|---|
| `GET /delivery_sessions/me` | `[ABSENT]` | **CRÉER** (route, handler, service, repo) | s'inspire de `GetDeliverySession` (`internal/modules/delivery_sessions/repository.go:422-514`) |
| `.../stops/{id}/select` (transition 1) | `[ABSENT]` | **CRÉER** | écrit `delivery_session_order.status`, `delivery_session.current_order_id` |
| `.../stops/{id}/arrived` (transition 2, manuel) | `[ABSENT]` | **CRÉER** | |
| Geofence auto-arrivée (transition 2, système) | `[ABSENT]` | **CRÉER**, intégré dans `PATCH /users/location` adapté | §3.7 |
| `.../stops/{id}/delivered` (transition 3) | `[PARTIEL]` | **RÉUTILISER** `SetDelivered`/`SetDeliveredLocal` (`order_life_cycle/service.go:283`, `repository.go:725-840`) **+ CRÉER** la couche `delivery_session_order`/`current_order_id`/réassignation paiement | auto-clôture session déjà existante (`status='0'`, §0.3) |
| `.../stops/{id}/failed` (transition 4) | `[ABSENT]` | **CRÉER** | nouvelle valeur `brand_status='DELIVERY_FAILED'` (§0.4) |
| `.../stops/{id}/cancel` (transition 5, NOUVEAU) | `[ABSENT]` | **CRÉER** | chemin d'annulation `[A VERIFIER]` — §1.3 #5 |
| `PATCH /delivery_sessions/me/close` (NOUVEAU) | `[ABSENT]` | **CRÉER** | §1.5/§3.8 ; écrit `delivery_session.status='done'` — risque de 7ᵉ valeur, §0.5 |
| Réassignation paiement au livreur | `[EXISTE]` (au close de session, bulk) | **RÉUTILISER** le pattern SQL de `CloseDeliverySession` step 3 (`repository.go:391-401`), appliqué par commande dès "livré" | |
| `PATCH /users/location` (heading + historique) | `[EXISTE]` (lat/lng, via `user_status_view`) | **ADAPTER** : `UpdateLocationRequest` (`internal/models/request_objects.go:807-811`), `SetUserLocation` (`internal/modules/users/repository.go:22-35`, `service.go:54-65`) — écriture désormais directe sur `users` (§0.2) | + écriture `delivery_position` + geofence |
| `delivery_session_order.status/arrived_at/delivered_at/failed_at/canceled_at/fail_reason` | `[ABSENT]` | **CRÉER** (migration 032) | |
| `delivery_session.current_order_id` | `[ABSENT]` | **CRÉER** (migration 032) | type `[À VÉRIFIER EN BASE]`, §0 |
| `users.heading` | `[EXISTE]` *(corrigé, §0.1)* | **NE PAS RECRÉER** | déjà sélectionnée par `user_status_view` |
| `users.last_position_at` | `[ABSENT]` | **CRÉER** (migration 032) | |
| `delivery_position` (historique position) | `[ABSENT]` | **CRÉER** (migration 032, nouvelle table) | écrite uniquement si session active |
| `customer.delivery_notes` | `[ABSENT]` | **CRÉER** (migration 032) | |
| `orders.public_id` + trigger `trg_orders_public_id` | `[ABSENT]` | **CRÉER** (migration 033) | pour `GET /track/{ref}` (§4, phase 2) |
| Notification `"UPDATE_DELIVERY_SESSION"` | `[EXISTE]` (`delivery_sessions/service.go:52,71,103`) | **RÉUTILISER** pour toutes les transitions par arrêt + `/me/close` | |
| Notification `UPDATE_ORDER` | `[EXISTE]` (auto via `ExecuteOrderMutation`, `order_life_cycle/service.go:111`) | **RÉUTILISER** (déclenchée automatiquement par `SetDelivered` ; appel manuel ajouté pour transitions 4/5) | |
| 1 session active / livreur | `[EXISTE]` (`StartDeliverySession`) | **RÉUTILISER** tel quel | |
| `end_date` écrit au close/cancel/`me/close` | `[ABSENT]` (jamais écrit, audit §2.5) | **DOCUMENTER** comme recommandation future Go (§2.3/§7), pas fait dans cette phase | |
| Normalisation `delivery_session.status` (`'1'/'PENDING'/'0'/'DONE'/'FINISHED'/'CANCELED'` → `active`/`done`/`canceled`) | `[DETTE TECHNIQUE]` (audit §2.5 + §0.3) | **SPÉCIFIÉ** en §7, **PAS fait** dans cette passe | |
| Paiement à la livraison (COD) | `[EXISTE]` (`AddPayment`/NF525) | **RÉUTILISER** tel quel — aucun nouvel endpoint paiement | `POST /orders/{order_id}/payments/create` |
| Annulation/remboursement pendant tournée | `[EXISTE]` (deux chemins distincts, §1.3 #5) | **RÉUTILISER** l'un et/ou l'autre — choix `[A VERIFIER]` | `PATCH /orders/{id}/cancel`, `POST /orders/{id}/refund` |
| Pilotage TPE | `[ABSENT]` | **HORS SCOPE** (non traité) | |
| Purge des positions (`delivery_position`) | `[ABSENT]` | **HORS SCOPE schéma** — job opérationnel, §6 | |

---

## 6. Conformité — rétention des positions (`delivery_position`)

`delivery_position` enregistre la trace GPS **brute** du livreur (lat/lng/heading/accuracy/speed,
horodatés) pendant chaque session active. À ce titre, c'est une donnée personnelle de
géolocalisation soumise aux principes de **minimisation** et de **limitation de la conservation**
(RGPD/CNIL).

- **Recommandation** : conserver le détail brut **30 à 90 jours** (fenêtre exacte à arbitrer avec
  le DPO du client final — non tranchée ici), suffisant pour l'audit/replay d'une tournée et la
  gestion de litiges de livraison récents.
- **Au-delà de cette fenêtre** : purger les lignes `delivery_position` brutes, en conservant les
  **agrégats** déjà existants au niveau `delivery_session` (`distance`, `duration`) qui résument
  la tournée sans exposer le tracé GPS détaillé.
- **Implémentation** : un job de purge périodique (`DELETE FROM delivery_position WHERE
  recorded_at < UTC_TIMESTAMP() - INTERVAL <N> DAY`), à câbler comme tâche `internal/tasks/`
  (`TasksManager`, cron actuellement désactivé — `cmd/api/tasks.go`). **Hors scope schéma** :
  aucune table/colonne supplémentaire requise pour cette passe ; mentionné ici pour ne pas être
  oublié lors de la mise en service de `delivery_position`.

---

## 7. Passe ultérieure — normalisation `delivery_session.status` (SPEC SEULE, NON faite ici)

Cette section **spécifie** une migration de données + adaptation Go pour unifier
`delivery_session.status` sur le vocabulaire snake_case `active`/`done`/`canceled` (§0.4),
résorbant la dette accumulée (audit §2.5, §0.3, §0.5). **À exécuter dans une passe dédiée,
testée séparément** — pas dans la migration `032`/`033` ni en même temps que le reste de cette
conception.

Stratégie **expand → migrate → contract** :

1. **Expand** — déployer du code Go tolérant aux **deux** vocabulaires simultanément :
   - `StartDeliverySession`, `CloseDeliverySession`, `CancelDeliverySession`,
     `GetPendingDeliverySessions`, `GetDeliverySessions` : remplacer toute comparaison stricte
     (`status = 'PENDING'`, `status >= 0`, `status NOT IN ('-1','CLOSED','CANCELED')`, etc.) par
     des gardes acceptant les deux formes, ex. `status IN ('1','PENDING','active')`,
     `status IN ('0','DONE','done')`, `status IN ('CANCELED','canceled')`.
   - `/delivery_sessions/me/close` (§3.8) : si déjà livré avant cette étape avec `status='done'`
     (option (b) de §0.5), aucune action ; sinon l'implémenter maintenant avec `'done'`.
   - Recréer `user_status_view` (si elle expose/filtre sur `delivery_session.status`, à vérifier
     via `SHOW CREATE VIEW user_status_view`) avec un `CASE` de normalisation à la lecture, pour
     que les deux familles de valeurs soient présentées de façon cohérente pendant la transition.
2. **Migrate** — migration de données (nouvelle migration SQL numérotée, hors `032`/`033`) :
   - `UPDATE delivery_session SET status='active' WHERE status IN ('1','PENDING')`
   - `UPDATE delivery_session SET status='done' WHERE status IN ('0','DONE','FINISHED')`
   - `UPDATE delivery_session SET status='canceled' WHERE status IN ('CANCELED')`
   - `'-1'`/`'CLOSED'` : aucune ligne attendue (jamais écrites, audit §2.5) — vérifier par
     `SELECT COUNT(*)` avant migration pour confirmer.
   - Harmoniser à cette occasion `SetDeliveredLocal`'s `SET ds.status = 0` (§0.3) en
     `SET ds.status = 'done', ds.end_date = UTC_TIMESTAMP()`.
3. **Contract** — une fois (2) déployé et vérifié sans écrivain legacy restant : retirer la
   tolérance double-vocabulaire introduite en (1), ne garder que `active`/`done`/`canceled`.
   Documenter `delivery_session.status` comme `VARCHAR` snake_case dans une future mise à jour de
   ce document / de l'audit.

**Dépendances** : cette passe doit être **terminée (étape 2 au minimum)** avant — ou en même
release que — toute implémentation de `/delivery_sessions/me/close` qui écrirait `'done'` (§0.5),
pour éviter la coexistence de `'0'`/`'DONE'`/`'done'`.

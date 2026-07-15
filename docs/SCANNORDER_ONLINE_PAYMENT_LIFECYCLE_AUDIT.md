# Audit — cycle de vie du paiement en ligne ScanNOrder (`PENDING_ONLINE_PAYMENT` ?)

> Audit du 2026-07-15. Aucune modification de code. Objectif : établir avec
> certitude si un statut `PENDING_ONLINE_PAYMENT` (ou équivalent) existe côté
> ScanNOrder, tracer son cycle de vie complet, et comparer avec le mécanisme
> `PENDING_CARD_PAYMENT` déjà implémenté côté Kiosk.
>
> Sources déjà existantes réutilisées et vérifiées ligne par ligne contre le
> code actuel : `docs/order-lifecycle.md` (2026-06-30, scope ScanNOrder) et
> `docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md` (2026-07-15, scope
> `brand_status`/`merchant_approval`). Un écart significatif a été trouvé
> entre `order-lifecycle.md` et l'état actuel : voir §4.

---

## 1. Statut(s) trouvé(s) exactement

**`PENDING_ONLINE_PAYMENT` n'existe pas sous ce nom exact dans le repo** (`grep -ri "pending_online_payment"` → aucune occurrence). Ce qui existe réellement :

| Constante/valeur exacte | Champ | Type | Défini/écrit où |
|---|---|---|---|
| `"ONLINE_PAYMENT_PENDING"` (chaîne brute, **pas de constante Go**) | `orders.brand_status` | `VARCHAR` | [internal/modules/order_life_cycle/repository.go:1780](internal/modules/order_life_cycle/repository.go#L1780) (`setOrderDefaults`) |
| `models.MerchantApprovalPendingApproval = "PENDING_APPROVAL"` | `orders.merchant_approval` | `VARCHAR` | [internal/models/orders_model.go:16](internal/models/orders_model.go#L16) |

Le mot-clé le plus proche de la demande (« ONLINE_PAYMENT » + « PENDING ») est donc **`ONLINE_PAYMENT_PENDING`** — ordre des mots inversé par rapport à `PENDING_ONLINE_PAYMENT`, et c'est un état sur `brand_status`, pas `merchant_approval`.

Point notable : `brand_status` n'a **aucune constante Go** définie pour cette valeur (ni pour aucune autre valeur de `brand_status` — confirmé par `docs/order-lifecycle.md` §P8 et `docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md`). Toutes les comparaisons/écritures se font en chaîne brute SQL, contrairement à `merchant_approval` qui a au moins une constante nommée.

---

## 2. Séquence exacte de création de commande ScanNOrder avec paiement en ligne

Endpoint : `POST /scannorder/{slug}/order` → `Handler.CreateOrderSNO` → `Service.CreateOrderSNO` ([internal/modules/scannorder/service.go:777](internal/modules/scannorder/service.go#L777)).

Ordre exact des opérations, vérifié dans le code actuel (`CreateOrderSNO`, cas `TAKE_AWAY`/`DELIVERY`) :

1. **Résolution du merchant** via le QR code ([service.go:781](internal/modules/scannorder/service.go#L781)).
2. **Détermination du type de commande** — pour `TAKE_AWAY`/`DELIVERY`, ScanNOrder force systématiquement :
   ```go
   order.OnlinePayment    = true
   order.MerchantApproval = "PENDING_APPROVAL"
   ```
   ([service.go:851-852](internal/modules/scannorder/service.go#L851), [service.go:882-883](internal/modules/scannorder/service.go#L882)). Il n'y a **pas de choix client** entre paiement en ligne et paiement à la livraison en espèces pour ces deux types — voir §6.
3. **Calcul du pricing** (`GetPricingSNO`), avec garde sur les produits indisponibles ([service.go:887-911](internal/modules/scannorder/service.go#L887)).
4. **Création de la ligne commande en base — AVANT toute session Stripe** : `s.orderLifeCycleSvc.CreateOrder(...)` ([service.go:924-932](internal/modules/scannorder/service.go#L924)) → `insertOrderBase`, précédé de `setOrderDefaults` ([order_life_cycle/repository.go:1074](internal/modules/order_life_cycle/repository.go#L1074), [:1739-1784](internal/modules/order_life_cycle/repository.go#L1739)).
   - `setOrderDefaults` ne pose `brand_status` que si vide (`req.Order.BrandStatus == ""`). Comme `CreateOrderSNO` n'écrit jamais `order.BrandStatus` explicitement dans les branches `DELIVERY`/`TAKE_AWAY`, le défaut s'applique :
     ```go
     if req.Order.OnlinePayment {
         req.Order.BrandStatus = "ONLINE_PAYMENT_PENDING"
     } else {
         req.Order.BrandStatus = "PENDING"
     }
     ```
     ([repository.go:1776-1784](internal/modules/order_life_cycle/repository.go#L1776)) — comme `OnlinePayment=true`, `brand_status='ONLINE_PAYMENT_PENDING'`.
   - **Statut immédiatement après création, avant toute confirmation de paiement** :
     `state='OPEN'` (défaut DB), **`brand_status='ONLINE_PAYMENT_PENDING'`**, **`merchant_approval='PENDING_APPROVAL'`**, `isPaid=0`.
5. **Seulement après le commit de la commande en base**, si `newOrder.Action == "payment"` : création de la Checkout Session Stripe (`s.StripeManager.CreateCheckoutSession(...)`, [service.go:939-945](internal/modules/scannorder/service.go#L939)), expiration Stripe fixée à **30 minutes** (`stripe/checkout.go:133,263`). L'URL de paiement est retournée au client dans la réponse HTTP.

**Réponse à la question posée** : la ligne commande est créée **avant** la Checkout Session Stripe, jamais l'inverse. Le champ exact qui porte l'état d'attente de paiement en ligne est `brand_status` (valeur `'ONLINE_PAYMENT_PENDING'`), **pas** `merchant_approval` (qui est déjà à `'PENDING_APPROVAL'` dès la création, avant même le paiement — c'est trompeur si on lit seulement ce champ).

---

## 3. Transition au succès du paiement

Webhook Stripe : `checkout.session.completed` → `StripeWebhookService.HandleCheckoutSessionCompleted` ([internal/webhook/stripe/service.go:97](internal/webhook/stripe/service.go#L97)), dans une transaction unique ([service.go:122-184](internal/webhook/stripe/service.go#L122)) :

1. Invalidation cache Redis de la commande.
2. Insertion du paiement (`CreatePaymentNoNotification`).
3. `UpdateOrderCreationDate` — reset de `creation_date` au moment du paiement (impacte les crons temporels, voir §4).
4. `UpdateOrderPaymentStatus` ([stripe/repository.go:109-125](internal/webhook/stripe/repository.go#L109)) → `isPaid = (price <= total_paid)`.
5. **`UpdateOrderDetails`** ([stripe/repository.go:127-138](internal/webhook/stripe/repository.go#L127)) — la transition d'état proprement dite :
   ```sql
   UPDATE orders SET brand_status = 'PENDING_APPROVAL',
                      merchant_approval = 'PENDING_APPROVAL',
                      last_update = UTC_TIMESTAMP()
   WHERE checkout_session_id = ? AND order_id = ?
   ```
   → `brand_status` passe de `'ONLINE_PAYMENT_PENDING'` à `'PENDING_APPROVAL'`. `merchant_approval` est réécrit à la même valeur qu'il avait déjà (sans effet ici, mais **écrase** un `'ACCEPTED'` antérieur si la commande en avait un — piège documenté indépendamment dans `order-lifecycle.md` §P7, hors scope commandes en ligne standard).
6. Auto-acceptation optionnelle si `merchant_parameters.auto_accept_*` est activé → `SetOrderAccepted` en tâche asynchrone post-commit, qui fait passer `brand_status='PENDING'`, `merchant_approval='ACCEPTED'`.
7. Notifications (websocket, email, SMS) envoyées uniquement après commit.

**Le passage à l'état "confirmé/acceptable par le restaurateur" est donc `brand_status: ONLINE_PAYMENT_PENDING → PENDING_APPROVAL`**, déclenché exclusivement par ce webhook. Aucun polling ni action manuelle ne peut produire cette transition.

---

## 4. Comportement en cas d'abandon

### Chemin normal (webhook `checkout.session.expired`)

`HandleCheckoutSessionCanceled` ([internal/webhook/stripe/service.go:236](internal/webhook/stripe/service.go#L236)) appelle `SetOrderDenied` → `brand_status='DENIED'`, `merchant_approval='DENIED'`, `state='CLOSED'`. La Checkout Session Stripe expire d'elle-même **30 minutes** après création ([infrastructure/stripe/checkout.go:133](internal/infrastructure/stripe/checkout.go#L133)) — c'est le seul TTL réel du système pour ce cas, **porté par Stripe, pas par le backend**.

### Si le webhook n'arrive jamais (panne, endpoint mal configuré)

**Aucun nettoyage automatique.** Le cron `DenyOrders` filtre :
```sql
WHERE o.brand_status = 'PENDING_APPROVAL' AND o.merchant_approval = 'PENDING_APPROVAL'
```
([internal/tasks/orders.go:91-92](internal/tasks/orders.go#L91)) — cette requête **ne capture jamais** `brand_status='ONLINE_PAYMENT_PENDING'`. Une commande dont le webhook Stripe échoue reste bloquée en `ONLINE_PAYMENT_PENDING` / `state='OPEN'` **indéfiniment**, sans TTL applicatif.

### ⚠️ Écart trouvé par rapport à `docs/order-lifecycle.md` (2026-06-30) et à CLAUDE.md

Ces deux documents affirment que **toutes les tâches cron sont désactivées** en production (`cmd/api/tasks.go:17` contenait un `return` immédiat). **Ce n'est plus vrai aujourd'hui** — `cmd/api/tasks.go` (fichier modifié, actuellement non commité) enregistre désormais activement tous les jobs, dont `DenyOrders` toutes les **1 minute** :

```go
add("@hourly", taskManager.CloseOrders)
add("@every 1m", taskManager.DenyOrders)
...
c.Start()
log.Info("✅ Système CRON démarré (toutes tâches actives, protégées par SkipIfStillRunning + Recover)")
```
([cmd/api/tasks.go:44-65](cmd/api/tasks.go#L44)).

**Conséquence pratique** : le cron tourne bel et bien maintenant, mais son filtre reste inchangé et **ne couvre toujours pas** `ONLINE_PAYMENT_PENDING`. Le risque décrit dans `order-lifecycle.md` §P3 ("état orphelin") **persiste identiquement**, même avec le cron actif — ce n'est pas un problème d'exécution du cron, c'est un problème de filtre SQL trop restrictif. `CLAUDE.md` doit être mis à jour séparément (« cron scheduling... currently disabled » n'est plus exact) — hors scope de cet audit, signalé pour information.

---

## 5. Comparaison avec le mécanisme Kiosk (`PENDING_CARD_PAYMENT`)

**Mécanisme différent, pas juste un nom différent.** Trois divergences structurelles :

| | ScanNOrder (`ONLINE_PAYMENT_PENDING`) | Kiosk (`PENDING_CARD_PAYMENT`) |
|---|---|---|
| **Champ porteur** | `brand_status` | `merchant_approval` ([internal/modules/kiosk/service.go:1481](internal/modules/kiosk/service.go#L1481)) |
| **Constante Go** | Aucune (chaîne brute) | `models.MerchantApprovalPendingCardPayment` ([internal/models/orders_model.go:25](internal/models/orders_model.go#L25)) |
| **`merchant_approval` à la création** | Déjà `'PENDING_APPROVAL'` (forcé, avant tout paiement) | `'PENDING_CARD_PAYMENT'` (reflète l'attente réelle) |
| **Mécanisme de paiement** | Stripe **Checkout** (redirection web, `checkout.session.completed`/`expired`) | Stripe **Terminal** (`payment_intent.succeeded`/`payment_failed`, mapping Redis `terminal_pi:{id}`, TTL 1h — [infrastructure/stripe/terminal.go:35](internal/infrastructure/stripe/terminal.go#L35)) |
| **Transition succès** | `ONLINE_PAYMENT_PENDING → PENDING_APPROVAL` (webhook), puis acceptation manuelle/auto → `ACCEPTED` (2 étapes) | `PENDING_CARD_PAYMENT → ACCEPTED` directement, via `SetOrderAccepted` ([webhook/stripe/service.go:384](internal/webhook/stripe/service.go#L384)) — 1 seule étape |
| **TTL abandon** | 30 min côté Stripe Checkout (`checkout.session.expired` → `DENIED`) ; aucun côté backend si le webhook échoue | Aucun — ni côté Stripe Terminal, ni côté backend. Le mapping Redis expire après 1h mais ça ne nettoie **pas** la commande, seulement le lien PI↔commande |
| **Capturé par `DenyOrders` cron ?** | Non (`brand_status` reste `ONLINE_PAYMENT_PENDING`, jamais `PENDING_APPROVAL`) | Non plus (`brand_status='PENDING'` par défaut car `OnlinePayment=false` posé explicitement — [kiosk/service.go:1511](internal/modules/kiosk/service.go#L1511) — donc ni `brand_status` ni `merchant_approval` ne matchent le filtre `PENDING_APPROVAL`/`PENDING_APPROVAL`) |

**Kiosk ne reproduit donc pas le pattern ScanNOrder** — il a délibérément choisi un mécanisme plus simple et plus cohérent : un seul champ (`merchant_approval`), une constante nommée, une transition directe en une étape. C'est documenté comme choix conscient dans `docs/KIOSK_DECISIONS.md` (« Statut "pending_counter_payment" ») et confirmé sûr par `docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md` §2-3 (ne pas réutiliser `brand_status`, déjà surchargé par 3 canaux).

**Point commun aux deux mécanismes, non résolu dans l'un ou l'autre** : aucun des deux ne bénéficie d'un TTL/cron de nettoyage backend en cas d'abandon client (fermeture d'onglet Checkout après échec silencieux du webhook, ou carte jamais présentée au Terminal). Le seul filet de sécurité pour ScanNOrder est le TTL Stripe (30 min) sur la Checkout Session elle-même, qui ne protège que le cas où le webhook `checkout.session.expired` arrive effectivement — la panne webhook reste un point aveugle des deux côtés.

---

## 6. Cas "commande sans paiement en ligne" côté ScanNOrder

**N'existe pas pour `TAKE_AWAY`/`DELIVERY`.** Comme vu en §2, `CreateOrderSNO` force `order.OnlinePayment = true` pour ces deux types, sans branche alternative — il n'y a **aucun choix de "paiement à la livraison en espèces"** exposé au client ScanNOrder pour ces flux ([service.go:851-852](internal/modules/scannorder/service.go#L851), [:882-883](internal/modules/scannorder/service.go#L882)).

Le seul cas ScanNOrder créé directement sans paiement en ligne est **`order_type = "IN"`** (commande sur place, table scannée) :
```go
case "IN":
    ...
    order.MerchantApproval = "ACCEPTED"
    order.BrandStatus = "PENDING"
```
([service.go:832-833](internal/modules/scannorder/service.go#L832)) — commande créée directement `ACCEPTED`/`PENDING`, encaissement géré ensuite au comptoir. C'est exactement le cas cité dans `docs/KIOSK_DECISIONS.md` (« comme `order_type = "IN"` chez ScanNOrder »).

**Symétrie avec Kiosk `pay_at_counter`** : le pattern Kiosk `pay_at_counter` (commande créée directement `ACCEPTED`, [kiosk/service.go:1482-1486](internal/modules/kiosk/service.go#L1482)) est plus proche du cas `IN` de ScanNOrder que du cas `TAKE_AWAY`/`DELIVERY`. Il n'y a **pas d'équivalent ScanNOrder** à "commande à emporter payée en espèces à la livraison" — ScanNOrder TAKE_AWAY/DELIVERY est **toujours** payé en ligne à la création.

---

## 7. Conclusion

**Le Kiosk n'a pas besoin de changer de champ ni de mécanisme.** Trois constats principaux :

1. **`PENDING_ONLINE_PAYMENT` n'existe pas** — la valeur réelle est `ONLINE_PAYMENT_PENDING` sur `brand_status` (mots inversés), un champ déjà identifié comme fragile et à ne pas réutiliser (`docs/BRAND_STATUS_MERCHANT_APPROVAL_AUDIT.md`). Le Kiosk utilise `merchant_approval` (`PENDING_CARD_PAYMENT`), un choix plus sûr et déjà audité comme tel.
2. **Le mécanisme ScanNOrder n'est pas un modèle à imiter tel quel** — il souffre d'un état orphelin non couvert par le cron (`ONLINE_PAYMENT_PENDING` jamais capturé par `DenyOrders`, même maintenant que le cron est actif), d'un champ sans constante Go, et d'une transition en deux étapes (`ONLINE_PAYMENT_PENDING → PENDING_APPROVAL → ACCEPTED`) plus complexe que nécessaire. Le Kiosk (`PENDING_CARD_PAYMENT → ACCEPTED` en une étape) est déjà **plus simple et plus robuste** que le pattern ScanNOrder existant.
3. **Le vrai point à corriger n'est pas côté nommage Kiosk, mais côté nettoyage des commandes abandonnées, dans les deux systèmes à la fois** — ni ScanNOrder (`ONLINE_PAYMENT_PENDING` non couvert par `DenyOrders`) ni Kiosk (`PENDING_CARD_PAYMENT` non couvert non plus, aucun TTL Terminal côté commande) n'ont de garde-fou automatique en cas d'abandon client sans webhook. Si un TTL de nettoyage est souhaité pour le Kiosk (ex. `kiosk_settings.unpaid_order_cancel_minutes`, déjà prévu dans `docs/KIOSK_DECISIONS.md` §C), ce serait une **amélioration au-delà de ce que fait déjà ScanNOrder**, pas un alignement sur un comportement existant à reproduire.

**Recommandation** : ne pas introduire de statut `PENDING_ONLINE_PAYMENT`/`ONLINE_PAYMENT_PENDING` côté Kiosk. Conserver `merchant_approval = PENDING_CARD_PAYMENT`. Si un besoin de nettoyage automatique des commandes carte abandonnées est confirmé, l'implémenter comme un cron dédié Kiosk (filtrant sur `merchant_approval = 'PENDING_CARD_PAYMENT'` + délai), sans dépendance au pattern `brand_status` de ScanNOrder qui a le même trou.

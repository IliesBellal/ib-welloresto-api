# Acceptation automatique des commandes Uber Eats / Deliveroo — notifications

Implémenté le 2026-10-18. Pas de migration.

## Problème

Signalé le 2026-10-18 par un client : les commandes Uber Eats acceptées automatiquement
s'affichaient quelques secondes dans le panneau « commandes à accepter » du POS Flutter,
puis disparaissaient seules.

Cause : le webhook créait la commande (`merchant_approval = PENDING_APPROVAL`) et
`CreateOrder` envoyait aussitôt un `UPDATE_ORDER` (WebSocket + FCM). L'acceptation locale
suivait de quelques millisecondes, mais la notification suivante ne partait qu'**après**
l'appel HTTP d'acceptation chez Uber. Le POS relit la commande à chaque notification
(`order_network_manager.dart`, `updateOrderFromPushNotificationData`). Quand sa lecture
arrivait avant l'acceptation locale, il voyait une commande à accepter et la gardait
dans le panneau jusqu'à la notification suivante, soit la durée de l'appel Uber.

Ce comportement n'est pas lié au commit 01b7594 : le chemin n'avait pas changé depuis
mars-avril 2026. Sa visibilité dépend d'une course et de la latence d'Uber.

## Règles retenues

| Cas | Notification |
|---|---|
| Commande reçue, acceptation **manuelle** | à la création (inchangé) |
| Commande reçue, acceptation **automatique** | aucune à la création |
| Uber confirme l'acceptation | une notification, après l'appel Uber (inchangé) |
| Uber refuse ou l'appel échoue | la commande revient en `PENDING_APPROVAL`, puis une notification : elle apparaît dans les commandes à accepter |
| L'acceptation échoue avant l'appel Uber (erreur base) | une notification : la commande, restée à accepter, apparaît en caisse |
| Deliveroo, acceptation automatique | aucune à la création, une notification juste après l'acceptation locale (pas d'appel API Deliveroo sur ce chemin) |

Aucune notification n'est envoyée au moment de l'acceptation locale tant qu'Uber n'a pas
répondu. On n'imprime pas en cuisine une commande qu'Uber peut encore refuser.

Le cache Redis de la commande est vidé une seule fois, après l'écriture en base (succès
ou retour en attente) et juste avant la notification. Avant ce changement, le module
Uber Eats ne vidait jamais ce cache après `UpdateOrderAccepted`. Une lecture de la liste
(`GetPendingOrders`, qui met chaque commande en cache 10 min) faite pendant l'appel Uber
pouvait laisser une version périmée servie 10 minutes.

## Implémentation

- **`order_life_cycle/service.go`**
  - `CreateOrder` délègue à `createOrder(ctx, req, notify)`. Une nouvelle méthode,
    `CreateOrderWithoutNotification`, l'appelle sans notification. Tout le reste de la
    création est inchangé (contrôle de cohérence, placement auto, suivi upsell).
  - Nouvelle méthode `AutoAcceptOrder` : elle appelle `SetOrderAccepted` et, en cas
    d'erreur, notifie elle-même.
  - Goroutine Uber de `SetOrderAccepted` : elle appelle désormais
    `finishPlatformAcceptance(merchantID, orderID, acceptErr)`. Cette fonction remet la
    commande à accepter en cas d'échec, vide le cache, puis notifie. Elle utilise un
    contexte neuf de 10 s, car celui de l'appel Uber (30 s) peut avoir expiré.
- **`order_life_cycle/repository.go`** : `RevertOrderAcceptedLocal`. Elle ne touche
  qu'une commande restée exactement dans l'état posé par `SetOrderAcceptedLocal`
  (`OPEN` / `PENDING` / `ACCEPTED`) : une annulation reçue entre-temps
  (webhook `orders.cancel`) l'emporte. `brand_status` reste à `PENDING`.
- **Webhooks** `ubereats/service/order_notification.go` et `deliveroo_orders/service.go`
  (`ProcessNewOrder`) : si le magasin est en acceptation automatique, ils créent la
  commande avec `CreateOrderWithoutNotification` puis appellent `AutoAcceptOrder`.
  `ProcessNewOrderOld` (code mort) n'est pas touché.

Cela vaut aussi pour l'acceptation **manuelle** d'une commande Uber depuis la caisse,
qui passe par la même goroutine : si Uber refuse, la commande revient dans les
commandes à accepter sur tous les appareils.

## Décision : `RecoverOrderState` retiré du chemin d'acceptation

Après un échec d'acceptation Uber, le code appelait `RecoverOrderState`, qui resynchronise
la commande avec l'état renvoyé par Uber. Il est retiré de ce chemin, pour deux raisons :

- **Il héritait du contexte de l'appel Uber.** Après un timeout de 30 s, ce contexte avait
  expiré, et la récupération échouait sans rien faire. La commande restait acceptée en
  local alors qu'Uber ne l'avait pas confirmée.
- **Il ferme les commandes dans un état non géré.** `FinishOrderIfDoesNotExist` ne connaît
  que `ACCEPTED`, `HANDED_OFF` et `FAILED`. Tout autre état tombe dans le `default`
  (`SUCCEEDED`), et la commande est **close comme terminée**. Une commande encore en
  attente chez Uber (état probablement `OFFERED`, à confirmer) aurait été fermée, ce qui
  contredit la règle « elle apparaît dans les commandes à accepter ».

Les annulations côté Uber restent traitées par le webhook `orders.cancel`.

## Points ouverts (non traités)

- **`FinishOrderIfDoesNotExist`** est toujours utilisée par le refus, l'annulation et
  le passage à « prête » côté Uber, avec le même `default` qui ferme la commande.
  Vérifier la liste réelle des états Uber et gérer celui de la commande en attente.
- **Uber a accepté mais la réponse s'est perdue** (timeout) : la commande revient à
  accepter. Une nouvelle acceptation depuis la caisse rappelle Uber. Comportement d'Uber
  sur une double acceptation non vérifié. Un refus passe par `RecoverOrderState`, qui
  resynchronise sur `ACCEPTED`.
- **Délai d'apparition** : une commande Uber auto-acceptée n'apparaît (et ne s'imprime)
  sur la caisse qu'après la réponse d'Uber, en général environ une seconde, au pire 30 s.
- **Échec d'une acceptation manuelle Deliveroo** (appel API) : inchangé. L'erreur est
  journalisée et la commande reste acceptée en local.

## Tests

`internal/modules/order_life_cycle/platform_auto_accept_postgres_integration_test.go`
(`-tags postgres_integration`), 5 cas :

1. auto-acceptation Deliveroo → commande acceptée ;
2. commande introuvable → erreur remontée ;
3. acceptation Uber confirmée → rien n'est annulé ;
4. acceptation Uber refusée → retour en `PENDING_APPROVAL` ;
5. refus après une annulation reçue entre-temps → l'annulation est conservée.

La non-émission de la notification à la création n'est pas couverte : le hub WebSocket
n'expose pas de quoi observer une diffusion depuis ce package.

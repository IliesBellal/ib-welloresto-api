# Horaires de livraison ScanNOrder

Un établissement peut livrer sur des horaires plus courts que ses horaires
d'ouverture. Exemples : ouvert de 18h à minuit mais livraison de 19h à 22h30 ;
pas de livraison le midi sauf le mardi.

## Règle

- **Aucun créneau configuré** : la livraison suit les horaires d'ouverture, comme
  avant. C'est l'état par défaut de tous les établissements.
- **Des créneaux configurés** : une commande ScanNOrder `DELIVERY` est acceptée
  seulement si l'établissement est ouvert (contrôle existant) **et** que l'heure
  tombe dans un créneau de livraison du jour. Un jour sans créneau = pas de
  livraison ce jour-là.
- Un créneau a un début inclus et une fin exclue. Une fin `<=` au début veut dire
  que le créneau passe minuit (19:00 → 00:00, 22:00 → 01:00) : la partie après
  minuit compte pour le lendemain.
- Heure contrôlée : l'heure actuelle de l'établissement pour une commande
  immédiate, et l'heure demandée (`estimated_ready`) pour une commande programmée
  (`is_scheduled = true`). Le front envoie toujours un `estimated_ready`, même
  pour une commande immédiate (c'est alors une estimation de préparation) : seul
  `is_scheduled` indique qu'il s'agit de l'heure choisie par le client.
- Refus : `CreateOrderSNO` renvoie le statut `delivery_closed`. Ce contrôle passe
  avant celui de la zone de livraison.

> Constaté le 2026-10-18, non corrigé ici : le contrôle d'ouverture existant
> (`pos_closed`) ne s'exécute que si `estimated_ready` est vide. Or le front
> l'envoie toujours, donc côté serveur il ne s'applique pas aux commandes
> ScanNOrder actuelles. Le contrôle des horaires de livraison, lui, s'exécute
> dans tous les cas.

## Périmètre : ScanNOrder uniquement

La limite ne s'applique qu'aux commandes ScanNOrder. **Le POS l'ignore
volontairement** : une livraison prise à la caisse (par téléphone, par exemple)
reste possible pendant toutes les heures d'ouverture. Le statut ouvert/fermé du
POS ne change pas. La borne, Uber Eats et Deliveroo ne sont pas concernés non
plus.

En conséquence :

- la table `delivery_hours` est lue seulement par `internal/modules/scannorder` ;
- `hours_of_operation`, `openinghours`, `GetPOSStatus`, `orders` et
  `order_life_cycle` ne sont pas modifiés ;
- le test `TestDeliveryHours_OnlyReadByScanNOrder` échoue si `delivery_hours`
  ou `isDeliveryOpenAt` apparaît ailleurs dans `internal/`.

## Données

Migration `migrations/todo/185_delivery_hours.up.sql` : table `delivery_hours`
(`merchant_id`, `day_of_week` de 1 = lundi à 7 = dimanche, `hour_from`,
`hour_to`). Il n'y a ni id fonctionnel ni soft delete : le back-office remplace
toute la semaine d'un coup.

**Ordre de déploiement : migration AVANT le code.** `CreateOrderSNO` lit la table
pour chaque commande `DELIVERY` (`error_005` si la lecture échoue). `GetMerchant`
la lit aussi, mais en cas d'échec il dégrade sans erreur : pas de créneaux
exposés, `delivery_open_now = true`.

Une table séparée a été préférée à une colonne « type » dans
`hours_of_operation` : cette table est lue par le POS, les réservations et le
calcul d'ouverture, qu'il aurait fallu filtrer partout.

## API

| Route | Usage |
|---|---|
| `GET /pos/settings/delivery_hours` | Back-office : `{"delivery_hours": [{"day_of_week": 2, "hour_from": "11:30", "hour_to": "14:00"}, …]}` |
| `PUT /pos/settings/delivery_hours` | Back-office : remplace toute la semaine (même corps). Une liste vide supprime la restriction. Exige `settings.manage`. Invalide le cache `GetMerchant` de l'établissement. |
| `GET /scannorder/{slug}` | Ajoute `delivery_hours` (même forme que `status.open_hours`, absent sans créneau) et `delivery_open_now` (heure actuelle dans un créneau ; `true` sans créneau ; ne tient pas compte de `status.is_open`). |
| `POST /scannorder/{slug}/orders` | Nouveau statut `delivery_closed`. |

Validation (`normalizeDeliveryHours`) : jour de 1 à 7, heures `HH:MM` (ou
`HH:MM:SS`), début différent de la fin. Sinon : `400 invalid_input`. Les
chevauchements sont tolérés : ils s'additionnent.

Code : `internal/modules/scannorder/delivery_hours.go` (règle, réglages),
`repository.go` (`GetDeliveryHours`, `ReplaceDeliveryHours`), contrôle dans
`CreateOrderSNO` (`case "DELIVERY"`).

## Back-office (wello-back-office)

Paramètres de l'établissement, onglet **Livraison**, carte « Horaires de
livraison » (`src/components/settings/DeliveryHours.tsx`) :

- un interrupteur « Limiter les horaires de livraison ». Désactivé, on enregistre
  une liste vide. Activé, la semaine est préremplie avec 19:00–22:30 tous les
  jours, à ajuster ;
- par jour : interrupteur (désactivé = pas de livraison ce jour-là), plusieurs
  créneaux, « Copier à tous ».

L'interrupteur principal évite une ambiguïté : sans lui, « tous les jours
désactivés » voudrait dire « aucune restriction » et non « pas de livraison ».
Activer l'interrupteur sans aucun créneau est refusé : pour couper totalement
la livraison, il faut passer par l'onglet Prise de commande.

## Front ScanNOrder (wello-resto-scannorder)

`src/lib/deliveryHours.ts` reprend la règle de l'API, qui reste seule juge.
Dans `CheckoutFlow` :

- hors créneau, en livraison, l'option « Standard » est désactivée avec les
  horaires du jour, et l'option « Planifier » est sélectionnée si les commandes
  programmées sont actives ;
- les créneaux planifiés hors horaires de livraison sont grisés (`GET /slots`
  ne connaît pas le mode de commande) ;
- le statut `delivery_closed` affiche un message avec les horaires du jour, et
  le panier est conservé.

## Hors périmètre (choix assumés)

- Pas d'horaires de livraison spécifiques aux jours fériés ou aux vacances : les
  fermetures existantes s'appliquent, puisque la livraison exige que
  l'établissement soit ouvert.
- `POST /pricing` ne signale pas `delivery_closed` : c'est le front qui prévient
  avant la validation, puis la création de commande refuse.
- `GET /slots` reste indépendant du mode de commande.

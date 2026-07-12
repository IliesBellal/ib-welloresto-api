# Contrat PublicDeliverySession

Structure retournée inline dans `GET /scannorder/{slug}/orders/{id}` sous le
champ `delivery_session`. Destinée à un client web public (SNO) authentifié
uniquement par le QR code de la commande.

Définie dans [internal/modules/scannorder/models.go](../../internal/modules/scannorder/models.go)
(`PublicDeliverySession`, `PublicDeliveryMan`). Assemblée par
`toPublicDeliverySession()` dans
[internal/modules/scannorder/service.go](../../internal/modules/scannorder/service.go).
Résolue via `Repository.GetDeliverySessionByOrderID` (voir §"Fix" dans
[docs/decisions.md](../decisions.md)).

Contexte : issue du hotfix RGPD du 2026-07-02 (voir `docs/decisions.md`
section "🔴 HOTFIX RGPD" et
[docs/audits/2026-07-02-order-tracking-page-refonte.md](../audits/2026-07-02-order-tracking-page-refonte.md)
section 10). Ce document fige le contrat pour la refonte visuelle SNO qui
suivra — pas de nouvel endpoint, `PublicDeliverySession` reste inline
(Option B).

## Champs exposés

| Champ | Type | Description | Exemple | Fréquence de MAJ |
|---|---|---|---|---|
| `delivery_session_id` | `string` | Identifiant de la session de livraison. | `"482"` | Fixe (identifiant de la session, pas de la position). |
| `status` | `string` | Statut de la session : `active`, `done` ou `canceled` (voir migration `035_delivery_session_status_normalization`). | `"active"` | À chaque changement de statut (start/close/cancel). |
| `delivery_man.first_name` | `string \| null` | Prénom du livreur uniquement. | `"Karim"` | Statique pour la durée de la session. |
| `delivery_man.lat` | `float \| null` | Latitude live du livreur. | `48.8566` | ~30s (cadence d'écriture de l'app driver, voir `users.lat`/`users.last_position_at`). |
| `delivery_man.lng` | `float \| null` | Longitude live du livreur. | `2.3522` | ~30s (idem `lat`). |
| `delivery_man.status` | `string \| null` | Statut de disponibilité du livreur (`user_status_view.status`). | `"busy"` | À chaque changement de statut. |
| `stops_before_you` | `int \| null` | Nombre de stops non-terminaux (`pending`/`en_route`/`arrived`) avant le stop du client courant dans la tournée, par priorité. `null` si la commande n'est pas trouvée dans la tournée. | `2` | À chaque avancée de la tournée (arrêt marqué delivered/failed/canceled). |
| `total_stops` | `int \| null` | Nombre total de stops de la tournée (compteur uniquement). | `5` | Fixe pour la durée de la session (les stops ne sont ni ajoutés ni retirés après dispatch). |

Tous les champs `delivery_man.*` sont `null` si aucun livreur n'est encore
assigné à la session (ne devrait pas arriver en pratique : une session n'est
créée qu'avec un `user_id` livreur).

## Champs NON exposés (et pourquoi)

Champs de `models.DeliverySession` (et modèles associés) volontairement
absents de `PublicDeliverySession` :

| Champ interne | Raison |
|---|---|
| `orders[]` | Contient les commandes de **tous les autres clients** de la tournée (nom, adresse, téléphone, email, GPS, notes de livraison). C'est la fuite RGPD corrigée par le hotfix — ne jamais réintroduire ce champ tel quel côté public. |
| `delivery_man.last_name` | Identification indirecte du livreur (couplé au prénom + position live, suffisant pour l'identifier). |
| `delivery_man.user_id` | Identifiant interne — permettrait de croiser avec d'autres données du livreur. |
| `delivery_man.phone` | Donnée personnelle. Note : le modèle interne (`models.OrderUser`) ne sélectionne même pas ce champ aujourd'hui pour la tournée — mentionné ici pour mémoire si un futur ajout au modèle interne l'expose, il ne doit pas être recopié dans `PublicDeliveryMan`. |
| `distance` / `duration` | Figés à la création de la session (`StartDeliverySession`), jamais recalculés en cours de tournée. Obsolètes dès le premier stop traité — ne pas les exposer comme s'ils reflétaient l'état courant. Le SNO doit calculer sa propre ETA (voir plus bas). |
| `user_id` (session) | Identifiant interne de session/livreur, pas nécessaire au client et potentiellement corrélable. |
| `merchant_id` | Déjà connu du SNO via le slug de l'URL — redondant et inutile côté client public. |
| `start_date` | Non nécessaire au tracking ; `status` suffit à savoir si la session est en cours. |
| `delivery_man.profile_picture` / `planning_color` | Champs internes RH/planning (`models.OrderUser`), sans utilité ni légitimité pour un client public. |

## Sources de données pour le SNO

Le SNO reconstitue la carte de tracking à partir de trois sources :

- **Restaurant** : `useMerchant().address.lat/lng` (chargé au montage de la
  page, non répété dans `PublicDeliverySession`).
- **Client courant** : `Order.Customer.customer_lat`/`customer_lng` (déjà
  retourné par `GET /scannorder/{slug}/orders/{id}` au niveau racine de
  l'order — vérifié sur le payload d'une commande DELIVERY réelle, aucune
  modification nécessaire côté `Order.Customer`).
- **Livreur** : `PublicDeliverySession.delivery_man.lat/lng` (mis à jour
  toutes les ~30 secondes côté DB par l'app driver).

## Calcul ETA côté SNO

L'ETA n'est **pas** calculé côté backend. Le SNO calcule l'ETA à partir de :

- Position actuelle du livreur (temps réel, `delivery_man.lat/lng`).
- Adresse client (statique, connue depuis `Order.Customer`).
- Polyline OSRM appelée directement depuis le client SNO (instance publique
  `router.project-osrm.org`, cohérent avec le pattern déjà utilisé côté POS
  Flutter pour le calcul d'itinéraire livreur).

Pattern de calcul recommandé (à titre indicatif, à implémenter côté SNO) :
distance restante = somme des segments non parcourus de la polyline depuis la
position interpolée courante jusqu'au client. Vitesse moyenne = duration
OSRM totale / distance OSRM totale (constante par tronçon). ETA = distance
restante / vitesse moyenne.

Cette approche évite de rappeler OSRM à chaque tick — un seul appel OSRM au
chargement + retry uniquement si la position livreur dévie significativement
de la polyline connue (le POS Flutter matérialise ce seuil de déviation via
`kOnRouteThresholdMeters` = 45 m, voir pattern de reroute ci-dessous).

## Fréquence de polling recommandée

- **Position livreur** : 30 secondes — côté DB, l'app driver écrit à cette
  cadence (`users.lat`/`lng`/`last_position_at`) ; un polling client plus
  fréquent n'apporte rien.
- **Reste du payload** : 10 secondes — cadence actuelle du polling
  `GET /scannorder/{slug}/orders/{id}` côté SNO, conservée.

Ces deux cadences peuvent être découplées côté SNO : un polling `GET order`
toutes les 10 secondes suffit à récupérer aussi la position livreur (qui aura
été mise à jour au plus tôt 30 secondes plus tôt en DB).

## Interpolation côté SNO

Sans interpolation, le marqueur livreur se téléporte tous les 10-30 secondes.
Le SNO doit interpoler la position visuelle du marqueur le long de la
polyline OSRM entre deux positions API reçues, sur une durée de 30 secondes
(matchée à la cadence de mise à jour DB).

Pattern de référence côté POS Flutter (repo `wello_resto_flutter`) :
[lib/ui/widgets/delivery/driver_marker_animation.dart](../../../wello_resto_flutter/lib/ui/widgets/delivery/driver_marker_animation.dart)
(classe `DriverMarkerAnimation`), consommé par `driver_navigation_map.dart`.

Points clés à reproduire :

- **Retarget depuis la position visuelle courante**, pas depuis la dernière
  position API : `retarget()` prend `departure = currentPosition` (position
  interpolée en cours d'animation), pas `targetPosition` — évite les sauts
  si une nouvelle position arrive avant la fin de l'animation précédente.
- **Interpolation par distance d'arc le long du path** (`sampleAlongPath` +
  `pathCumulativeDistances`), pas de lerp lat/lng naïf : la position glisse à
  vitesse visuelle constante le long du vrai tracé (polyline OSRM projetée),
  pas en ligne droite à travers les bâtiments.
- **Bearing recalculé à chaque frame** depuis le segment courant du path
  (`_bearingDegrees(a, b)` sur le segment échantillonné) — pas de `heading`
  fourni par le backend, le calcul est entièrement côté client.
- **No-op si déplacement sub-métrique** : `retarget()` ignore tout nouveau
  target à moins de 0,5 m de l'ancien (bruit GPS), évite de relancer une
  animation pour rien.

## Améliorations non incluses dans ce contrat (à traiter séparément)

- Champ `heading` côté modèle interne (nécessite migration + mise à jour de
  l'app driver — chantier séparé).
- Champ `last_position_at` exposé côté modèle public (la colonne
  `users.last_position_at` existe déjà côté interne, migration 032 ; il
  manque juste sa lecture/exposition) — permettrait un indicateur de
  staleness côté SNO (ex. "position non mise à jour depuis Xs").
- Centralisation des appels OSRM côté backend (chantier long terme, quand un
  OSRM auto-hébergé sera en place — évite de dépendre de l'instance publique
  `router.project-osrm.org` pour un usage production).

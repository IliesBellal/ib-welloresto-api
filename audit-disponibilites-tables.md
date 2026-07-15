# Audit — Disponibilités tables pour le plan de salle

Lecture seule. Aucune modification apportée.

## 1. Fenêtre temporelle de `nextActiveBooking()`

- Le filtre temporel est appliqué en amont, dans la requête SQL qui charge les bookings, pas dans `nextActiveBooking()` elle-même : [internal/modules/locations/repository.go:89-90](internal/modules/locations/repository.go#L89-L90)
  ```sql
  WHERE b.merchant_id = ? AND b.status = 'ACCEPTED'
  AND b.booking_date_to > UTC_TIMESTAMP - INTERVAL 5 HOUR
  ```
  Ce n'est pas une fenêtre glissante "prochaines N heures" : c'est une borne basse uniquement (exclut les réservations terminées depuis plus de 5h). Il n'y a **pas de borne haute** — une réservation demain, la semaine prochaine, etc. est chargée tout autant qu'une réservation dans l'heure.
- Statuts filtrés : uniquement `ACCEPTED` ([repository.go:89](internal/modules/locations/repository.go#L89)). `PENDING` est exclu.
- Sélection finale par table : `nextActiveBooking()` ([repository.go:204-233](internal/modules/locations/repository.go#L204-L233)) prend, parmi toutes les réservations `ACCEPTED` chargées pour la table (qui peuvent donc s'étaler sur plusieurs jours), celle dont `BookingDateFrom` est **la plus petite** ([repository.go:210-213](internal/modules/locations/repository.go#L210-L213)), via comparaison de chaînes au format `"2006-01-02 15:04:05"` (correct lexicographiquement pour ce format, mais confirme l'absence de logique "future la plus proche" : une réservation passée (encore dans la fenêtre des 5h) est préférée à une réservation future si son `BookingDateFrom` est antérieur).

## 2. Données retournées par table

Struct `LocationBooking` ([internal/models/orders_model.go:106-112](internal/models/orders_model.go#L106-L112)) :
```go
type LocationBooking struct {
    BookingID     string `json:"booking_id"`
    BookingNumber string `json:"booking_number"`
    PartySize     int    `json:"party_size"`
    StartsAt      string `json:"starts_at"` // ISO 8601 UTC
    CustomerName  string `json:"customer_name"`
}
```
Tous ces champs sont effectivement renseignés par `nextActiveBooking()` ([repository.go:226-232](internal/modules/locations/repository.go#L226-L232)).

- **`starts_at`** : conversion explicite en RFC3339 UTC ([repository.go:216-219](internal/modules/locations/repository.go#L216-L219)) :
  ```go
  startsAt := best.BookingDateFrom
  if t, err := time.Parse("2006-01-02 15:04:05", best.BookingDateFrom); err == nil {
      startsAt = t.UTC().Format(time.RFC3339)
  }
  ```
  Exemple attendu : `2026-07-13T19:30:00Z`. Point d'attention : si le parsing échoue, `startsAt` retombe silencieusement sur la chaîne brute `"YYYY-MM-DD HH:MM:SS"` (non-ISO) sans signalement d'erreur.
- **`customer_name`** : la requête bookings fait un `INNER JOIN customer c ON c.customer_id = b.customer_id` ([repository.go:88](internal/modules/locations/repository.go#L88)) — un booking sans client associé serait donc **exclu de la requête entière**, pas renvoyé avec un nom vide. En revanche `c.customer_name` n'a pas de `COALESCE` (contrairement à `customer_tel`, ligne 85) : si un client existant a un nom NULL en base, `customerName` sera mis à `""` par `nextActiveBooking()` ([repository.go:221-224](internal/modules/locations/repository.go#L221-L224)) — c'est le seul cas où le champ peut être vide.

## 3. Données manquantes pour les états enrichis

- **A. "Libre"** (`open_order_id == null && booking == null`) : suffisant tel quel — `OpenOrderID` ([internal/models/orders_model.go:96](internal/models/orders_model.go#L96)) et `Booking` ([orders_model.go:99](internal/models/orders_model.go#L99)) sont deux champs indépendants du même struct `Location`.
- **B/C. "Réservée bientôt" / "réservée plus tard"** : `starts_at` est bien un RFC3339 parseable côté client dans le cas nominal (cf. §2). Le calcul du seuil (< 30 min, etc.) est entièrement délégué au client — l'API n'expose aucun seuil ni classification pré-calculée.
- **D. "Occupée + réservation imminente"** : confirmé au point A — `OpenOrderID` et `Booking` sont indépendants dans le code, rien ne les rend mutuellement exclusifs ; ils peuvent coexister non-nil dans la même réponse pour une même table.
- **E. Heure de service actuel** : `GET /services/{device_id}` retourne `PerformedService{ServiceID, StartDate, EndDate}` ([internal/models/create_order_models.go:157-168](internal/models/create_order_models.go#L157-L168)), mais c'est une route **distincte** de `GET /locations` — aucun champ de service/horaire n'est inclus dans la réponse `/locations` elle-même. Le client devrait faire un appel séparé (`/services/{device_id}`) et corréler côté client pour contextualiser "bientôt" par rapport au service en cours.

## 4. Couverture des tables sans étage

- `locations.floor_id` est nullable en base : `floor_id INT NULL` ([migrations/done/050_baseline_floorplan.up.sql:27](migrations/done/050_baseline_floorplan.up.sql#L27)).
- La requête `queryLocs` de `GetLocations` ([internal/modules/locations/repository.go:34-50](internal/modules/locations/repository.go#L34-L50)) ne filtre pas sur `floor_id` (seulement `merchant_id` et `enabled`) — les tables avec `floor_id == null` sont donc **retournées** par `GET /locations`, avec `FloorID` sérialisé comme chaîne vide côté JSON (`json:"floor_id,omitempty"`, [orders_model.go:89](internal/models/orders_model.go#L89)).

## 5. Fréquence de rafraîchissement

- Route unique déclarée : `GET /locations` → `locationsH.GetLocations` ([cmd/api/routes.go:936](cmd/api/routes.go#L936)), pas de variante `/stream` ni de souscription.
- Aucune référence à "location" trouvée dans `internal/infrastructure/websocket/` : le hub WebSocket existant sert les événements de commandes, pas les tables/plan de salle.
- Aucun mécanisme FCM/push lié aux locations trouvé dans le module `locations`.
- Conclusion : `GET /locations` est conçu pour un appel à l'ouverture du plan de salle et/ou du polling côté client (POS) — il n'existe aucun mécanisme temps réel notifiant un changement de statut de table (nouvelle réservation, ouverture/fermeture de commande sur une table).

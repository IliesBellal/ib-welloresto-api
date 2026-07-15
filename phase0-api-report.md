# Phase 0 — État des lieux `ib-welloresto-api` — Plan de salle & tables

> Lecture seule. Aucune modification de code n'a été apportée pour produire ce rapport.
> Référentiel de schéma pris pour hypothèse : celui fourni en prompt (héritage PHP, aucun DDL dans le repo au moment de l'écriture du schéma — voir §1.1 pour une précision sur ce point, une migration de documentation existe depuis).

---

## Axe 1 — Modèle de données

### 1.1 Colonnes lues/écrites par table

Précision préalable : contrairement à l'hypothèse du prompt, une migration **documentaire** des 5 tables existe déjà dans le repo : [migrations/done/050_baseline_floorplan.up.sql](migrations/done/050_baseline_floorplan.up.sql). Son en-tête (lignes 1-14) précise explicitement qu'elle est *inférée* des requêtes Go et n'a jamais été appliquée en création sur la prod (les `CREATE TABLE IF NOT EXISTS` y sont des no-op sur une base où les tables préexistent). Elle ne fait donc pas foi sur le schéma réel, mais documente déjà la même intention que ce rapport. Une seconde migration, [052_booked_location_unique.up.sql](migrations/done/052_booked_location_unique.up.sql), a dédoublonné `booked_location` et posé une contrainte `UNIQUE (booking_id, location_id)`.

#### `locations`

Colonnes lues dans `GetLocations` — [internal/modules/locations/repository.go:32-47](internal/modules/locations/repository.go#L32-L47) :
`location_id, location_name, location_desc (COALESCE '' ), seats, location_order, floor_id, shape, current_x, current_y, current_width, current_height, angle` + jointure calculée `order_id`/`available`.

Colonnes écrites à la création — `INSERT INTO locations` [internal/modules/locations/repository.go:172-190](internal/modules/locations/repository.go#L172-L190) :
`merchant_id, floor_id, location_name, seats, shape, current_x, current_y, current_width, current_height, angle, location_order, enabled(=TRUE en dur)`.

Colonnes écrites à la mise à jour — `UPDATE locations SET …` [internal/modules/locations/repository.go:208-223](internal/modules/locations/repository.go#L208-L223) :
`location_name, location_order, floor_id, seats, shape, current_x, current_y, current_width, current_height, angle, enabled` (toutes en `COALESCE(?, colonne)`, payload partiel supporté). La requête est portée par [UpdateTableRequest](internal/modules/locations/models.go#L14-L27), qui déclare bien `Seats *int` (ligne 18) et `Shape *string` (ligne 19).

Colonne présente en base mais **ignorée en écriture** : `location_desc` — lue (ligne 34 ci-dessus, avec `COALESCE(l.location_desc, '')`) mais absente de `CreateTableRequest` ([internal/modules/locations/models.go:3-12](internal/modules/locations/models.go#L3-L12)) et de `UpdateTableRequest` (aucun champ `LocationDesc` dans [internal/modules/locations/models.go:14-27](internal/modules/locations/models.go#L14-L27)) — dead column du point de vue de l'API (lecture seule, jamais réécrite par aucun endpoint du repo).

Pas de colonne payload absente du SET pour `seats`/`shape` — voir §1.2 pour la vérification détaillée.

#### `floors`

Colonnes lues — [internal/modules/locations/repository.go:134](internal/modules/locations/repository.go#L134) : `SELECT id, name FROM floors WHERE merchant_id = ? AND enabled IS TRUE` (seulement `id`/`name`, pas de tri/ordre déclaré côté schéma fourni de toute façon).

Colonnes écrites à la création — [internal/modules/locations/repository.go:305-310](internal/modules/locations/repository.go#L305-L310) : `INSERT INTO floors (merchant_id, name, enabled) VALUES (?, ?, TRUE)`.

Colonnes écrites à la mise à jour — [internal/modules/locations/repository.go:250-253](internal/modules/locations/repository.go#L250-L253) : `UPDATE floors SET name = ? WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`.

Suppression (soft delete) — [internal/modules/locations/repository.go:290-293](internal/modules/locations/repository.go#L290-L293) : `UPDATE floors SET enabled = FALSE WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`, protégée par un contrôle préalable de tables actives ([repository.go:278-288](internal/modules/locations/repository.go#L278-L288), retourne `ErrFloorNotEmpty` si `COUNT(*) > 0` sur `locations` actives du floor).

Aucune colonne de `floors` n'est ignorée : le schéma fourni (`id, merchant_id, name, creation_date, enabled`) est intégralement couvert par `merchant_id` (scoping), `name` (C/R/U), `enabled` (soft delete). `creation_date` n'est jamais lue ni écrite explicitement (valeur par défaut `current_timestamp()` côté SQL, jamais retournée dans les réponses JSON) — dead column côté API.

#### `floor_areas`

Colonnes lues — [internal/modules/locations/repository.go:145-149](internal/modules/locations/repository.go#L145-L149) :
`SELECT fa.id, fa.floor_id, fa.name, fa.points, fa.x, fa.y, fa.angle, fa.stroke_color, fa.color FROM floor_areas fa INNER JOIN floors f ON f.id = fa.floor_id WHERE f.merchant_id = ? AND fa.enabled IS TRUE AND f.enabled IS TRUE`. Toutes les colonnes du schéma fourni sont lues, sauf `creation_date` (jamais utilisée).

Aucune écriture : voir §1.3 pour la vérification exhaustive (aucun `INSERT`/`UPDATE`/`DELETE` sur `floor_areas` dans tout le repo Go).

#### `order_location`

Lue — jointure dans `GetLocations` [internal/modules/locations/repository.go:39-45](internal/modules/locations/repository.go#L39-L45) (sous-requête filtrant les commandes non `DELETED/DONE/CANCELED/CLOSED`) ; jointure dans `orders` pour l'affichage des tables sur une commande [internal/modules/orders/orders_fetcher_builder.go:41-44](internal/modules/orders/orders_fetcher_builder.go#L41-L44) ; jointure dans `scannorder` pour résoudre la commande ouverte d'une table scannée [internal/modules/scannorder/repository.go:46-47](internal/modules/scannorder/repository.go#L46-L47) ; jointure dans `order_life_cycle` pour la résolution QR→commande [internal/modules/order_life_cycle/repository.go:861](internal/modules/order_life_cycle/repository.go#L861) et [internal/modules/order_life_cycle/repository.go:942](internal/modules/order_life_cycle/repository.go#L942).

Écrite — `DELETE FROM order_location WHERE order_id = ?` puis réinsertion bulk à chaque mise à jour de commande, [internal/modules/order_life_cycle/repository.go:1493-1503](internal/modules/order_life_cycle/repository.go#L1493-L1503) ; insertion dédiée à la création via `InsertOrderLocations`, [internal/modules/order_life_cycle/repository.go:1131-1150](internal/modules/order_life_cycle/repository.go#L1131-L1150) (bulk `INSERT INTO order_location (order_id, location_id) VALUES (?, ?), (?, ?)…`, ligne 1150).

Seules les deux colonnes du schéma fourni (`order_id`, `location_id`) sont utilisées — aucune colonne ignorée.

#### `booked_location`

Lue — jointure dans `GetLocations` [internal/modules/locations/repository.go:69-79](internal/modules/locations/repository.go#L69-L79) (résas `ACCEPTED` avec fenêtre `booking_date_to > UTC_TIMESTAMP - INTERVAL 5 HOUR`, ligne 78-79) ; jointure dans `bookings` pour l'affichage des tables d'une résa [internal/modules/bookings/repository.go:618](internal/modules/bookings/repository.go#L618) et [internal/modules/bookings/repository.go:828](internal/modules/bookings/repository.go#L828) ; contrôle de conflit [internal/modules/bookings/repository.go:775-785](internal/modules/bookings/repository.go#L775-L785) (voir §Axe 4) ; fetcher liste [internal/modules/bookings/bookings_fetcher.go:127](internal/modules/bookings/bookings_fetcher.go#L127) ; résolution table→résa en cours côté ScannOrder [internal/modules/scannorder/repository.go:614](internal/modules/scannorder/repository.go#L614).

Écrite — remplacement complet (delete puis insert) via `ReplaceBookingLocations`, [internal/modules/bookings/repository.go:1084-1111](internal/modules/bookings/repository.go#L1084-L1111) : `DELETE … FROM booked_location bl INNER JOIN bookings b …` (lignes 1087-1092) puis boucle d'`INSERT INTO booked_location(booking_id, location_id)` (lignes 1101-1104). Une seconde voie d'écriture existe à la création staff d'une réservation, [internal/modules/bookings/repository.go:732](internal/modules/bookings/repository.go#L732) (`INSERT INTO booked_location(booking_id, location_id)`), utilisée uniquement par le flux `POST /bookings/create` (staff).

Seules les colonnes du schéma fourni (`booking_id`, `location_id`) sont manipulées ; `id` (PK auto-incrément) n'est jamais lu ni référencé côté Go.

### 1.2 Vérification du bug de persistance `seats`/`shape`

**Le bug décrit dans le brief n'est plus reproductible dans l'état actuel du code.**

- `PATCH /locations/tables/{location_id}` accepte bien `seats` et `shape` dans le body : [internal/modules/locations/models.go:18-19](internal/modules/locations/models.go#L18-L19) (`Seats *int`, `Shape *string` dans `UpdateTableRequest`).
- Ils sont bien présents dans le `SET` de l'`UPDATE` : [internal/modules/locations/repository.go:214-215](internal/modules/locations/repository.go#L214-L215) (`seats = COALESCE(?, seats), shape = COALESCE(?, shape)`), et bindés dans les arguments d'exécution à [internal/modules/locations/repository.go:226](internal/modules/locations/repository.go#L226).
- Une validation applicative existe en amont, dans le service : [internal/modules/locations/service.go:34-56](internal/modules/locations/service.go#L34-L56) (`validateTableGeometry`), appelée à la fois pour `CreateTable` ([service.go:64](internal/modules/locations/service.go#L64)) et `UpdateTable` ([service.go:85](internal/modules/locations/service.go#L85)) : `seats` doit être `≥ 1` (ligne 48-50), `shape` doit être dans `{circle, square, rectangle}` (ligne 51-53, cf. `validTableShapes` ligne 29), et `x/y` (0-1000), `width/height` (40-300), `angle` (0-359) sont bornés (lignes 39-47).

Le rapport `audit-tables-plan-de-salle.md` (racine du repo, §1.5 et §5.3, daté 2026-07-04) documente ce bug comme actif à cette date — l'écart avec l'état actuel indique que le correctif (persistance + validation géométrique) a été appliqué **depuis** cette date. Aucune trace de correctif dans un commit spécifique n'a été recherchée (hors périmètre lecture-seule de ce rapport).

### 1.3 CRUD `floor_areas`

**Lecture seule.** Recherche exhaustive de `floor_areas` dans tout le repo Go (`internal/`, `cmd/`) : deux occurrences seulement — [internal/modules/locations/repository.go:147](internal/modules/locations/repository.go#L147) (le `FROM floor_areas fa` de la requête `GetLocations`, seule occurrence de code applicatif) et [internal/modules/bookings/conflict_test.go:322](internal/modules/bookings/conflict_test.go#L322) (liste de tables à vider entre tests). Aucun `INSERT INTO floor_areas`, `UPDATE floor_areas` ni `DELETE FROM floor_areas` n'existe dans le repo. Aucun handler, service ou route n'expose de CRUD sur cette table — elle n'est atteignable que via `GET /locations` (champ `areas[]` de la réponse, [internal/modules/locations/repository.go:22-25](internal/modules/locations/repository.go#L22-L25) et [144-159](internal/modules/locations/repository.go#L144-L159)).

Qui crée les `floor_areas` en prod : indéterminable depuis ce repo (aucun outil d'édition ni endpoint d'écriture dans `ib-welloresto-api`). L'absence totale de chemin d'écriture Go indique soit un legacy PHP disparu, soit une écriture directe en base — non vérifiable en lecture seule du présent dépôt.

### 1.4 Éléments absents du schéma / du code

Vérifié par recherche exhaustive (`grep` sur `internal/`, `cmd/`, `migrations/`) :

| Élément recherché | Constat |
|---|---|
| Table `table_combinations` / `table_combination_members` | Absente. Aucune occurrence dans le code Go ni dans les migrations. Aucune notion de combinaison de tables nulle part dans le repo. |
| Table `floor_obstacles` | Absente. Aucune occurrence. |
| Colonne `attributes` JSON sur `locations` | Absente. `locations` n'a que les colonnes du schéma fourni (confirmé par le `SELECT`/`INSERT`/`UPDATE` exhaustifs de §1.1) ; aucune migration `ALTER TABLE locations` n'existe dans `migrations/done/` (recherche `ALTER TABLE locations` : 0 résultat). |
| Colonnes `name` VARCHAR + `rules` JSON sur `floor_areas` | `name` existe déjà et est lue ([repository.go:146](internal/modules/locations/repository.go#L146), champ `fa.name`) — conforme au schéma fourni, qui la déclare déjà. `rules` JSON : absente, aucune occurrence. |
| Colonne `out_of_service` (ou tout statut autre que `enabled`) sur `locations` | Absente. `enabled` (soft delete) est la seule colonne de statut lue/écrite (§1.1). Aucune migration n'ajoute de colonne de statut à `locations`. |

Aucune migration touchant `locations`, `floors` ou `floor_areas` au-delà de la baseline documentaire (050) n'existe dans `migrations/done/` (recherche `ALTER TABLE locations|floors|floor_areas` : 0 résultat ; seule `052_booked_location_unique` modifie une table de liaison du périmètre).

### 1.5 Impact d'une migration VARCHAR des IDs existants

Occurrences du littéral `location_id` (snake_case, JSON tags + SQL) dans les fichiers `.go` : **69 occurrences dans 24 fichiers**. Occurrences du littéral `floor_id` : **21 occurrences dans 7 fichiers**. Détail par fichier :

**`location_id`** — [cmd/api/routes.go](cmd/api/routes.go) (2), [internal/models/create_order_models.go](internal/models/create_order_models.go) (1), [internal/models/orders_model.go](internal/models/orders_model.go) (1), [internal/webhook/deliveroo_orders/repository.go](internal/webhook/deliveroo_orders/repository.go) (3), [internal/webhook/deliveroo_orders/deliveroo_models.go](internal/webhook/deliveroo_orders/deliveroo_models.go) (1), [internal/modules/auth/login_response.go](internal/modules/auth/login_response.go) (1), [internal/modules/auth/repository.go](internal/modules/auth/repository.go) (3), [internal/webhook/deliveroo_menu/repository.go](internal/webhook/deliveroo_menu/repository.go) (1), [internal/modules/bookings/bookings_fetcher.go](internal/modules/bookings/bookings_fetcher.go) (2), [internal/modules/bookings/conflict_test.go](internal/modules/bookings/conflict_test.go) (6), [internal/modules/deliveroo/repository.go](internal/modules/deliveroo/repository.go) (1), [internal/modules/bookings/models.go](internal/modules/bookings/models.go) (3), [internal/modules/bookings/repository.go](internal/modules/bookings/repository.go) (7), [internal/modules/locations/service.go](internal/modules/locations/service.go) (1), [internal/modules/kiosk/models.go](internal/modules/kiosk/models.go) (3), [internal/modules/locations/repository.go](internal/modules/locations/repository.go) (6), [internal/modules/kiosk/service.go](internal/modules/kiosk/service.go) (1), [internal/modules/orders/orders_fetcher_builder.go](internal/modules/orders/orders_fetcher_builder.go) (2), [internal/modules/kiosk/repository.go](internal/modules/kiosk/repository.go) (7), [internal/modules/locations/handler.go](internal/modules/locations/handler.go) (4), [internal/modules/order_life_cycle/repository.go](internal/modules/order_life_cycle/repository.go) (5), [internal/modules/users/repository.go](internal/modules/users/repository.go) (1), [internal/modules/scannorder/models.go](internal/modules/scannorder/models.go) (1), [internal/modules/scannorder/repository.go](internal/modules/scannorder/repository.go) (6).

**`floor_id`** — [cmd/api/routes.go](cmd/api/routes.go) (3), [internal/models/orders_model.go](internal/models/orders_model.go) (2), [internal/modules/bookings/models.go](internal/modules/bookings/models.go) (1), [internal/modules/locations/models.go](internal/modules/locations/models.go) (1), [internal/modules/locations/service.go](internal/modules/locations/service.go) (1), [internal/modules/locations/repository.go](internal/modules/locations/repository.go) (7), [internal/modules/locations/handler.go](internal/modules/locations/handler.go) (6).

Occurrences de l'identifiant Go `LocationID` (champs de struct / variables) : **53 occurrences dans 28 fichiers** ; `FloorID` : **7 occurrences dans 4 fichiers** (`internal/models/orders_model.go`, `internal/modules/bookings/models.go`, `internal/modules/locations/models.go`, `internal/modules/locations/repository.go`). `AreaID` : aucune occurrence (le modèle `Area` n'a pas de champ nommé `AreaID`, son identifiant est `ID`, voir §Axe 4).

**Constat structurant pour l'évaluation d'impact** (fait, aucune recommandation) : les structs Go partagés typent déjà ces identifiants en `string`, pas en `int` — [internal/models/orders_model.go:75](internal/models/orders_model.go#L75) (`Location.LocationID string`), [internal/models/orders_model.go:82](internal/models/orders_model.go#L82) (`Location.FloorID string`), [internal/models/orders_model.go:101](internal/models/orders_model.go#L101) (`Floor.ID string`), [internal/models/orders_model.go:106](internal/models/orders_model.go#L106) (`Area.ID string`). Le driver `database/sql` scanne d'ores et déjà les colonnes `INT` de `locations.location_id`/`floors.id` vers ces champs `string` (ex. `l.LocationID` dans `Scan()`, [internal/modules/locations/repository.go:57-60](internal/modules/locations/repository.go#L57-L60)) et les requêtes paramétrées (`?`) passent ces valeurs en `interface{}` sans typage SQL explicite côté Go.

---

## Axe 4 — Endpoints existants liés aux tables et au plan de salle

Router : [cmd/api/routes.go](cmd/api/routes.go). Handlers wirés lignes [356-357](cmd/api/routes.go#L356-L357) (repo/service locations) et [442](cmd/api/routes.go#L442) (handler locations).

### `locations`

| Endpoint | Handler | Statut |
|---|---|---|
| `GET /locations` | [locationsH.GetLocations](internal/modules/locations/handler.go#L24-L38) → [LocationsService.GetLocations](internal/modules/locations/service.go#L19-L26) → [LocationsRepository.GetLocations](internal/modules/locations/repository.go#L20-L167), déclaré [routes.go:924](cmd/api/routes.go#L924) | ✅ fonctionnel |
| `POST /locations/floors/{floor_id}/tables` | [locationsH.CreateTable](internal/modules/locations/handler.go#L40-L66) → [LocationsService.CreateTable](internal/modules/locations/service.go#L58-L77) → [LocationsRepository.CreateTable](internal/modules/locations/repository.go#L169-L202), déclaré [routes.go:927](cmd/api/routes.go#L927) | ✅ fonctionnel |
| `PATCH /locations/tables/{location_id}` | [locationsH.UpdateTable](internal/modules/locations/handler.go#L68-L94) → [LocationsService.UpdateTable](internal/modules/locations/service.go#L79-L97) → [LocationsRepository.UpdateTable](internal/modules/locations/repository.go#L204-L232), déclaré [routes.go:928](cmd/api/routes.go#L928) | ✅ fonctionnel (voir §1.2 — `seats`/`shape` persistés) |
| `DELETE /locations/tables/{location_id}` | [locationsH.DeleteTable](internal/modules/locations/handler.go#L96-L116) → [LocationsService.DeleteTable](internal/modules/locations/service.go#L99-L113) → [LocationsRepository.DeleteTable](internal/modules/locations/repository.go#L234-L245) (soft delete), déclaré [routes.go:929](cmd/api/routes.go#L929) | ✅ fonctionnel |

**`PATCH /locations/{id}/coordinates`** : n'existe **pas** dans le router actuel. Recherche exhaustive du terme `coordinates` dans tout le repo Go (`internal/`, `cmd/`) : deux occurrences, toutes deux sans rapport (`internal/modules/scannorder/service.go:1019` — commentaire sur des coordonnées GPS de livraison ; `internal/modules/users/repository.go:75,77` — coordonnées de livraison). Aucun endpoint `/coordinates` n'est déclaré dans `routes.go`. La géométrie d'une table (x, y, width, height, angle) n'est donc mise à jour que par `PATCH /locations/tables/{location_id}` — c'est bien l'unique endpoint qui écrit la géométrie.

### `floors`

| Endpoint | Handler | Statut |
|---|---|---|
| `POST /floors` | [locationsH.CreateFloor](internal/modules/locations/handler.go#L118-L143) → [LocationsService.CreateFloor](internal/modules/locations/service.go#L115-L130) → [LocationsRepository.CreateFloor](internal/modules/locations/repository.go#L305-L321), déclaré [routes.go:915](cmd/api/routes.go#L915) | ✅ fonctionnel |
| `PATCH /floors/{floor_id}` | [locationsH.UpdateFloor](internal/modules/locations/handler.go#L145-L176) → [LocationsService.UpdateFloor](internal/modules/locations/service.go#L132-L146) → [LocationsRepository.UpdateFloor](internal/modules/locations/repository.go#L247-L273), déclaré [routes.go:916](cmd/api/routes.go#L916) | ✅ fonctionnel — renomme le floor, distingue « nom déjà identique » de « floor introuvable » via une lecture de contrôle ([repository.go:258-270](internal/modules/locations/repository.go#L258-L270)) |
| `DELETE /floors/{floor_id}` | [locationsH.DeleteFloor](internal/modules/locations/handler.go#L178-L198) → [LocationsService.DeleteFloor](internal/modules/locations/service.go#L148-L162) → [LocationsRepository.DeleteFloor](internal/modules/locations/repository.go#L275-L303), déclaré [routes.go:917](cmd/api/routes.go#L917) | ✅ fonctionnel — soft delete, refuse si des tables actives existent encore sur le floor ([repository.go:278-288](internal/modules/locations/repository.go#L278-L288), erreur `ErrFloorNotEmpty`) |

Les deux endpoints (`PATCH`/`DELETE /floors/{id}`) sont déclarés et disposent d'un handler complet — l'écart signalé par `audit-tables-plan-de-salle.md` (§2.1-2.2, « endpoints fantômes ») ne correspond plus à l'état actuel du code.

### `floor_areas`

Aucun endpoint dédié. Seule exposition : imbriquées dans `GET /locations`, champ `areas[]` de `models.LocationResponse` ([internal/models/orders_model.go:94-98](internal/models/orders_model.go#L94-L98) pour la définition du champ, alimenté [internal/modules/locations/repository.go:144-159](internal/modules/locations/repository.go#L144-L159)). Colonnes renvoyées : `id, floor_id, name, points, x, y, angle, stroke_color, color` (mapping exact vers [models.Area](internal/models/orders_model.go#L105-L115), toutes les colonnes de la requête SQL — voir §1.1 — sont reprises dans la réponse JSON). Statut : ❌ pas de route déclarée pour créer/modifier/supprimer une zone (aucune route `floor_areas` ni `areas` dans `routes.go`).

### `booked_location`

Pas d'endpoint direct sur la table elle-même ; exposée via le module `bookings` :

| Endpoint | Handler | Ce qu'il fait | Statut |
|---|---|---|---|
| `POST /bookings/create` | [bookingsH.CreateBooking](internal/modules/bookings/handler.go), service → [BookingsRepository] `INSERT INTO booked_location` [repository.go:732](internal/modules/bookings/repository.go#L732) | Création staff d'une résa avec tables (`booking.locations[]`) | ✅ fonctionnel |
| `PATCH /bookings/{booking_id}/locations` | [bookingsH.AssignBookingLocations](internal/modules/bookings/handler.go#L269-L299) → [BookingsService.AssignBookingLocations](internal/modules/bookings/service.go#L375-L423), déclaré [routes.go:1101](cmd/api/routes.go#L1101) | (Ré)affecte les tables d'une résa **existante** : contrôle de conflit ([FindConflictingBookings](internal/modules/bookings/repository.go#L756-L801), requête verrouillante `FOR UPDATE` ligne 785, filtre les résas `PENDING_APPROVAL/ACCEPTED/ORDER_OPEN` en chevauchement de créneau) puis remplacement transactionnel des lignes ([ReplaceBookingLocations](internal/modules/bookings/repository.go#L1084-L1111)) | ✅ fonctionnel — c'est l'endpoint d'affectation de tables à une réservation existante que le prompt demande de rechercher |

**Constat structurant** : cet endpoint répond directement au gap documenté dans `audit-tables-plan-de-salle.md` (§3, « aucun moyen d'affecter, modifier ou retirer les tables d'une réservation existante ») — il n'existait pas au 2026-07-04 (`AssignBookingLocations` n'apparaît dans aucun `git blame`/historique consulté ici, mais est absent de la description de l'audit) et existe dans l'état actuel du code, avec un contrôle de conflit table×créneau dédié (absent lui aussi de la description de l'audit à cette date).

Contrainte SQL complémentaire (hors du flux applicatif) : [migrations/done/052_booked_location_unique.up.sql:26-27](migrations/done/052_booked_location_unique.up.sql#L26-L27) ajoute `UNIQUE (booking_id, location_id)` sur `booked_location`, empêchant un doublon exact (même table affectée deux fois à la même résa) — distinct du contrôle de chevauchement de créneau, qui reste applicatif (transaction + `FOR UPDATE`).

### `order_location`

Pas d'endpoint dédié — écrite exclusivement via les endpoints de gestion de commande du module `order_life_cycle` (hors périmètre géométrique du plan de salle). Écriture : `InsertOrderLocations` [internal/modules/order_life_cycle/repository.go:1131-1150](internal/modules/order_life_cycle/repository.go#L1131-L1150) (création) et `DELETE`+réinsertion bulk [internal/modules/order_life_cycle/repository.go:1493-1503](internal/modules/order_life_cycle/repository.go#L1493-L1503) (mise à jour, remplacement total à chaque édition de commande).

### RBAC

Aucun des groupes de routes `/locations` ([routes.go:921-930](cmd/api/routes.go#L921-L930)) et `/floors` ([routes.go:912-918](cmd/api/routes.go#L912-L918)) n'applique de middleware de permission au-delà de `authMiddleware` (authentification simple). Recherche de `RequirePermission`/helpers `middleware.Has*` référençant locations/floors/tables/bookings dans [internal/middleware/permissions.go](internal/middleware/permissions.go) : aucun résultat — confirmé également pour le groupe `/bookings` ([routes.go:1076-1109](cmd/api/routes.go#L1076-L1109)), y compris la route `PATCH /{booking_id}/locations` ([routes.go:1101](cmd/api/routes.go#L1101)), qui ne porte pas de contrôle de permission additionnel.

---

## Axe 5 — Consommateurs actuels du plan de salle

### `locations`

Modules internes lecteurs : `locations` (source, `GET /locations`) ; `bookings` — `loadMerchantLocations` [internal/modules/bookings/repository.go:1624-1648](internal/modules/bookings/repository.go#L1624-L1648), utilisée par `GET /bookings/availability/{date}` (ne sélectionne que `location_id, location_name, location_desc` — les colonnes de géométrie/capacité déclarées dans le struct [bookings.Location](internal/modules/bookings/models.go#L37-L53) (`Seats, Shape, X, Y, W, H, Angle, OpenOrderID, Available`) ne sont **jamais peuplées** par ce chemin, elles restent à leur valeur zéro Go dans la réponse JSON de disponibilité) ; `orders` — jointure `location_name`/`location_desc` pour l'affichage des tables sur une commande [internal/modules/orders/orders_fetcher_builder.go:41-44](internal/modules/orders/orders_fetcher_builder.go#L41-L44), résolue **au moment de la lecture** (pas de dénormalisation à la clôture) ; `scannorder` — résolution table→commande ouverte via jointure `locations`/`qrcodes`/`order_location` [internal/modules/scannorder/repository.go:34-47](internal/modules/scannorder/repository.go#L34-L47).

Modules internes écrivains : uniquement `locations` (CRUD complet, §Axe 4). Aucun autre module n'écrit dans `locations`.

Routes publiques vs staff : toutes les routes exposant `locations` (`GET /locations`, `POST/PATCH/DELETE /locations/tables/{id}`, `POST /locations/floors/{floor_id}/tables`) sont sous authentification staff ([routes.go:921-930](cmd/api/routes.go#L921-L930), `r.Use(authMiddleware)`). Le flux public de réservation (`/rsv/{slug}/...`, [routes.go:1112-1130](cmd/api/routes.go#L1112-L1130)) n'expose aucune route liée à `locations` — confirmé par l'absence de toute référence à `location_id`/table dans le module `reservation` (les seules occurrences de « location » dans `internal/modules/reservation/*.go` concernent `time.Location`, l'API de fuseau horaire Go, sans rapport avec les tables — ex. [internal/modules/reservation/service.go:136](internal/modules/reservation/service.go#L136), [internal/modules/reservation/repository.go:473-486](internal/modules/reservation/repository.go#L473-L486)).

### `floors`

Lecteurs internes : `locations` (`GET /locations`, liste imbriquée). Écrivains : `locations` uniquement (CRUD complet, §Axe 4). Aucune route publique ; toutes sous `authMiddleware` ([routes.go:912-918](cmd/api/routes.go#L912-L918)).

### `floor_areas`

Lecteur unique : `locations`, imbriqué dans `GET /locations` (§Axe 4). Aucun écrivain dans le repo (§1.3). Aucune route publique — accessible uniquement via `GET /locations` (staff).

### `booked_location`

Lecteurs internes : `locations` (`GET /locations`, résas par table) ; `bookings` (affichage des tables d'une résa, contrôle de conflit) ; `scannorder` (résolution de la résa en cours pour une table scannée, [internal/modules/scannorder/repository.go:614](internal/modules/scannorder/repository.go#L614)). Écrivains : `bookings` uniquement (création staff + `AssignBookingLocations`, §Axe 4). Le module `reservation` (flux public) n'écrit jamais dans `booked_location` — confirmé par l'absence totale de référence aux tables dans ce module (voir plus haut) : une réservation publique ne se voit donc jamais attribuer de table à la création, contrairement à la création staff (`POST /bookings/create`).

Routes : staff uniquement (`/bookings/...`, [routes.go:1076-1109](cmd/api/routes.go#L1076-L1109)) — rien côté public.

### `order_location`

Lecteurs/écrivains internes : `order_life_cycle` (écrivain exclusif, §Axe 4) ; `orders` (lecture pour affichage/historique, [orders_fetcher_builder.go:41-44](internal/modules/orders/orders_fetcher_builder.go#L41-L44)) ; `scannorder` (lecture pour résoudre la commande ouverte d'une table, [scannorder/repository.go:46-47](internal/modules/scannorder/repository.go#L46-L47)) ; `locations` (lecture, occupation d'une table dans `GET /locations`, [locations/repository.go:39-45](internal/modules/locations/repository.go#L39-L45)).

### Références `location_id` côté kiosk / scannorder (module API)

**`kiosk`** : `kiosks.location_id` est une colonne à part — métadonnée d'appareil, nullable, sans rapport avec les tables du plan de salle. Lue dans les 3 requêtes de listing/détail de bornes : [internal/modules/kiosk/repository.go:184](internal/modules/kiosk/repository.go#L184), [211](internal/modules/kiosk/repository.go#L211), [235](internal/modules/kiosk/repository.go#L235) (colonne `location_id`, scannée dans `row.LocationID`, ex. ligne [191](internal/modules/kiosk/repository.go#L191)). Aucune logique métier ne l'exploite au-delà du stockage/restitution — pas de jointure vers `locations`. À distinguer de `qrcodes.location_id` (référencé ligne [586](internal/modules/kiosk/repository.go#L586), `getMerchantSlug`, filtre `AND location_id IS NULL` pour isoler le QR racine du merchant) et de `stripe_accounts.terminal_location_id` (concept Stripe Terminal, sans rapport, [internal/modules/kiosk/repository.go:505-514](internal/modules/kiosk/repository.go#L505-L514)) — trois entités homonymes distinctes.

**`scannorder`** : `qrcodes.location_id` résout la table scannée vers sa commande ouverte et ses métadonnées ([internal/modules/scannorder/repository.go:34-47](internal/modules/scannorder/repository.go#L34-L47), jointure `locations l on l.location_id = qr.location_id`) et vers sa réservation en cours ([repository.go:614](internal/modules/scannorder/repository.go#L614), jointure `booked_location bc ON bc.location_id = qr.location_id`). `LocationID` est restitué au client ScannOrder dans la réponse merchant/QR ([internal/modules/scannorder/models.go:59](internal/modules/scannorder/models.go#L59), [internal/modules/scannorder/service.go:174](internal/modules/scannorder/service.go#L174)). C'est le seul module exposant `location_id` sur une route non authentifiée (flux client ScannOrder) — à vérifier séparément si une politique RBAC/scoping spécifique s'y applique (hors périmètre de ce rapport, qui couvre le plan de salle staff).

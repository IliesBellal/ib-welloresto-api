# 25 — Journal de conversion Tier 2 (dbx + vérification réelle Postgres)

Conversion des 16 modules Tier 2 de [07-module-inventory.md](07-module-inventory.md) vers
l'infrastructure `internal/database/dbx` ([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)),
même méthodologie que le [rapport 14 (Tier 1)](14-tier1-conversion-log.md) : chaque module vérifié
par un test d'intégration réel (tag de build `postgres_integration`) contre le Postgres Docker de
dev (`localhost:5433`, base `welloresto_dev`), avec données de test insérées puis nettoyées par le
test.

**Ordre traité** (simple → complexe) : translation, webhook/brevo_sms_reply, tags, discounts,
integrations, notification, availabilities, webhook/ubereats, webhook/deliveroo_orders, stats,
deliveroo, reservation, locations, webhook/stripe, auth, stocks.

**Commande de vérification** (par module) :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/modules/<module>/...
```

**Résultat global : 16/16 modules convertis et verts.** `go build ./...` OK ; suite unitaire
complète : liste d'échecs strictement identique à la baseline préexistante (`bookingcomm`,
`planning/employees`, `planning/leave`, `planning/swaps`, `pos/accounting` build failed,
`ubereats` build failed — tous hors Tier 2, pré-existants, non touchés par ce chantier).

## Tableau de synthèse

| # | Module | Statut | Fichiers modifiés | Test réel Postgres | Écarts non prévus par l'audit |
|---|---|---|---|---|---|
| 1 | `translation` | ✅ | `repository.go` (+ test) | CRUD traductions + upsert vérifié | 2 sites `ON DUPLICATE KEY UPDATE` → `ON CONFLICT DO UPDATE` (branche dialecte) |
| 2 | `webhook/brevo_sms_reply` | ✅ | `repository.go` (+ test) | Insert + relecture réponse SMS OK | — |
| 3 | `tags` | ✅ | `repository.go` (+ test) | CRUD tags OK | — |
| 4 | `discounts` | ✅ | `repository.go` (+ test) | CRUD + soft delete OK | Divergence MySQL "changed rows" vs Postgres "matched rows" sur `RowsAffected` dans `DeleteDiscount` — corrigé en ajoutant `AND enabled = true` au `WHERE` du soft-delete |
| 5 | `integrations` | ✅ | `repository.go` (+ test) | Settings Uber Eats/Deliveroo OK | `COALESCE(iue.estimated_preparation_time, 0)` — colonne varchar vs littéral int → littéral `'0'`. `UpdateUberEatsSettings` liait un `int` Go directement sur colonne varchar → `strconv.Itoa()` |
| 6 | `notification` | ✅ | `notification_repository.go` (+ test) | CRUD notifications OK | — |
| 7 | `availabilities` | ✅ | `repository.go` (+ test) | Placeholders dynamiques `IN` (via `strings.Repeat`) confirmés correctement rebindés par `dbx.GetDB`/`sqlx.Rebind` en conditions réelles | — |
| 8 | `webhook/ubereats` | ✅ | `repository/{attribute_mapping_repo,orders_repo,product_mapping_repo}.go` (+ test) | Mapping produits/attributs + orders OK | `CreateAttributeFromUberGroup` insérait dans un PK varchar sans défaut puis relisait via un `LastInsertId()` impossible en pgx — **corrigé** (validé utilisateur) par génération d'ID côté client (`helpers.GeneratePrefixedID(helpers.AttributeIDPrefix)`). Bug préexistant identique aux deux dialectes dans la même fonction : `configurable_attributes.product_id` NOT NULL jamais renseigné — **documenté, non corrigé** (validé utilisateur) |
| 9 | `webhook/deliveroo_orders` | ✅ | `repository.go` (+ test) | Sync commandes/produits OK | Même bug d'ID identity appliqué sans nouvelle question (précédent établi au module 8). Jointures `merchant.id` (integer) vs `*.merchant_id` (varchar) corrigées via `CAST`. `SyncProduct` : colonne `category` NOT NULL jamais renseignée — bug préexistant identique aux deux dialectes, **documenté, non corrigé** |
| 10 | `stats` | ✅ | `repository.go` (+ test) | Stats CA/upsell avec agrégations et fuseaux horaires OK | **Découverte critique** : `CONVERT_TZ(x, '+00:00', offset)` → `AT TIME ZONE (offset::interval)` — un cast direct de l'offset en texte inverse le signe (convention POSIX) vs MySQL/ISO ; caster en `INTERVAL` corrige. `GROUP BY` référençant une expression paramétrée deux fois : chaque `?` répété crée un placeholder Postgres distinct (`$1` vs `$5`), non reconnus comme identiques → corrigé en groupant sur l'alias de sortie. Bug d'arrondi upsell HT (centimes fractionnaires faisant échouer un scan `int64`) → nouveau helper `roundToIntExpr()` |
| 11 | `deliveroo` | ✅ | `repository.go` (+ test) | CRUD intégration Deliveroo OK | `orders.dateDeparture` référencée en code mais inexistante dans les deux dialectes — bug préexistant, **documenté, non corrigé** |
| 12 | `reservation` | ✅ | `repository.go` (+ test) | Réservations + settings + transaction de création OK | Bug de jointure cross-type supplémentaire trouvé en cours de route (`bookings_settings`/`merchant_parameters` vs `merchant.id`), non repéré à l'audit initial — corrigé via le même pattern `CAST`. `CreateBooking` (code mort) : `booking_number` NOT NULL jamais renseignée — documenté. `CreateBookingTransaction` dépend transitivement du module `customers` (Tier 3, non converti) — contourné en insérant les données de test directement plutôt que de toucher un module hors périmètre |
| 13 | `locations` | ✅ | `repository.go` (+ test), `04-schema-postgres-target.sql` | CRUD floors/floor_areas/locations OK | **Correction majeure validée utilisateur** : `floors.id`, `floor_areas.id`, `locations.location_id` sont des colonnes identity auto-incrémentées, mais le code insérait des ID préfixés générés côté client → basculé sur `dbx.InsertReturningID` sur les 3 fonctions ; **change le format de l'ID retourné** (chaîne préfixée → entier simple) — **breaking change côté frontend explicitement documenté**. Bug de comptage de placeholders (`?` manquant) auto-introduit dans l'INSERT de `CreateTable`, corrigé en cours de conversion |
| 14 | `webhook/stripe` | ✅ | `repository.go` (+ test) | Paiements/webhooks Stripe, `FROM_UNIXTIME` → `to_timestamp()` vérifié en UTC | Postgres interdit `SET alias.colonne = ...` (contrairement à MySQL) — corrigé en retirant les préfixes d'alias des cibles `SET`. `InsertStripePayment` (code mort) : colonne NOT NULL `success_key` jamais renseignée — documenté |
| 15 | `auth` | ✅ | `repository.go`, `last_login_test.go`, `pin_test.go` (+ test) | Login/PIN/MFA/sessions OK ; tests existants restés verts à chaque étape | **Bug de fixtures préexistant découvert avant conversion** : un commit du 01/07 a ajouté `pos_upsell_enabled` au SELECT+Scan de `GetUserByToken`/`Login` sans mettre à jour les fixtures sqlmock de `last_login_test.go`/`pin_test.go` — corrigé. **Bug de production critique découvert via test Postgres réel** : ce même commit du 01/07 a oublié d'ajouter `mp.pos_upsell_enabled` au SELECT de `GetUserByPIN`, alors que cette fonction partage `scanUserLoginRow` (77 colonnes attendues) avec `GetUserByToken`/`Login` → **toute connexion par PIN (kiosque/POS) est cassée en production depuis ce commit** ; corrigé en ajoutant la colonne manquante. Colonne `packages.kiosks_enabled` référencée en code mais absente de la doc schéma — confirmée par l'utilisateur comme ajout récent en production, ajoutée à `04-schema-postgres-target.sql` (+ `ALTER TABLE` sur la base de dev) |
| 16 | `stocks` | ✅ | `repository.go` (+ test) | Scan code-barres, perte de stock (composant/produit), mouvements manuels, consommation de commande, historique OK | `? * ?` (deux paramètres bruts multipliés) ambigu en PG — 4 sites corrigés en pré-multipliant côté Go. `ROUND(double precision/real, integer)` n'accepte que `numeric` en 2 arguments côté PG (même lacune que `stats`) → nouveau helper `round4()`, appliqué sur ~9 sites. `COALESCE(c.unit_of_measure, '')` — colonne integer vs littéral varchar → `CAST(... AS CHAR/TEXT)`. **Divergence de comportement trouvée en test réel** : `unit_of_measure_convert.id_to` est une colonne integer ; MySQL coerce silencieusement une valeur `req.Unit` non numérique en `0` (aucune correspondance → `ErrUnitNotFound`), Postgres lève une erreur de type dure pour la même entrée — corrigé en validant `req.Unit` via `strconv.Atoi` côté Go avant la requête, pour un comportement identique aux deux dialectes face à une entrée client invalide |

## Écarts transverses (à retenir pour les Tiers 3+)

1. **`CONVERT_TZ(x, '+00:00', offset)` → `x AT TIME ZONE (offset::interval)`** : caster l'offset en
   texte seul inverse le signe (convention POSIX vs ISO/MySQL) — le cast en `INTERVAL` est
   obligatoire, pas optionnel.
2. **`GROUP BY` sur une expression paramétrée répétée** : chaque occurrence de `?` dans la requête
   devient un placeholder Postgres distinct (`$1`, `$5`, ...) même si la valeur liée est identique —
   Postgres refuse de les considérer comme la même expression de regroupement. Grouper sur l'alias
   de sortie plutôt que de répéter l'expression.
3. **`ROUND(x, n)` à 2 arguments n'accepte que `numeric` en Postgres** (double precision/real
   rejetés), contrairement à MySQL qui accepte tout type numérique — pattern déjà vu en Tier 1
   (`roundToIntExpr` dans `stats`), reproduit ici via `round4()` dans `stocks`. À anticiper dès
   qu'une colonne `double precision`/`real` traverse un `ROUND()`.
4. **`COALESCE(x, y)` exige des types compatibles** : un littéral `''` face à une colonne integer,
   ou un littéral `0` face à une colonne boolean, échouent en Postgres alors que MySQL coerce
   silencieusement. Vérifier chaque `COALESCE` touchant une colonne numérique/booléenne.
5. **`SET alias.colonne = ...` est invalide en Postgres** même à l'intérieur d'un `UPDATE table
   alias ...` — seul `SET colonne = ...` (non qualifié) est accepté.
6. **Coercition implicite MySQL de chaînes non numériques vers `0`** sur une colonne integer : ce
   n'est pas qu'un problème de génération d'ID (déjà vu en Tier 1 avec les colonnes identity) — cela
   affecte aussi les comparaisons `WHERE`/`JOIN` sur des paramètres utilisateur non numériques
   (`stocks.RecordComponentMovement`). Quand une entrée client peut être arbitraire, valider le
   format côté Go avant de la lier à une colonne integer, plutôt que de compter sur l'échec de la
   comparaison pour produire un "not found".
7. **Bugs preexistants identiques aux deux dialectes** (colonnes NOT NULL jamais renseignées dans du
   code mort/peu utilisé, ex. `configurable_attributes.product_id`, `orders.dateDeparture`,
   `bookings.booking_number`, `success_key`) : documentés sans correction, par précédent établi en
   Tier 1 — un changement de comportement fonctionnel n'est pas dans le périmètre d'une migration de
   dialecte SQL.
8. **Colonnes identity + insertion d'ID côté client** (pattern déjà vu en Tier 1 avec
   `bookingcore`) : reconfirmé sur 3 fonctions de `locations` — Postgres rejette explicitement
   l'insertion sur une colonne `GENERATED ALWAYS AS IDENTITY`, MySQL coerçait silencieusement (et
   retournait un mauvais ID). Toujours grep `LastInsertId` avant de commencer un module.

## Modifications d'infrastructure ajoutées pendant ce chantier

- `internal/modules/stats/repository.go` : + `roundToIntExpr()` (cast `ROUND()` en `numeric` pour PG)
- `internal/modules/stocks/repository.go` : + `round4()` (même pattern, dédié aux colonnes
  stock/ratio `double precision`/`real` de ce module)
- [04-schema-postgres-target.sql](04-schema-postgres-target.sql) : `packages.kiosks_enabled boolean
  NOT NULL DEFAULT false` ajoutée (colonne de production récente absente du dump DDL audité,
  confirmée par l'utilisateur)

## Bug de production critique signalé en cours de chantier

`auth.GetUserByPIN` ne sélectionnait pas `mp.pos_upsell_enabled` depuis le commit du 01/07 qui a
ajouté cette colonne à `scanUserLoginRow` (partagée avec `GetUserByToken`/`Login`) — toute connexion
par code PIN (kiosque/POS) est probablement cassée en production depuis cette date. Corrigé dans le
cadre de la conversion du module `auth` (voir ligne 15 du tableau ci-dessus) ; **à vérifier/déployer
séparément de la migration Postgres**, le bug existe identiquement en MySQL.

## Découpage en commits atomiques (préparé, non exécuté)

Un commit par module, dans l'ordre de conversion. `DB_DIALECT=mysql` reste le défaut en prod ; aucun
commit n'a été exécuté — à faire par l'utilisateur.

| Commit suggéré | Fichiers |
|---|---|
| `postgres: convert translation module to dbx` | `internal/modules/translation/*` |
| `postgres: convert brevo_sms_reply webhook to dbx` | `internal/webhook/brevo_sms_reply/*` |
| `postgres: convert tags module to dbx` | `internal/modules/tags/*` |
| `postgres: convert discounts module to dbx` | `internal/modules/discounts/*` |
| `postgres: convert integrations module to dbx` | `internal/modules/integrations/*` |
| `postgres: convert notification module to dbx` | `internal/modules/notification/*` |
| `postgres: convert availabilities module to dbx` | `internal/modules/availabilities/*` |
| `postgres: convert ubereats webhook to dbx` | `internal/webhook/ubereats/repository/*` |
| `postgres: convert deliveroo_orders webhook to dbx` | `internal/webhook/deliveroo_orders/*` |
| `postgres: convert stats module to dbx` | `internal/modules/stats/*` |
| `postgres: convert deliveroo module to dbx` | `internal/modules/deliveroo/*` |
| `postgres: convert reservation module to dbx` | `internal/modules/reservation/*` |
| `postgres: convert locations module to dbx` | `internal/modules/locations/*`, `docs/migration-postgres/04-schema-postgres-target.sql` (si non déjà inclus dans un commit précédent) |
| `postgres: convert stripe webhook to dbx` | `internal/webhook/stripe/*` |
| `postgres: fix auth test fixtures for pos_upsell_enabled` | `internal/modules/auth/last_login_test.go`, `internal/modules/auth/pin_test.go` |
| `postgres: convert auth module to dbx (fix GetUserByPIN missing pos_upsell_enabled)` | `internal/modules/auth/repository.go`, `internal/modules/auth/postgres_integration_test.go`, `docs/migration-postgres/04-schema-postgres-target.sql` (kiosks_enabled, si non déjà committé) |
| `postgres: convert stocks module to dbx` | `internal/modules/stocks/*` |
| `docs: add Tier 2 postgres conversion log` | `docs/migration-postgres/25-tier2-conversion-log.md` |

> Note : selon l'ordre réel des modifications à `04-schema-postgres-target.sql`, la colonne
> `kiosks_enabled` peut être rattachée soit au commit `locations` soit au commit `auth` — à vérifier
> par `git diff` avant de committer, le fichier n'ayant qu'une seule modification cumulative dans
> l'arbre de travail actuel.

## Hors périmètre — travail non lié détecté dans l'arbre de travail

L'arbre de travail contient également des modifications non liées à ce chantier Tier 2
(fonctionnalité "commentaires de jour" pour le planning, ajoutée plus tôt dans cette session) :
`cmd/api/routes.go`, `internal/helpers/ids.go`, `internal/models/responses_models.go`,
`internal/modules/planning/{planning_handler,planning_repository,planning_service}.go`,
`internal/modules/planning/daycomments/` (nouveau), `migrations/065_planning_day_comments.{up,down}.sql`
(nouveaux). Ces fichiers n'ont pas été touchés pendant ce chantier et ne sont volontairement listés
dans aucun commit ci-dessus — à committer séparément par l'utilisateur s'il le souhaite.

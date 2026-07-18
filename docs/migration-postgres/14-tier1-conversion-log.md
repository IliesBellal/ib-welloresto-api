# 14 — Journal de conversion Tier 1 (dbx + vérification réelle Postgres)

Conversion des 11 modules Tier 1 de [07-module-inventory.md](07-module-inventory.md) vers
l'infrastructure `internal/database/dbx` ([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)),
chacun vérifié par un test d'intégration réel (tag de build `postgres_integration`) contre le
Postgres Docker de dev (`localhost:5433`, base `welloresto_dev`), avec données de test insérées
puis nettoyées par le test.

**Commande de vérification** (par module ou globale) :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/modules/<module>/...
```

**Résultat global : 11/11 modules convertis et verts.** `go build ./...` OK ; suite unitaire
complète : liste d'échecs strictement identique à la baseline préexistante du rapport 12
(auth, bookingcomm, planning/{employees,leave,swaps}, pos/accounting, ubereats — tous hors Tier 1).

## Incident d'environnement corrigé avant messaggio

La base de dev n'était **pas** chargée avec le schéma cible à jour : les colonnes `merchant_id`
étaient encore `integer` (état antérieur au chantier 13). Rechargée intégralement depuis
[04-schema-postgres-target.sql](04-schema-postgres-target.sql) (drop schema + réimport, 181 tables),
puis **les 7 modules déjà convertis ont été re-vérifiés verts** sur le schéma corrigé.

## Tableau de synthèse

| # | Module | Statut | Fichiers modifiés | Test réel Postgres | Écarts non prévus par l'audit |
|---|---|---|---|---|---|
| 1 | `bookingevents` | ✅ | `events.go` (+ test) | Insert + relecture jsonb `metadata` OK | — |
| 2 | `webhook/deliveroo_menu` | ✅ | `repository.go` (+ test) | Lecture brand_id + `sql.ErrNoRows` OK | Accès direct `r.db` (sans dbutils) — passé par `dbx.GetDB` |
| 3 | `allergens` | ✅ | `repository.go` (+ test) | Liste avec seed OK | — |
| 4 | `receipt` | ✅ | `repository.go` (+ test) | `FOR UPDATE`, insert jsonb `[]byte`, relecture OK | `[]byte` Go → `jsonb` accepté tel quel par pgx (pas d'adaptation) |
| 5 | `user_services` | ✅ | `repository.go` (+ test) | Jointure 4 tables + service courant + device_link OK | **`sub_cash_registers.cash_register_id` était `varchar(20)` face à un PK `integer`** (incohérence héritée du MySQL source, jointure impossible en PG). Aligné en `integer` dans 04 + base de dev. `cd.enabled = 1` → `= TRUE` |
| 6 | `bookingcore` | ✅ | `create.go` (+ test) | Insert avec `RETURNING booking_id` + boucle unicité numéro OK ; tests unitaires existants verts | **`res.LastInsertId()` non supporté par pgx** → nouveau helper transverse `dbx.InsertReturningID` (Exec+LastInsertId en MySQL, `RETURNING` en PG). Insert extrait en helper `insertBooking` pour testabilité (CreateBooking dépend de `customers`, module Tier 3 non converti) |
| 7 | `audit` | ✅ | `repository.go` (+ test) | Chaînage hash 2 inserts + `FOR UPDATE` OK | `NOW()` est valide dans les deux dialectes → aucune traduction (l'audit prévoyait une réécriture). Anomalie **préexistante** (identique en MySQL) : `InsertLog` omet `hash` NOT NULL → insert échoue silencieusement (erreur avalée volontairement par le code) |
| 8 | `messaggio` | ✅ | `marketing_repository.go` (+ test) | Settings (jointure qrcodes) + upsert `ON CONFLICT` avec accumulation vérifiée (3+2=5) | `? * ?` sans contexte de type → « operator is not unique » en PG ; coût calculé côté Go. `DATE_FORMAT('%Y-%m-01')` → mois calculé côté Go (identique aux 2 dialectes). Scan `sms_enabled` passé de `int` à `bool` (compatible tinyint MySQL via `driver.Bool`) |
| 9 | `googlemaps` | ✅ | `repository.go` (+ test) | Upsert `ON CONFLICT` avec accumulation vérifiée (4+6=10) | Comme messaggio (mois en Go, upsert par dialecte). PK `(merchant_id, month)` présente en cible — pas de contrainte à ajouter |
| 10 | `printers` | ✅ | `repository.go` (+ test) | CRUD complet : create, doublon PK → `ErrInvalidInput`, list, update partiel (SET dynamique rebindé), soft delete, `ErrNotFound` | Détection 1062 en dur remplacée par `dbx.IsDuplicateEntry` (23505 couvert). `enabled = 1/0` → `TRUE`/`FALSE` |
| 11 | `upsell` | ✅ | `repository.go` (+ test) | Create/Get (jsonb, enum `channel`), acceptation + idempotence + mismatch marchand, purge `interval`, produits vedettes, settings avec cap | `DATE_SUB(NOW(), INTERVAL ? MONTH)` → branche PG `now() - (? * interval '1 month')`. Booléens `= 1` → `= TRUE` (3 colonnes) |

## Écarts transverses (à retenir pour les Tiers 2+)

1. **`LastInsertId()` interdit avec pgx** — tout insert sur colonne identity doit passer par
   `dbx.InsertReturningID`. L'audit 07 ne comptait pas ces sites ; à greper (`LastInsertId`)
   avant chaque module.
2. **`UTC_TIMESTAMP()` → `now()`** (via `dbx.UTCNow()`) : les colonnes cibles étant `timestamptz`,
   `now()` est l'instant absolu correct ; ne pas utiliser `now() AT TIME ZONE 'UTC'` (timestamp
   naïf réinterprété dans le fuseau de session). `NOW()` MySQL est déjà valide en PG tel quel.
3. **Comparaisons boolean** : `col = 1` / `= 0` → `= TRUE` / `= FALSE` (littéraux valides aussi en
   MySQL sur TINYINT(1)) ; scanner dans un `bool` Go (compatible les deux drivers).
4. **Expressions arithmétiques entre paramètres** (`? * ?`) : intraduisibles en PG sans contexte de
   type — calculer côté Go.
5. **Upserts** : pas de syntaxe commune — branche `dbx.ActiveDialect()` avec deux requêtes aux
   paramètres identiques ; vérifier que la contrainte UNIQUE/PK du `ON CONFLICT` existe en cible.
6. **Schéma de dev** : penser à recharger `welloresto_dev` après toute évolution de
   [04-schema-postgres-target.sql](04-schema-postgres-target.sql) — l'incident merchant_id
   ci-dessus venait d'un chargement obsolète.

## Modifications d'infrastructure ajoutées pendant ce chantier

- `internal/database/dbx/dialect.go` : + `UTCNow()`
- `internal/database/dbx/db.go` : + `InsertReturningID()`
- `internal/database/dbx/pgtest/pgtest.go` : helper d'ouverture pour les tests `postgres_integration`
- [04-schema-postgres-target.sql](04-schema-postgres-target.sql) : `sub_cash_registers.cash_register_id`
  `varchar(20)` → `integer` (commenté en tête de table ; migration de données : `CAST` à la copie)

## Découpage en commits atomiques (préparé, non exécuté)

Un commit par module, dans l'ordre de conversion. Fichiers par commit :

| Commit suggéré | Fichiers |
|---|---|
| `postgres: convert bookingevents module to dbx` | `internal/modules/bookingevents/*` |
| `postgres: convert deliveroo_menu webhook to dbx` | `internal/webhook/deliveroo_menu/*` |
| `postgres: convert allergens module to dbx` | `internal/modules/allergens/*` |
| `postgres: convert receipt module to dbx` | `internal/modules/receipt/*` |
| `postgres: convert user_services module to dbx` | `internal/modules/user_services/*`, `docs/migration-postgres/04-schema-postgres-target.sql` |
| `postgres: convert bookingcore module to dbx` | `internal/modules/bookingcore/*`, `internal/database/dbx/*` (UTCNow, InsertReturningID, pgtest) |
| `postgres: convert audit module to dbx` | `internal/modules/audit/*` |
| `postgres: convert messaggio module to dbx` | `internal/modules/messaggio/*` (inclut l'unification merchant_id string déjà en cours dans l'arbre) |
| `postgres: convert googlemaps module to dbx` | `internal/modules/googlemaps/*` (idem) |
| `postgres: convert printers module to dbx` | `internal/modules/printers/*` |
| `postgres: convert upsell module to dbx` | `internal/modules/upsell/*` |

> Note : `internal/database/dbx/pgtest/` est requis par le premier test d'intégration —
> si l'ordre strict est conservé, rattacher `pgtest.go` au commit bookingevents.

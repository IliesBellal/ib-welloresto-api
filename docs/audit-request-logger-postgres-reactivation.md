# Audit — Réactivation de `RequestLoggerMiddleware` pour Postgres

Date : 2026-08-14 · Branche : `staging` · Périmètre : `internal/middleware/request_logger/`, `cmd/api/routes.go`

Contexte : le middleware était désactivé depuis un commentaire (« cause des timeouts lors d'appels
API Uber Eats »), à l'époque où l'app tournait sur MySQL. La base cible est maintenant Postgres
(migration terminée en production, cf. [`docs/migration-postgres/66-prod-data-load.md`](migration-postgres/66-prod-data-load.md)).
Objectif : déterminer si la réactivation est sûre côté Postgres, et corriger ce qui doit l'être avant
de le faire — sans supposition, en vérifiant chaque affirmation dans le code et le schéma réellement
déployés.

---

## 1. Constat initial — pourquoi le middleware était cassé pour Postgres

### 1.1 Placeholders SQL incompatibles (bloquant, silencieux)

`flush()` construisait sa requête à la main avec des `?` et appelait `l.db.ExecContext` **directement**,
sans passer par `internal/database/dbx`, qui est le point d'entrée unique établi dans ce repo pour
réécrire `?` → `$1, $2, ...` selon le dialecte actif (`dbx.Rebind`, voir
[`internal/database/dbx/dialect.go:41`](../internal/database/dbx/dialect.go#L41)). Tous les autres
repositories du projet passent par `dbx.GetDB`/`dbx.Rebind` pour cette raison précise — `audit/repository.go`
en est l'exemple direct pour un cas très proche (colonnes `jsonb`, voir §1.2).

Conséquence si réactivé tel quel avec `DB_DIALECT=postgres` (actif en staging/production depuis la
bascule) : **100 % des batchs échouent** — pgx ne comprend pas `?` comme placeholder. Pas de panic
(l'erreur est catchée et loggée dans `flush()`), mais aucune ligne n'est jamais écrite, et une écriture
DB + une entrée d'erreur sont gaspillées à chaque flush (jusqu'à 1×/s sous trafic).

### 1.2 Colonne `payload` typée `jsonb`, corps de requête non garanti JSON

Schéma cible : `payload jsonb` ([`docs/migration-postgres/04-schema-postgres-target.sql:96`](migration-postgres/04-schema-postgres-target.sql#L96)),
contre `longtext + CHECK json_valid` en MySQL. Le middleware stockait le corps brut de la requête
(`io.ReadAll(r.Body)`) sans validation — or de nombreuses routes reçoivent des corps non-JSON
(`multipart/form-data` : `POST /users/profile/avatar`, `POST /uploads/haccp`, `PUT /menu/products/{id}/image`,
imports de menu, etc.).

Le point critique n'est pas seulement l'échec de la ligne concernée : `flush()` fait un **INSERT
multi-lignes en un seul batch** (jusqu'à 50 entrées). Une seule ligne à payload invalide fait échouer
**tout le batch**, y compris les 49 autres requêtes JSON valides qui l'accompagnaient.

### 1.3 `MerchantID *int64` — incompatible avec le schéma et avec le reste du code

Schéma cible : `api_request_logs.merchant_id varchar(64)` (même fichier, ligne 93) — pas `bigint`.
Cohérent avec le reste du projet : `merchant_id` est traité comme une **string** de bout en bout
partout ailleurs (`internal/models`, 21 occurrences en `string`/`*string`, 0 en entier — confirmé par
grep). Un audit antérieur avait déjà documenté ce point précis pour ce fichier :
[`docs/migration-postgres/10-merchant-id-type-scope.md:159`](migration-postgres/10-merchant-id-type-scope.md#L159).

Ce même audit note que `models.ContextUserID`/`ContextMerchantID` ne sont **jamais renseignés nulle
part dans le repo** (aucun `context.WithValue` correspondant) — donc `user_id`/`merchant_id` sont
toujours `NULL` dans `api_request_logs`, indépendamment de cette réactivation. C'est un bug préexistant,
distinct du sujet Postgres, et hors périmètre de cette intervention (nécessiterait de faire écrire ces
clés de contexte par le middleware d'auth — un changement plus large, non demandé ici).

Le type `*int64` restait néanmoins un risque latent : `id := v.(int64)` est une assertion de type
**non protégée** — si une future modification écrit un jour une string dans ce contexte (cohérent avec
le reste du code), c'est un **panic** au premier hit, pas une erreur silencieuse.

### 1.4 Ce qui n'est *plus* un problème : le dimensionnement du pool

La raison invoquée dans le commentaire d'origine (timeouts Uber Eats) est cohérente avec l'ancien pool
MySQL — **une seule connexion ouverte** (`internal/database/mysql.go`, contrainte d'hébergement
Hostinger) partagée entre toute l'app et les flush périodiques du logger. Le pool Postgres actuel
autorise **15 connexions ouvertes / 4 idle** ([`internal/database/postgres.go:18`](../internal/database/postgres.go#L18)),
ce qui change fondamentalement le risque de contention : un flush toutes les secondes sur un pool de 15
n'est structurellement plus comparable à la situation à 1 connexion qui avait motivé la désactivation.

---

## 2. Modifications appliquées

| Fichier | Changement | Pourquoi |
|---|---|---|
| [`internal/middleware/request_logger/logger.go`](../internal/middleware/request_logger/logger.go) | `stmt := dbx.Rebind(...)` avant `ExecContext` | Corrige §1.1 — placeholders `$N` sur Postgres, inchangé sur MySQL |
| [`internal/middleware/request_logger/middleware.go`](../internal/middleware/request_logger/middleware.go) | `payload` validé via `json.Valid` ; sinon `{"non_json_body_bytes":N}` | Corrige §1.2 — ne poison plus jamais le batch, évite aussi de stocker des blobs binaires bruts en base |
| `middleware.go` | Assertions de type en forme `v, ok := x.(T)` pour `UserID`/`MerchantID` | Corrige §1.3 — supprime le panic latent, sans dépendre du fait que le contexte est aujourd'hui toujours vide |
| [`internal/middleware/request_logger/models.go`](../internal/middleware/request_logger/models.go) | `LogEntry.MerchantID` : `*int64` → `*string` | Aligne sur le schéma réel (`varchar(64)`) et sur la convention du reste du code |
| [`cmd/api/routes.go`](../cmd/api/routes.go) | Middleware réactivé (`r.Use(...)`), import décommenté, commentaire remis à jour | Objet de la demande, une fois §1.1–1.3 traités |

Le comportement en cas d'échec d'écriture reste inchangé et volontairement non bloquant : `flush()` logue
l'erreur et continue (`l.log.Error("request log flush failed", ...)`), sans jamais faire remonter
d'erreur vers le handler HTTP appelant ni impacter la requête métier. Conforme à la consigne : perdre un
enregistrement d'audit pour ne pas risquer un crash serveur est le comportement voulu, et il préexistait
à cette intervention — non modifié ici.

---

## 3. Vérifications effectuées

- `go build ./...` — OK.
- `go vet ./internal/middleware/request_logger/...` — OK (le seul avertissement `go vet` du dépôt,
  sur `cmd/api`, porte sur `auth.AuthService`/`singleflight.Group` et est préexistant, sans lien avec
  ce changement).
- Deux tests unitaires ajoutés dans
  [`internal/middleware/request_logger/logger_test.go`](../internal/middleware/request_logger/logger_test.go)
  (via `sqlmock`, sans dépendance à une base réelle) :
  - `TestFlush_Postgres_RebindsPlaceholders` — `DB_DIALECT=postgres` ⇒ la requête reçue par le driver
    utilise bien `$1..$7`, et le flush ne marque pas d'échec.
  - `TestFlush_MySQL_KeepsQuestionMarkPlaceholders` — non-régression : `?` inchangé en MySQL.
  - Les deux **passent** (`go test ./internal/middleware/request_logger/... -v`).
- Un test d'intégration Postgres a été ajouté suivant la convention du repo (tag
  `postgres_integration`, cf. [`internal/modules/audit/postgres_integration_test.go`](../internal/modules/audit/postgres_integration_test.go)) :
  [`internal/middleware/request_logger/postgres_integration_test.go`](../internal/middleware/request_logger/postgres_integration_test.go).
  **Non exécuté ici** — Docker Desktop n'était pas démarré dans cet environnement et je ne l'ai pas
  lancé de ma propre initiative (action d'infrastructure locale, pas strictement nécessaire à la
  correction). À lancer avant déploiement pour la vérification la plus forte :
  ```
  docker compose -f docker-compose.postgres.yml up -d
  DB_DIALECT=postgres POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev \
    go test -tags postgres_integration ./internal/middleware/request_logger/...
  ```

---

## 4. Risques résiduels assumés (non traités, hors périmètre)

- **`user_id`/`merchant_id` toujours `NULL`** dans `api_request_logs` (cf. §1.3) : préexistant, pas une
  régression de cette intervention. Le corriger demanderait de faire écrire ces valeurs dans le contexte
  de requête par le middleware d'auth — changement plus large, à traiter séparément si le besoin métier
  (traçabilité par utilisateur/marchand) le justifie.
- **Contenu sensible dans `payload`** : le corps brut des requêtes JSON (mots de passe en clair sur
  `/auth/login`, PIN, éventuellement des données de paiement) est stocké tel quel dans une table de logs,
  sans rédaction. Ce comportement existait déjà avant la désactivation d'origine (pas introduit par ce
  changement) — signalé ici pour visibilité, pas corrigé, une politique de rédaction par route serait un
  changement de portée distincte.
- **Lecture intégrale du corps en mémoire** (`io.ReadAll(r.Body)`) pour chaque requête, y compris les
  uploads volumineux (documents HACCP, images, imports de menu) : comportement préexistant, non modifié.
  Le payload non-JSON n'est plus *stocké* en base (§1.2), mais il est toujours *lu* intégralement en
  mémoire côté middleware avant d'être rejeté.

## 5. Rollback

Recommenter la ligne `r.Use(requestlogger.RequestLoggerMiddleware(...))` et son import dans
`cmd/api/routes.go` suffit à revenir à l'état désactivé ; aucune migration de schéma n'a été touchée.

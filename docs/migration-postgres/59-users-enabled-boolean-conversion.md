# 59 — Conversion de `users.enabled` en boolean

Statut : **appliqué côté code et schéma cible, validé contre un rechargement Postgres complet
avec les données réelles**. La migration MySQL réelle (`migrations/071_*.up/down.sql`) est
**préparée mais non exécutée** — elle reste à lancer séparément sur la base MySQL réelle, hors
pic de trafic, après validation. Rien n'a été commité.

## Contexte

`users.enabled` est aujourd'hui la seule colonne `enabled`-nommée du module `users` restée
`integer` côté cible alors que ~100 autres colonnes `enabled` du schéma sont `boolean`
([`04-schema-postgres-target.sql:3826`](04-schema-postgres-target.sql)). Ce n'est pas un oubli :
le plan initial ([`04-schema-mapping-notes.md:31`](04-schema-mapping-notes.md)) prévoyait
explicitement `users.enabled` en boolean (« `u.enabled = 0` → `= false` » dans la vue
`user_status_view`) — une exception manuelle à la règle générale « `INT(n)` → `INTEGER`, la
largeur d'affichage n'a aucune sémantique » ([`04-schema-mapping-notes.md:12`](04-schema-mapping-notes.md)),
puisque la colonne source est `int(11)` et non `tinyint(1)`.

Le Tier 3 a fait marche arrière sur ce point. Le sondage préalable pgx du rapport
[27-tier3-conversion-log.md:27-38](27-tier3-conversion-log.md) a établi empiriquement qu'un
`bool` Go ne peut pas se lier sur une colonne `integer` Postgres (erreur d'encodage dure), alors
que MySQL accepte silencieusement les deux. Plutôt que de retyper la colonne, la décision prise à
ce moment-là ([27-tier3-conversion-log.md:47](27-tier3-conversion-log.md)) a été de **garder
`users.enabled` en integer côté cible** et de convertir le filtre Go correspondant en 0/1 explicite
— un contournement, pas une décision définitive sur le type. Ce ticket revient sur ce
contournement maintenant que les données réelles ont été auditées.

## 1. Distribution réelle des valeurs (dump réel, agrégat uniquement)

Table source : `data-migration/migration_welloresto_data.sql` (dump `mysqldump` réel, gitignored,
jamais commité). DDL confirmée ligne 598138 : `` `enabled` int(11) NOT NULL DEFAULT 1 `` — colonne
**NOT NULL**, donc `NULL` n'est pas une valeur possible.

4 instructions `INSERT INTO \`users\`` dans le dump (1 bloc de 40 lignes + 3 inserts unitaires
ultérieurs) = 46 lignes au total, soit l'export complet de la table `users`. Comptage exact par
parsing du dump (tuples MySQL décodés avec gestion des échappements de chaînes), aucune ligne
citée :

| Valeur | Occurrences |
|---|---|
| `1` | 43 |
| `0` | 3 |
| **Total** | **46** |

**Aucune valeur hors `{0, 1}`.** Contrairement à `customer.is_migrated` (334/7337 lignes à `127`,
cf. [33-sql-output-generation.md:135-143](33-sql-output-generation.md)), il n'y a ici aucun cas à
arbitrer : la colonne est propre à 100 % sur l'export réel.

## 2. Le code Go donne-t-il un sens à une valeur hors {0,1} ? (méthodologie is_migrated=127)

Précision utile avant de comparer les deux cas : `is_migrated=127` n'a pas été résolu en donnant
un sens métier à `127` — le grep exhaustif du rapport
[35-dead-columns-removal.md:16-29,92-110](35-dead-columns-removal.md) a montré que **le Go ne lit
jamais cette colonne**, ni en SQL ni en JSON ; elle a donc été jugée morte et retirée du schéma
cible plutôt que convertie. Ce n'est pas un précédent de « valeur exotique interprétée par le
code », c'est un précédent de « colonne non lue, donc écartée ».

`users.enabled` est le cas inverse : c'est une colonne **activement lue sur le chemin de
connexion** (voir § 3). Ceci a une conséquence directe et vérifiable sur l'absence de valeurs
exotiques, indépendante du seul échantillon de 46 lignes du dump :

- `internal/modules/auth/repository.go:165` et `:340` (`GetUserByToken`, requêtes de login) ainsi
  que `internal/modules/users/admin_repository.go:519,561` scannent `u.enabled` **directement**
  dans un champ Go `bool` (`auth/models.go:129` `Enabled bool`, `users/models.go:18` `Enabled
  bool`).
- Le package standard `database/sql` (`convertAssignRows` → `driver.Bool.ConvertValue`) n'accepte,
  pour une source entière scannée vers `*bool`, **que les valeurs `0` et `1`** ; toute autre valeur
  entière produit une erreur de scan dure (`sql: Scan error ... couldn't convert N into type
  bool`), qui remonterait immédiatement comme un échec de connexion pour l'utilisateur concerné.
- Autrement dit : si `users.enabled` contenait une valeur hors `{0,1}` en production aujourd'hui,
  côté MySQL, cet utilisateur ne pourrait déjà plus se connecter. Le système impose déjà
  implicitement l'invariant `{0,1}` — contrairement à `is_migrated`, qui pouvait dériver
  silencieusement faute d'être jamais lu.

**Conclusion** : l'invariant `{0,1}` est corroboré par deux sources indépendantes — l'export réel
(46/46 lignes) et le comportement du code Go en production (toute valeur hors `{0,1}` y
provoquerait déjà une erreur visible). Il n'y a pas de « cas 127 » à arbitrer pour cette colonne.

## 3. Sites Go touchant `users.enabled`

### Écriture

Aucun site de production n'écrit une valeur dans `users.enabled` :

- `internal/modules/users/create_repository.go:14-22` (`CreateUser`, `INSERT INTO users`) —
  `enabled` **absent** de la liste de colonnes ; la ligne reçoit le `DEFAULT 1` de la table.
- Le seul site qui **filtre** sur `users.enabled` avec une valeur posée côté Go est le
  contournement Tier 3 : `internal/modules/users/admin_repository.go:33-42` (`ListMerchantUsers`),
  qui convertit un `*bool` en `0`/`1` avant de le lier — précisément le code à retirer.
- `internal/modules/planning/employees/repository.go:132` compare `u.enabled = 1` en dur (filtre
  WHERE, pas une écriture) — reliquat du même contournement, à passer en `= TRUE` par cohérence
  (le reste du fichier utilise déjà `p.enabled = TRUE` / `ur.enabled = TRUE`).
- Deux fixtures de test d'intégration posent `enabled = 1` littéralement :
  `internal/modules/auth/postgres_integration_test.go:80-83` et
  `internal/modules/stocks/postgres_integration_test.go:104-107`. Aucune ne pose une valeur hors
  `{0,1}`.

### Lecture

Toutes les lectures scannent la colonne dans un `bool` Go (déjà listées § 2) ou la comparent à un
littéral `0`/`1`/`TRUE` dans une clause `WHERE` :

- `internal/modules/auth/repository.go:56,220,435` (`SELECT ... u.enabled`)
- `internal/modules/users/repository.go:177`
- `internal/modules/users/admin_repository.go:70,129,198` (`SELECT`), `519,561` (`Scan`)
- `internal/modules/planning/employees/repository.go:132` (`WHERE ... u.enabled = 1`)

**Aucun site, écriture ou lecture, ne pose ni ne tolère une valeur hors `{0,1}`.**

## 4. Décision

Le point 4 du mandat (« si toutes les valeurs hors 0/1 sont des erreurs sans signification
métier, proposer leur conversion ») ne s'applique pas ici : il n'existe **aucune valeur hors
`{0,1}`** à convertir, ni dans l'export réel ni tolérée par le code. La conversion en boolean est
directe, sans table de correspondance ni cas particulier à documenter.

## 5. Artefacts — état appliqué

### 5.1 Schéma cible — appliqué dans `04-schema-postgres-target.sql`

```diff
--- a/docs/migration-postgres/04-schema-postgres-target.sql
+++ b/docs/migration-postgres/04-schema-postgres-target.sql
@@ CREATE TABLE users (
-    enabled integer NOT NULL DEFAULT 1,
+    enabled boolean NOT NULL DEFAULT true,
@@ CREATE VIEW user_status_view AS
     CASE
-        WHEN u.enabled = 0 THEN 'DISABLED'
+        WHEN u.enabled = false THEN 'DISABLED'
```

Aligne la colonne sur le plan initial de `04-schema-mapping-notes.md:31` et sur le pattern des
~100 autres colonnes `enabled boolean` du fichier. Les deux autres exceptions `enabled integer`
du fichier (`configurable_attribute_options.enabled` ligne 792, `planned_shifts.enabled` ligne
2570) sont hors périmètre de ce ticket et restent inchangées.

**Revalidé avec le parseur Postgres réel** (`pglast`/libpg_query, comme la validation d'origine
du rapport 04) : **467 statements parsés sans erreur** sur le fichier modifié.

### 5.2 Migration MySQL réelle — `migrations/071_users_enabled_boolean.up.sql` / `.down.sql`

**Fichiers créés dans le repo, non exécutés** (aucune connexion à la base MySQL réelle n'a été
faite). Numérotation confirmée libre : dernière migration présente avant ce ticket était
`070_widen_loyalty_program_id_columns` (renumérotée depuis `068` par le rapport
[56](56-haccp-traceability-integration.md) suite à une collision) ; `071` n'existait ni dans
`migrations/` ni dans `migrations/done/`.

**`071_users_enabled_boolean.up.sql`**
```sql
-- Aligne users.enabled sur le type MySQL idiomatique des ~30 autres colonnes
-- booleennes de la table (isReception, isWaiter, admin, terms_of_use_accepted,
-- toutes tinyint(1)) et sur le plan initial docs/migration-postgres/04-schema-mapping-notes.md
-- (user_status_view attendait deja `u.enabled = false`). La colonne stockait
-- deja exclusivement 0/1 (audit docs/migration-postgres/59-users-enabled-boolean-conversion.md,
-- export reel 46/46 lignes + invariant deja impose par le scan Go vers bool sur
-- le chemin de connexion). MODIFY int(11) -> tinyint(1) ne change aucune donnee.

ALTER TABLE users
  MODIFY enabled tinyint(1) NOT NULL DEFAULT 1;
```

**`071_users_enabled_boolean.down.sql`**
```sql
-- Reverts 071_users_enabled_boolean.up.sql.

ALTER TABLE users
  MODIFY enabled int(11) NOT NULL DEFAULT 1;
```

À exécuter hors pic de trafic comme les précédents `ALTER TABLE` de ce dossier — `MODIFY` d'un
entier vers `tinyint(1)` sur une table de taille modeste (46 lignes réelles) ne devrait poser
qu'un verrou de métadonnées bref. **Reste la seule étape non exécutée de ce ticket** — à valider
et lancer séparément sur MySQL réel.

### 5.3 Générateur `data-migration/transform_mysql_csv.py`

**Confirmé : aucune adaptation nécessaire**, et vérifié en conditions réelles (§ 6.3) : une fois
le diff § 5.1 appliqué, `users.enabled` a été automatiquement traité comme les ~100 autres
colonnes boolean par `generate-all-sql`, sans toucher au script. Le générateur ne connaît aucun
nom de colonne en dur pour les booléens : `load_schema()` (lignes 248-300) dérive
`boolean_columns` en parsant le type déclaré dans `04-schema-postgres-target.sql`
(`if column_type == "boolean"`, ligne 276-277), et `format_sql_value()` (lignes 393-436) applique
alors `_bool_to_sql_literal()`.

### 5.4 Go — retrait du contournement Tier 3 (appliqué)

**`internal/modules/users/admin_repository.go:33-42`** — le filtre reconverti en bool direct
(`filters.Active` est déjà `*bool`, `admin_models.go:44`) :

```diff
 	if filters.Active != nil {
-		// users.enabled is an integer column (not boolean): bind 0/1 — pgx
-		// refuses a Go bool on int4, MySQL accepts both.
-		active := 0
-		if *filters.Active {
-			active = 1
-		}
 		baseQuery += ` AND u.enabled = ?`
-		args = append(args, active)
+		args = append(args, *filters.Active)
 	}
```

**`internal/modules/planning/employees/repository.go:132`** — littéral entier converti en
littéral portable, cohérent avec le reste du fichier (`p.enabled = TRUE`, `ur.enabled = TRUE`) :

```diff
-		WHERE ur.merchant_id = ? AND ur.user_id = ? AND ur.enabled = TRUE AND u.enabled = 1
+		WHERE ur.merchant_id = ? AND ur.user_id = ? AND ur.enabled = TRUE AND u.enabled = TRUE
```

**Sites de lecture** (`auth/repository.go:165,340`, `users/admin_repository.go:519,561`,
scans `u.enabled` → `bool`) : **aucun changement requis**, confirmé par les tests d'intégration
réels (§ 6.3). `driver.Bool.ConvertValue` accepte déjà un entier MySQL `0`/`1` aujourd'hui ; un
`tinyint(1)` MySQL ou le `boolean` Postgres renvoient la même valeur logique par le même chemin de
scan.

### 5.5 Fixtures de test — écart trouvé uniquement par le test d'intégration réel

Le diff prévu ne suffisait pas : `go build`/tests mockés ne pouvaient pas le détecter, seule
l'exécution réelle contre Postgres rechargé (§ 6.3) l'a révélé. Deux fixtures posaient un littéral
SQL entier `1` (pas un paramètre lié) dans l'`INSERT INTO users` :

```diff
--- a/internal/modules/auth/postgres_integration_test.go
+++ b/internal/modules/auth/postgres_integration_test.go
@@
 		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
-		VALUES ($1, 'ITest User', 'ITest', 'User', $2, 'itest-auth@example.com', '+33600000000', 'user-tok', 1)`, userID, passwordHash); err != nil {
+		VALUES ($1, 'ITest User', 'ITest', 'User', $2, 'itest-auth@example.com', '+33600000000', 'user-tok', true)`, userID, passwordHash); err != nil {
```
```diff
--- a/internal/modules/stocks/postgres_integration_test.go
+++ b/internal/modules/stocks/postgres_integration_test.go
@@
 		INSERT INTO users (user_id, name, first_name, last_name, password, email, token, enabled, merchant_id)
-		VALUES ($1, 'ITest Stock User', 'ITest', 'User', 'hash', 'itest-stk@example.com', 'stk-tok', 1, $2)`, userID, merchantID); err != nil {
+		VALUES ($1, 'ITest Stock User', 'ITest', 'User', 'hash', 'itest-stk@example.com', 'stk-tok', true, $2)`, userID, merchantID); err != nil {
```

Sur MySQL, `1` était silencieusement accepté par la colonne `int(11)`. Sur Postgres, avec la
colonne désormais `boolean`, un littéral entier échoue fort — Postgres ne caste pas
`integer → boolean` implicitement : `ERROR: column "enabled" is of type boolean but expression is
of type integer (SQLSTATE 42804)`. Confirmé qu'aucune autre fixture `postgres_integration_test.go`
du repo ne pose de littéral similaire sur `users.enabled` (grep exhaustif, § 6.3).

Le fixture mocké `internal/modules/users/admin_service_test.go` a aussi été mis à jour (attendait
`1` comme argument lié pour `filters.Active`, doit désormais attendre `true` suite à § 5.4) :
```diff
-		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", 1).
+		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", true).
...
-		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", 1, 20, 0).
+		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", true, 20, 0).
```

## 6. Validation

### 6.1 Build + tests unitaires

`go build ./...` : OK, aucune erreur.

`go test -count=1 ./internal/modules/auth/... ./internal/modules/users/... ./internal/modules/planning/employees/...` :
- `auth` : **PASS** (toutes les suites).
- `users` : **PASS** après correction de la fixture § 5.5 (échouait avant : `status = 500, want
  200`, argument mocké `1` au lieu de `true`).
- `planning/employees` : 2 échecs (`TestServiceDeleteEmployeeNullifiesAssignedShifts`,
  `TestServiceDeleteEmployeeReturnsNotFoundWhenEmployeeMissing`) — confirmés **pré-existants et
  sans rapport** avec ce ticket : reproduits à l'identique sur le code non modifié (`git stash`),
  concernent `DeleteEmployee`/`ExpectedBegin`, pas `IsMerchantUserLinked` ni `users.enabled`.

### 6.2 Revalidation pglast

`docs/migration-postgres/04-schema-postgres-target.sql` reparsé avec `pglast` (libpg_query) :
**467 statements OK**, 0 erreur.

### 6.3 Reset complet + rechargement réel du Postgres Docker de dev

1. `docker compose -f docker-compose.postgres.yml down -v` puis `up -d` : volume entièrement
   recréé (`welloresto-postgres-dev`, `postgres:16`, `localhost:5433`, DB `welloresto_dev`).
2. Schéma chargé : `docs/migration-postgres/04-schema-postgres-target.sql` → **184 tables**, 0
   erreur. `users.enabled` confirmé `boolean DEFAULT true` en base.
3. Régénération des 147 fichiers de données depuis le dump réel via
   `transform_mysql_csv.py generate-all-sql` : **147/147 tables générées, 0 échec**, `users: 46`
   lignes (cohérent avec § 1). Les 3 tables ajoutées au schéma par les rapports 55-57
   (`haccp_traceability_records`, `haccp_traceability_photos`, `discount_redemptions`) sont
   absentes du dump local (postérieures à sa date d'export) — comportement attendu, sans rapport
   avec ce ticket.
4. Chargement des 147 fichiers dans l'ordre numérique (`psql -v ON_ERROR_STOP=1` par fichier) :
   **147/147 chargés, 0 échec**.
5. Vérification directe en base :
   ```
   SELECT enabled, count(*) FROM users GROUP BY enabled;
     f | 3
     t | 43
   ```
   Distribution identique à l'audit du dump (§ 1). `user_status_view` (comparaison `= false`
   désormais) fonctionne : `DISABLED` = 3, cohérent.
6. Suite `postgres_integration` complète (`go test -tags postgres_integration -count=1 ./...`)
   contre cette base rechargée :
   - `auth` : **PASS**.
   - `users` : **PASS**.
   - C'est cette exécution qui a révélé l'écart de fixtures § 5.5 (`ERROR: column "enabled" is of
     type boolean but expression is of type integer`) — corrigé, puis suite relancée verte pour
     ces deux modules.
   - Échecs observés ailleurs dans la suite complète, **tous confirmés sans rapport avec
     `users.enabled`** (aucun ne touche à cette colonne ni aux fichiers modifiés par ce ticket) :
     `bookingcomm` (URL construite), `kiosk` (`kiosks.device_id` inexistant en schéma — écart
     structurel préexistant, sans rapport avec `enabled`), `planning/employees` (`DeleteEmployee`,
     § 6.1), `planning/leave` et `planning/swaps` (pagination par défaut 20 vs 200 attendu dans
     des fixtures mockées), `pos/accounting` (données TVA). Cohérent avec la liste d'échecs
     pré-existants déjà documentée au rapport 27 (`bookingcomm`, `planning/{employees,leave,
     swaps}`, `pos/accounting` — hors Tier 3, non touchés).

## Ce qu'il reste à faire

1. **Exécuter la migration MySQL réelle** (§ 5.2) sur la base MySQL de production/staging, hors
   pic de trafic — seule étape non réalisée par ce ticket. Aucune connexion à MySQL réel n'a été
   faite pendant ce travail.
2. Revue et commit des fichiers modifiés (schéma, `admin_repository.go`,
   `planning/employees/repository.go`, les 3 fixtures de test, les 2 fichiers de migration) —
   rien n'a été commité.

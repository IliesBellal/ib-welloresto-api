# 52 — Conversion des tâches cron `internal/tasks/` (dbx + vérification réelle Postgres)

Périmètre distinct de [07-module-inventory.md](07-module-inventory.md) : cet audit ne portait
que sur `internal/modules/*` et `internal/webhook/*` — `internal/tasks/` (tâches cron pilotées
par `TasksManager`, câblées dans `cmd/api/tasks.go`, actuellement désactivées via un `return`
précoce dans `SetupTasks`) n'avait **jamais été scoré ni converti** par les Tiers 1–4. Ce
chantier comble ce trou : audit + conversion complète vers `internal/database/dbx`
([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)), même méthodologie que
les rapports [14](14-tier1-conversion-log.md)/[25](25-tier2-conversion-log.md)/[27](27-tier3-conversion-log.md)/[29](29-tier4-conversion-log.md) :
vérification réelle contre le Postgres Docker de dev (`localhost:5433`, base `welloresto_dev`).
**Aucune donnée réelle n'est citée dans ce rapport.**

## 1. Inventaire — fichiers de `internal/tasks/` et sites SQL directs

| Fichier | Fonctions exportées | Sites SQL directs (hors modules déjà convertis) |
|---|---|---|
| `manager.go` | `ExpirePendingBookings`, `ExpireWaitlistNotifications`, `SendBookingReminders` | **0** — délègue entièrement à `BookingService` (module `bookings`, converti Tier 4) |
| `orders.go` | `CloseOrders`, `DenyOrders` | 2 `QueryContext` (`tm.DB` brut, aucun passage par `dbx`) |
| `distribution.go` | `UpdateAverageDistributionTime` | 3 (`QueryContext` ×2, `ExecContext` ×1, `tm.DB` brut) |
| `payments.go` | `CapturePayments`, `CancelPayments` | 1 `QueryContext` partagé (`processStripePayments`, `tm.DB` brut) |
| `products.go` | `UpdatePopularProducts` | `BeginTx`/`tx.ExecContext` ×2 (transaction brute, hors `dbx.Wrap`) + `collectIDs` (`QueryContext`, `tm.DB` brut) |
| `upsell.go` | `RecomputeUpsellPatterns`, `CleanupOldUpsellSuggestions` | `QueryRowContext` ×1 + `QueryContext` ×2 (`tm.DB` brut) ; `CleanupOldUpsellSuggestions` délègue à `UpsellRepo` (module `upsell`, converti Tier 1) — hors périmètre |
| `notifications.go` | `SendLoyaltyProgrammReminder` | **0** — non implémenté (log only) |

**Constat transverse : aucun fichier de `internal/tasks/` ne passait par `dbutils.GetDB`/`dbx.GetDB`.**
Toutes les requêtes utilisaient directement `tm.DB` (`*sql.DB`), qui **est** la connexion Postgres
réelle dès que `DB_DIALECT=postgres` (`cmd/api/main.go:27`, injecté tel quel dans
`NewTasksManager` par `cmd/api/routes.go:455` sous le nom historique `mysqlDB`). Résultat : sous
Postgres, **aucun `?` n'était jamais réécrit en `$N`** — c'est la cause racine commune à toutes
les tâches, indépendamment des fonctions de date MySQL-spécifiques qui s'y ajoutent.

## 2. `DenyOrders` (orders.go:98) — cause exacte du `syntax error at or near ";"`

### Requête en cause (avant correction)

```sql
SELECT o.order_id, o.merchant_id
FROM orders o
WHERE o.state <> 'DONE'
AND o.brand = 'WELLO_RESTO'
AND o.brand_status = 'PENDING_APPROVAL'
AND o.merchant_approval = 'PENDING_APPROVAL'
AND o.scheduled = false
AND TIMESTAMPDIFF(MINUTE, o.creation_date, UTC_TIMESTAMP) >= ?;
```
exécutée via `tm.DB.QueryContext(ctx, query, autoDenyDelay)`.

### Méthode

Un outil jetable (`tools/diag_tasks`, pgx/stdlib direct, jamais commité, supprimé en fin de
session — même précédent que le chargeur du rapport 51) a rejoué cette requête **telle quelle**
contre le Postgres Docker de dev, puis des variantes isolant chaque suspect (le `?` littéral seul,
`UTC_TIMESTAMP` bare seul, `TIMESTAMPDIFF` seul, le `;` final seul avec un `$1` correct).

### Résultat — la cause n'est ni un `;` en trop, ni en premier lieu les fonctions MySQL

| Variante testée | Résultat Postgres réel |
|---|---|
| Requête telle quelle (`?` littéral + `TIMESTAMPDIFF`/`UTC_TIMESTAMP` bare) | `ERROR: syntax error at or near ";"` (SQLSTATE 42601) — **reproduction exacte du bug rapporté** |
| Rebind `?`→`$1` fait, fonctions MySQL non traduites | `ERROR: column "minute" does not exist` (42703) — erreur *différente* |
| `?` littéral seul (sans aucune fonction MySQL) | `ERROR: syntax error at or near ";"` — **identique à la requête complète** |
| `$1` correct + `;` final (sans fonction MySQL) | OK — le `;` final n'est **pas** en cause |
| `UTC_TIMESTAMP` bare seul (sans parenthèses) | `ERROR: column "utc_timestamp" does not exist` (42703) — bug distinct, secondaire |
| `TIMESTAMPDIFF(...)` seul | `ERROR: column "minute" does not exist` (42703) — bug distinct, secondaire |

**Conclusion :** le `;` observé dans le message d'erreur n'est qu'un effet de bord — la cause
réelle est le **`?` littéral non réécrit**. `internal/tasks/orders.go` n'appelait jamais
`dbx.GetDB`/`dbx.Rebind` : sous le driver `pgx/stdlib` (protocole étendu, placeholders natifs
`$1, $2, ...`), un `?` littéral dans le texte de la requête n'est reconnu ni comme un
placeholder ni comme un opérateur valide à cette position — le scanner Postgres échoue et
rapporte l'erreur au niveau du token suivant qu'il peut identifier (ici le `;` de fin de
requête), pas au niveau du `?` lui-même. C'est un résultat contre-intuitif mais reproductible à
volonté (confirmé par la variante isolée « `?` seul, sans fonctions MySQL » → même erreur).
Une fois le rebind appliqué, une **second cause, indépendante**, apparaît : `TIMESTAMPDIFF`/
`UTC_TIMESTAMP` (bare) sont MySQL-only et doivent être traduits (`tskMinutesSince`, §4).

### Correction

`DenyOrders` route désormais par `dbx.GetDB(ctx, tm.DB)` (rebind `?`→`$N` actif sous Postgres) et
`TIMESTAMPDIFF(MINUTE, o.creation_date, UTC_TIMESTAMP) >= ?` est remplacé par le fragment portable
`tskMinutesSince("o.creation_date") >= ?` (§4). Vérifié réel : voir §5.

## 3. Constat additionnel non anticipé par la consigne — jointures `merchant.id` non castées

En creusant `CloseOrders`/`UpdateAverageDistributionTime`/`UpdatePopularProducts`/
`RecomputeUpsellPatterns`, un **second problème de portabilité indépendant** est apparu :
`merchant.id` est `integer` (identity) alors que les colonnes `merchant_id` qui le référencent
(`orders`, `merchant_parameters`, `subscriptions`, ...) sont `varchar(64)`. MySQL coerce
silencieusement `integer = varchar` dans une jointure ; Postgres refuse
(`operator does not exist: integer = character varying`, SQLSTATE 42725 — confirmé en lecture
seule contre le Docker de dev avant toute correction). C'est exactement le même pattern déjà
rencontré et corrigé dans `auth`/`users`/`ubereats`/`scannorder`/`reservation` (Tiers 2–3, cf.
`authMerchantJoinCast`/`usersMerchantJoinCast`/`ueMerchantJoinCast`/`snoMerchantJoinCast`) —
`internal/tasks/` en avait **4 occurrences** non détectées par la consigne initiale (qui ne
mentionnait que `TIMESTAMPDIFF`/`UTC_TIMESTAMP`) :

| Fichier | Jointure |
|---|---|
| `orders.go` (`CloseOrders`) | `merchant m` ↔ `orders.merchant_id`, `merchant_parameters.merchant_id`, `subscriptions.merchant_id` (3 jointures dans la même requête) |
| `distribution.go` (liste marchands) | `merchant m` ↔ `merchant_parameters.merchant_id` |
| `products.go` (liste marchands) | `merchant m` ↔ `subscriptions.merchant_id` |
| `upsell.go` (liste marchands, texte dupliqué de products.go) | `merchant m` ↔ `subscriptions.merchant_id` |

Nouveau helper local `tskMerchantJoinCast()` (même convention que les modules cités :
`CAST(m.id AS CHAR)` MySQL / `CAST(m.id AS TEXT)` Postgres), appliqué aux 4 sites. Vérifié réel
(psql direct) avant et après correction — §5.

## 4. Fragments SQL portables ajoutés (`internal/tasks/sqlcompat.go`)

Nouveau fichier, même esprit que `bkgAbsSecondsFromNow`/`workedExpr`
(`internal/modules/bookings/repository.go`, `internal/modules/planning/performance/repository.go`) :

| Helper | MySQL | Postgres |
|---|---|---|
| `tskMinutesSince(col)` | `TIMESTAMPDIFF(MINUTE, col, UTC_TIMESTAMP())` | `FLOOR(EXTRACT(EPOCH FROM (now() - col)) / 60)` |
| `tskSecondsBetween(from, to)` | `TIMESTAMPDIFF(SECOND, from, to)` | `EXTRACT(EPOCH FROM (to - from))::bigint` |
| `tskUnixTimestamp(col)` | `UNIX_TIMESTAMP(col)` | `EXTRACT(EPOCH FROM col)::bigint` |
| `tskNowMinusMinutes()` (porte un `?` paramétré) | `DATE_SUB(UTC_TIMESTAMP(), INTERVAL ? MINUTE)` | `(now() - (? * interval '1 minute'))` |
| `tskNowMinusDays()` (porte un `?` paramétré) | `DATE_SUB(NOW(), INTERVAL ? DAY)` | `(NOW() - (? * interval '1 day'))` |
| `tskNowMinus30Days()` (borne fixe) | `NOW() - INTERVAL 30 DAY` | `NOW() - INTERVAL '30 days'` |
| `tskMerchantJoinCast()` | `CAST(m.id AS CHAR)` | `CAST(m.id AS TEXT)` |

`tskNowMinusDays`/`tskNowMinus30Days` conservent `NOW()` (déjà valide dans les deux dialectes,
cf. 14-tier1-conversion-log.md §2) plutôt que `UTC_TIMESTAMP()`/`dbx.UTCNow()`, pour ne pas
changer le comportement horloge des tâches qui utilisaient déjà `NOW()` (`products.go`,
`upsell.go`) — seule la syntaxe `INTERVAL` était non portable. À l'inverse, `orders.go`/
`payments.go`/`distribution.go` utilisaient déjà `UTC_TIMESTAMP()` explicitement : `tskMinutesSince`/
`tskNowMinusMinutes` reproduisent ce choix via `dbx.UTCNow()`, sans changement de comportement.

## 5. Tableau de synthèse — par tâche

| # | Tâche (fichier) | Statut | Fichiers modifiés | Test réel Postgres | Écarts trouvés |
|---|---|---|---|---|---|
| 1 | `DenyOrders` (orders.go) | ✅ | `orders.go` | Cause du bug rapporté isolée et reproduite (§2), corrigée par `dbx.GetDB` + `tskMinutesSince` | `?` non rebindé = cause racine (pas le `;`) ; `TIMESTAMPDIFF`/`UTC_TIMESTAMP` bare = second bug indépendant |
| 2 | `CloseOrders` (orders.go) | ✅ | `orders.go` | `TestSQLCompatFragments_Postgres` (fragments réutilisés) + jointures validées en psql direct (§3) | Même cause racine que DenyOrders ×2 conditions + **3 jointures `merchant.id` non castées** (non anticipées par la consigne) |
| 3 | `UpdateAverageDistributionTime` (distribution.go) | ✅ | `distribution.go` | `TestComputeAndStoreAverageDistributionTime_Postgres` : SELECT réel (`UNIX_TIMESTAMP`/`TIMESTAMPDIFF`/`DATE_SUB` traduits) + upsert réel (insert puis update via `ON CONFLICT`, 1 seule ligne confirmée) | `ON DUPLICATE KEY UPDATE` → `ON CONFLICT (merchant_id) DO UPDATE` (PK confirmée sur `average_distribution_time.merchant_id`, pas de contrainte à ajouter) ; jointure `merchant.id` non castée |
| 4 | `CapturePayments`/`CancelPayments` (payments.go) | ✅ | `payments.go` | `TestSQLCompatFragments_Postgres` (fragment `tskMinutesSince` partagé, identique à la requête réelle) — voir note de périmètre ci-dessous | Même cause racine (`?` + `TIMESTAMPDIFF`/`UTC_TIMESTAMP`) ; pas de jointure `merchant` dans ce fichier |
| 5 | `UpdatePopularProducts` (products.go) | ✅ | `products.go` | `TestUpdateMerchantPopularProducts_Postgres` : reset + IN dynamique + transaction réels (produit avec commandes récentes → `is_popular=TRUE`, produit sans commande → remis à `FALSE`) | `is_popular = 0/1` → `TRUE`/`FALSE` (colonne boolean) ; `NOW() - INTERVAL 30 DAY` → forme portable ; transaction brute `tx.ExecContext` non rebindée → `dbx.Wrap(tx)` ; jointure `merchant.id` non castée dans la liste des marchands |
| 6 | `RecomputeUpsellPatterns` (upsell.go) | ✅ | `upsell.go` | `TestProcessUpsellPatternsForMerchant_Postgres` : agrégats de co-occurrence réels (support/confiance/lift) sur commandes CLOSED seedées | `DATE_SUB(NOW(), INTERVAL ? DAY)` (×4 occurrences) → `tskNowMinusDays()` ; jointure `merchant.id` non castée dans la liste des marchands |
| 7 | `CleanupOldUpsellSuggestions` (upsell.go) | — | — | Hors périmètre : délègue à `UpsellRepo`, module déjà converti Tier 1 | — |
| 8 | `manager.go` (Expire*/SendBookingReminders) | — | — | Hors périmètre : délègue à `BookingService`, module déjà converti Tier 4 | — |
| 9 | `notifications.go` | — | — | Non implémenté (log only), aucune surface SQL | — |

**Résultat global : 6/6 tâches avec surface SQL propre converties et vertes.**

### Note de périmètre — pourquoi les points d'entrée cron exportés ne sont pas appelés directement

`CloseOrders`, `DenyOrders`, `CapturePayments`, `CancelPayments` bouclent sur **l'intégralité**
des tables `orders`/`payments`/`stripe_payments` sans filtre par marchand, puis appellent
`tm.OrderService`/`tm.StripeService` (services métier hors périmètre, déjà couverts par leurs
propres tests Tier 4) pour chaque ligne trouvée. Le Postgres Docker de dev utilisé ici contient
une **copie chargée de données réelles** (rapports 36–43, 48–51), pas seulement des fixtures
synthétiques — une vérification en lecture seule avant tout test a montré des lignes réelles déjà
présentes en base répondant à des critères proches (ex. paiements `stripe_payments` au statut
`REQUIRES_CONFIRMATION`). Appeler ces points d'entrée tels quels aurait donc risqué soit un panic
(service métier non instancié dans un test scopé à cette seule conversion), soit, plus grave, une
tentative d'action réelle (refus de commande, capture/remboursement Stripe) sur des données de
production copiées. Décision (même prudence que le rapport 51, préfixes `itest-*` jamais de
balayage complet) : la portabilité de ces 4 requêtes est vérifiée par
`TestSQLCompatFragments_Postgres`, qui exerce **les mêmes fragments SQL** (`tskMinutesSince`,
`tskMerchantJoinCast`, etc.) sur des tables dérivées isolées et des valeurs littérales — jamais
sur les tables réelles `orders`/`payments`/`merchant`. Les 3 tâches restantes
(`UpdateAverageDistributionTime`, `UpdatePopularProducts`, `RecomputeUpsellPatterns`) exposent
chacune une fonction **par marchand** déjà séparée dans le code (`computeAverageDistributionTime`,
`updateMerchantPopularProducts`, `processUpsellPatternsForMerchant`) : celles-ci sont testées
réellement de bout en bout, scopées au seul marchand sentinelle créé et nettoyé par le test —
jamais de boucle sur les marchands réels de la base de dev.

## 6. Vérification réelle Postgres

**Commande de vérification** :
```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/tasks/... -v
```

Nouveau fichier `internal/tasks/postgres_integration_test.go` (tag `postgres_integration`,
helper `pgtest`) :

| Test | Portée | Résultat |
|---|---|---|
| `TestSQLCompatFragments_Postgres` (7 sous-tests) | Les 7 helpers de `sqlcompat.go`, isolés (tables dérivées/valeurs littérales, jamais les tables réelles) | ✅ PASS |
| `TestComputeAndStoreAverageDistributionTime_Postgres` | `computeAverageDistributionTime` (SELECT réel) + upsert `ON CONFLICT` réel, scopés marchand sentinelle | ✅ PASS (insert puis update confirmés, 1 seule ligne) |
| `TestUpdateMerchantPopularProducts_Postgres` | `updateMerchantPopularProducts` (transaction + IN dynamique + booléens) | ✅ PASS |
| `TestProcessUpsellPatternsForMerchant_Postgres` | `processUpsellPatternsForMerchant` (agrégats co-occurrence) | ✅ PASS |

**Nettoyage post-test confirmé** : chaque test seed son propre marchand sentinelle
(`token` préfixé `it...`, id auto-incrémenté réel) et le nettoie en `t.Cleanup`. Recomptage après
exécution : 0 résidu (`merchant`/`average_distribution_time` filtrés sur les identifiants
sentinelles utilisés par ces tests).

**`go build ./...`** : OK.
**`go test ./internal/...`** (dialecte MySQL par défaut) : liste d'échecs strictement identique à
la baseline pré-existante des rapports 14/25/29 (`bookingcomm` 1, `planning/employees` 2,
`planning/leave` 7, `planning/swaps` 3 — tous hors périmètre, intacts). Le paquet
`internal/tasks` lui-même est **entièrement vert**, y compris les 6 tests unitaires
pré-existants de `distribution_test.go` (logique pure `simulateAverageDistributionTime`,
non touchée). **Aucune régression.**

`go vet ./...` : mêmes 2 échecs pré-existants et non liés (`ubereats/client` code inaccessible,
`auth` copie de lock) déjà documentés dans les rapports 27/29 — rien dans `internal/tasks/`.

## 7. Écarts transverses (à retenir si d'autres tâches cron sont ajoutées)

1. **`internal/tasks/` n'était converti par aucun Tier précédent** : le périmètre initial
   (rapport 07) ne couvrait que `internal/modules/*`/`internal/webhook/*` — toute nouvelle
   tâche cron doit désormais aussi passer par `dbx.GetDB` dès l'écriture, pas seulement les
   modules HTTP.
2. **Un `?` littéral non rebindé produit une erreur trompeuse** : sous pgx (protocole étendu),
   Postgres rapporte `syntax error at or near ";"` (le token suivant qu'il parvient à identifier),
   pas une erreur pointant le `?` lui-même. Un tel message dans les logs prod est un signal fort
   de code qui contourne `dbx.GetDB`, à chercher en premier (`grep tm.DB.Query\|tm.DB.Exec` /
   plus généralement toute connexion utilisée hors `dbx.GetDB`/`dbx.Wrap`).
3. **Jointure `merchant.id` (integer) ↔ `*.merchant_id` (varchar)** : pattern déjà connu
   (Tiers 2–3) mais qui peut réapparaître dans n'importe quel nouveau code listant les marchands
   par jointure directe sur `merchant m` — `tskMerchantJoinCast()` centralisé ici, à répliquer
   (ou factoriser un jour au niveau `dbx` si un 5ᵉ site apparaît).
4. **Transactions brutes (`db.BeginTx` + `tx.ExecContext`)** hors du chemin `dbx.GetDB` : même
   piège que `planning/swaps.Approve` (rapport 29) — envelopper avec `dbx.Wrap(tx)` ; grep
   `BeginTx` systématique en fin de conversion, y compris hors `internal/modules/`.
5. **Duplication de requête entre fichiers** : la requête de liste des marchands actifs
   (`SELECT m.id FROM merchant m INNER JOIN subscriptions s ...`) est dupliquée à l'identique
   dans `products.go` et `upsell.go` — corrigée aux deux endroits séparément (pas de
   factorisation introduite, hors périmètre de cette conversion).

## 8. Modifications d'infrastructure ajoutées pendant ce chantier

- `internal/tasks/sqlcompat.go` (nouveau) : `tskMinutesSince`, `tskSecondsBetween`,
  `tskUnixTimestamp`, `tskNowMinusMinutes`, `tskNowMinusDays`, `tskNowMinus30Days`,
  `tskMerchantJoinCast`.
- `internal/tasks/postgres_integration_test.go` (nouveau) : voir §6.
- Aucune modification de `internal/database/dbx/*` ni de
  `04-schema-postgres-target.sql` n'a été nécessaire pour ce chantier (contrairement aux Tiers
  précédents) — `average_distribution_time.merchant_id` est déjà PK, aucune contrainte à ajouter
  pour `ON CONFLICT`.

## 9. Découpage en commits atomiques (préparé, non exécuté)

Un commit par tâche/fichier, plus un commit dédié pour l'infrastructure partagée et un pour ce
rapport. `DB_DIALECT=mysql` reste le défaut en production ; aucun commit n'a été exécuté.

| Commit suggéré | Fichiers |
|---|---|
| `postgres: add internal/tasks SQL compat helpers (dbx)` | `internal/tasks/sqlcompat.go` |
| `postgres: convert orders.go cron tasks to dbx (fix DenyOrders syntax error root cause)` | `internal/tasks/orders.go` |
| `postgres: convert distribution.go cron task to dbx` | `internal/tasks/distribution.go` |
| `postgres: convert payments.go cron tasks to dbx` | `internal/tasks/payments.go` |
| `postgres: convert products.go cron task to dbx (wrap tx, boolean literals)` | `internal/tasks/products.go` |
| `postgres: convert upsell.go cron task to dbx` | `internal/tasks/upsell.go` |
| `postgres: add internal/tasks postgres_integration tests` | `internal/tasks/postgres_integration_test.go` |
| `docs: add internal/tasks postgres conversion log` | `docs/migration-postgres/52-tasks-cron-conversion.md` |

## 10. Hors périmètre — travail non lié détecté dans l'arbre de travail

L'arbre de travail contient, indépendamment de ce chantier et non touché par lui, une
modification déjà présente de `docs/migration-postgres/04-schema-postgres-target.sql`
(élargissement `audit_logs.id` `varchar(36)` → `varchar(64)`) accompagnée de fichiers non suivis
(`docs/migration-postgres/53-audit-logs-column-width.md`,
`migrations/067_widen_audit_logs_id.{up,down}.sql`) ainsi qu'un binaire de debug
(`cmd/api/__debug_bin.exe...`) — aucun de ces éléments n'a été créé ni modifié pendant ce
chantier ; à committer (ou nettoyer, pour le binaire) séparément par l'utilisateur s'il le
souhaite, même précédent que le rapport 25 §« Hors périmètre ».

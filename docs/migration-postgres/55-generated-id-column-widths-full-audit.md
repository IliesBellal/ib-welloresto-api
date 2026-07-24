# 55 — Audit complet des largeurs de colonne pour les valeurs `helpers.GeneratePrefixedID` (écart transverse #7 ter)

Erreur observée en test Postgres : `value too long for type character varying(30)` sur le
chemin `ProcessOrderLoyalty` ([service.go:203-213](../../internal/modules/customers/service.go#L203-L213))
→ `UpdateLoyaltyFromOrder` ([repository.go:1353](../../internal/modules/customers/repository.go#L1353)).
Aucune donnée réelle citée ci-dessous — uniquement le schéma et le code.

Contrairement aux rapports [28](28-varchar-widening.md) et [53](53-audit-logs-column-width.md), qui
corrigeaient chacun la ou les colonnes responsables de l'erreur observée, ce chantier fait le
balayage **complet** annoncé mais jamais réalisé au Tier 4 (`29-tier4-conversion-log.md`) : chaque
appelant de `helpers.GeneratePrefixedID` dans `internal/`, comparé colonne par colonne à
`04-schema-postgres-target.sql`, pour ne plus découvrir ce défaut un endpoint à la fois.

## 1. Colonne exacte à l'origine de l'erreur

`ProcessOrderLoyalty` appelle `UpdateLoyaltyFromOrder`, qui — pour un client sans progression
existante sur un programme de fidélité — exécute en premier :

```go
newProgressID := helpers.GeneratePrefixedID("cus-progress")
INSERT INTO customer_loyalty_progress (id, customer_id, loyalty_program_id, current_value, last_update)
VALUES (?, ?, ?, 0, ...)
```
([repository.go:1444-1445](../../internal/modules/customers/repository.go#L1444-L1445))

`id` (49 caractères, voir §2) tient dans `customer_loyalty_progress.id varchar(64)` — déjà élargie
au [rapport 28](28-varchar-widening.md). La colonne qui échoue est la **troisième** de l'INSERT :
`loyalty_program_id`, alimentée par `p.ID` = `customer_loyalty_programs.id`, une valeur
`GeneratePrefixedID("loyal-prog")` de 47 caractères (§2). Avant ce chantier, le schéma cible
portait :

```sql
CREATE TABLE customer_loyalty_progress (
    id varchar(64) NOT NULL,
    customer_id varchar(30) NOT NULL,
    loyalty_program_id varchar(30) NOT NULL,   -- <- trop étroite : 47 chars > 30
    ...
```

`customer_loyalty_programs.id` lui-même est `varchar(50)` et n'a jamais été signalé comme trop
étroit — à raison, 47 ≤ 50. Le défaut n'est pas sur la colonne source de l'ID, mais sur les
colonnes qui en **recopient la valeur** comme référence de type clé étrangère (même schéma que
`progress_id` au rapport 28, qui recopie `customer_loyalty_progress.id`).

Si un client a déjà une ligne de progression, ce premier INSERT est sauté et l'exécution tombe sur
l'INSERT suivant (`customer_loyalty_progress_order`, §3) qui porte exactement le même défaut — donc
le même incident aurait resurgi sur le prochain client sans progression pré-existante. C'est
précisément le genre de découverte « un endpoint à la fois » que ce chantier a pour but d'arrêter.

## 2. Générateur Go et longueur réelle

Toutes deux `helpers.GeneratePrefixedID(prefix)` = `prefix + "-" + uuid.New().String()`
([ids.go:67-69](../../internal/helpers/ids.go#L67-L69)), UUID toujours 36 caractères (`8-4-4-4-12`).

| Générateur | Préfixe | Longueur préfixe | Longueur totale |
|---|---|---|---|
| `CreateLoyaltyProgram` ([service.go:92](../../internal/modules/customers/service.go#L92)) | `"loyal-prog"` | 10 | **47** |
| `UpdateLoyaltyFromOrder` ([repository.go:1444](../../internal/modules/customers/repository.go#L1444)) | `"cus-progress"` | 12 | 49 |
| `UpdateLoyaltyProgress` ([repository.go:946](../../internal/modules/customers/repository.go#L946), flux manuel) | `"loyalty-progress"` | 16 | 53 |

La valeur qui déborde est celle de la première ligne (47 caractères) : c'est
`customer_loyalty_programs.id`, généré une seule fois à la création du programme, puis copié tel
quel dans `loyalty_program_id` par les trois flux d'écriture identifiés au §3 — que la progression
associée provienne du flux manuel (`loyalty-progress`) ou automatique (`cus-progress`) ne change
rien à la largeur de `loyalty_program_id`, seule `p.ID` (47 chars) y est écrite.

## 3. Balayage complet — tous les call sites de `GeneratePrefixedID`

77 sites de production ont été recensés (`grep -rn "GeneratePrefixedID" internal/`, hors fichiers
`*_test.go`), chacun comparé à la colonne cible réelle qui reçoit la valeur (colonne d'origine
**et** colonnes qui en recopient une copie, à la manière de `progress_id`/rapport 28). Seuls les
défauts sont listés en détail ; le reste (74 sites) a été vérifié suffisant et n'est pas reporté
ligne à ligne pour rester lisible — méthode et résultat agrégé en annexe (§7).

### Défauts trouvés — famille unique : copies de `customer_loyalty_programs.id`

| Table.colonne | Largeur avant | Généré par | Longueur réelle | Verdict |
|---|---|---|---|---|
| `customer_loyalty_progress.loyalty_program_id` | varchar(30) | `p.ID` / `req.LoyaltyProgramID` (copie de `customer_loyalty_programs.id`, 47 chars) | 47 | **DÉFAUT** |
| `customer_loyalty_progress_order.loyalty_program_id` | varchar(30) | idem | 47 | **DÉFAUT** |
| `customer_rewards.loyalty_program_id` | varchar(30) | idem | 47 | **DÉFAUT** |

Trois sites d'écriture recopient `customer_loyalty_programs.id` dans ces colonnes :

- [repository.go:1445](../../internal/modules/customers/repository.go#L1445) — INSERT
  `customer_loyalty_progress` (flux commande, `UpdateLoyaltyFromOrder`)
- [repository.go:946](../../internal/modules/customers/repository.go#L946) — INSERT
  `customer_loyalty_progress` (flux manuel, `UpdateLoyaltyProgress`)
- [repository.go:1490](../../internal/modules/customers/repository.go#L1490) — INSERT
  `customer_loyalty_progress_order`
- [repository.go:1504-1507](../../internal/modules/customers/repository.go#L1504-L1507) — INSERT
  `customer_rewards`

Les deux tables sœurs `customer_loyalty_program_target_products` et
`customer_loyalty_program_reward_products` recopient elles aussi `loyaltyProgramID` dans une
colonne `loyalty_program_id`, mais celle-ci est `varchar(50)` — 47 ≤ 50, **pas de défaut**
([repository.go:796](../../internal/modules/customers/repository.go#L796) et
[:819](../../internal/modules/customers/repository.go#L819)). C'est la différence de largeur entre
tables sœurs (30 vs 50) qui explique pourquoi seules trois colonnes sur cinq portant le même genre
de valeur sont en défaut.

### Site à part : `haccp_traceability_records` / `haccp_traceability_photos` — tables absentes du schéma cible

`helpers.HACCPTraceabilityRecordIDPrefix` (`"haccp-trace"`, 48 chars) et
`helpers.HACCPTraceabilityPhotoIDPrefix` (`"haccp-trace-photo"`, 55 chars)
([repository.go:1721-1746](../../internal/modules/haccp/repository.go#L1721-L1746)) alimentent
`haccp_traceability_records.id` et `haccp_traceability_photos.id`/`record_id`. Ces deux tables
**n'existent pas** dans `04-schema-postgres-target.sql` — elles ont été créées directement en
MySQL par la migration [`migrations/done/067_haccp_traceability.up.sql`](../../migrations/done/067_haccp_traceability.up.sql)
(déjà exécutée, voir §5), postérieure au dump phpMyAdmin du 2026-07-13 sur lequel
`04-schema-postgres-target.sql` a été généré (même situation que `planning_day_comments` avant le
rapport 26).

Côté largeur, pas de défaut : la migration MySQL déjà appliquée déclare `id`/`record_id` en
`varchar(64)`, qui contient largement 48 et 55 caractères. Mais le schéma cible Postgres ne
contenant aucune définition de ces deux tables, le module HACCP traçabilité ne pourrait tout
simplement pas fonctionner après bascule Postgres — pas d'erreur de largeur, une erreur "table
inexistante". **Hors périmètre de ce chantier** (qui porte sur la largeur des colonnes existantes,
pas sur les tables manquantes), mais un risque plus grave que tout ce qui est corrigé ici :
mériterait un rapport dédié pour ajouter ces deux `CREATE TABLE` à `04-schema-postgres-target.sql`
à partir de la migration 067 déjà en production. **Traité par le
[rapport 56](56-haccp-traceability-integration.md)**.

### Reste du balayage — 74 sites vérifiés suffisants

Tous les autres appelants de `GeneratePrefixedID` ciblent une colonne dont la largeur couvre la
longueur réellement produite (préfixe + 37 caractères), y compris des colonnes restées sous
varchar(64) par ailleurs (ex. `tags.tag_id varchar(42)` avec le préfixe `"tag"` → 40 chars,
`availabilities.availability_id varchar(50)` avec `"avail"` → 42 chars,
`customer_loyalty_programs.id varchar(50)` avec `"loyal-prog"` → 47 chars) : conformément à la
consigne, ces colonnes déjà suffisantes n'ont **pas** été élargies par confort — seuls les
véritables défauts sont touchés. Le détail table par table (77 sites, préfixe, longueur calculée,
colonne cible, verdict) a été vérifié un par un pendant ce chantier ; il n'est pas recopié
intégralement ici pour ne pas noyer les 3 vrais défauts, mais toute colonne `varchar(N)` avec
`N < 64` non citée en défaut ci-dessus a été confirmée suffisante pour la valeur qu'elle reçoit
réellement (liste des tables couvertes : `availabilities*`, `configurable_attributes`,
`marketing_categories`, `discounts`, `floor_obstacles`, `users`, `kiosks`/`kiosk_device_tokens`/
`kiosk_enrollment_codes`, `booking_events`, `booking_duration_rules`, `orders.public_id`,
`hours_of_operation`, `printers`, `receipts`, `stock_movements`, `employee_documents`, `tags`,
tables `haccp_*` avec colonnes `varchar(64)`, tables `planning_*` avec colonnes `varchar(64)`,
`upsell_suggestions`).

## 4. Correction du schéma cible Postgres

[`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) — les 3 colonnes en défaut portées
à `varchar(64)`, cohérent avec la convention des rapports 28/53 (marge sans changer de classe de
stockage — préfixe de longueur sur 1 octet, valable jusqu'à 255) :

```sql
CREATE TABLE customer_loyalty_progress (
    id varchar(64) NOT NULL,
    customer_id varchar(30) NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    ...
CREATE TABLE customer_loyalty_progress_order (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    progress_id varchar(64) NOT NULL,
    ...
CREATE TABLE customer_rewards (
    reward_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    customer_id varchar(30) NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    ...
```

`customer_id` (30) n'est pas touché : il reçoit `orders.customer_id`/`customer.customer_id`, un
identifiant entier casté en chaîne — pas une valeur `GeneratePrefixedID`, hors périmètre de ce
chantier.

### Revalidation `pglast`

Même méthode que les rapports [13](13-merchant-id-schema-update.md)/[18](18-order-id-schema-update.md)/
[26](26-planning-day-comments-integration.md)/[28](28-varchar-widening.md)/[53](53-audit-logs-column-width.md) :

```
python3 -c "
import pglast
with open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8') as f:
    sql = f.read()
stmts = pglast.parse_sql(sql)
print('PARSE OK -', len(stmts), 'statements')
"
→ PARSE OK - 457 statements
```

Même compte qu'après les rapports 26/28/53 (aucune instruction ajoutée/retirée, seules des largeurs
de colonnes modifiées).

## 5. Migration MySQL réelle nécessaire

Oui — `wello-resto-mysql-ddl.md` confirme que `loyalty_program_id varchar(30)` est déjà la largeur
en MySQL source pour les trois tables (lignes 845, 858, 873) : ce n'est pas un écart introduit par
la traduction du schéma, le défaut préexiste en production MySQL (même situation que
`audit_logs.id` au rapport 53).

**Collision de numérotation découverte pendant ce chantier** : le rapport 53 proposait
`067_widen_audit_logs_id` comme « prochain numéro libre après 066 », mais n'avait vérifié que le
dossier `migrations/` (migrations écrites, pas encore exécutées). Or `migrations/done/` (migrations
déjà exécutées en base réelle, convention introduite au commit `9b18801`) contient déjà
[`067_haccp_traceability.up.sql`](../../migrations/done/067_haccp_traceability.up.sql), ajouté dans
le même commit `0b4509f` que `066_widen_varchar_columns` et `067_widen_audit_logs_id`. Les deux
migrations « 067 » (`widen_audit_logs_id`, toujours en attente dans `migrations/` ; et
`haccp_traceability`, déjà exécutée dans `migrations/done/`) portent donc le même numéro sans
jamais avoir été en conflit d'exécution — mais le prochain numéro à utiliser n'est pas 067. Signalé
ici pour que `067_widen_audit_logs_id` soit renuméroté avant exécution ; non corrigé par ce
chantier (hors périmètre demandé) — **renuméroté 069** par le
[rapport 56](56-haccp-traceability-integration.md#partie-1--collision-de-numérotation), qui a
constaté que **068** (choisi ci-dessous) n'était plus libre au moment de corriger la collision,
d'où le décalage des deux migrations en 069/070 pour préserver l'ordre de production.

[`migrations/070_widen_loyalty_program_id_columns.up.sql`](../../migrations/070_widen_loyalty_program_id_columns.up.sql) /
[`.down.sql`](../../migrations/070_widen_loyalty_program_id_columns.down.sql) — **068** au moment de
ce chantier, prochain numéro réellement libre alors (aucun fichier `068*` dans `migrations/` ni
`migrations/done/`) ; renuméroté **070** par le [rapport 56](56-haccp-traceability-integration.md)
en même temps que 067→069 (voir note ci-dessus).

```sql
ALTER TABLE customer_loyalty_progress
  MODIFY loyalty_program_id varchar(64) NOT NULL;

ALTER TABLE customer_loyalty_progress_order
  MODIFY loyalty_program_id varchar(64) NOT NULL;

ALTER TABLE customer_rewards
  MODIFY loyalty_program_id varchar(64) NOT NULL;
```

**Impact index/clé** : élargir un `varchar(30)` vers `varchar(64)` reste dans la même classe de
stockage (préfixe de longueur sur 1 octet) — MySQL exécute ces trois `MODIFY` en place. Aucune des
trois colonnes n'est indexée (`ALTER TABLE ... customer_loyalty_progress/_order/customer_rewards`
dans `wello-resto-mysql-ddl.md:3758-3776` : aucun index sur `loyalty_program_id`), donc aucun index
à reconstruire.

> ⚠️ **Changement de schéma MySQL réel en production**, distinct de la bascule Postgres à venir. À
> appliquer et tester séparément (staging d'abord). Non exécutée par ce chantier.

### Requête de vérification à faire tourner en prod avant d'appliquer la migration

Aucun accès aux données de production n'était disponible ici (comme aux rapports 28/53). Aucune des
trois colonnes n'est `UNIQUE` ni `PRIMARY KEY` — une troncature silencieuse en mode MySQL non
strict n'aurait donc pas provoqué de rejet visible, contrairement à `audit_logs.id` (PK) : c'est le
scénario le plus proche de `users.token` au rapport 28 (risque de collision silencieuse, pas de
garde-fou structurel). Les valeurs stockées peuvent référencer un `customer_loyalty_programs.id`
tronqué à 30 caractères, ambigu entre plusieurs programmes si deux id tronqués coïncident sur les
30 premiers caractères (peu probable vu le préfixe fixe `"loyal-prog-"` suivi d'un UUID, mais à
vérifier) :

```sql
-- Distribution des longueurs stockées — une concentration à exactement 30
-- caractères indique des loyalty_program_id déjà tronqués par le mode non strict
SELECT LENGTH(loyalty_program_id) AS len, COUNT(*) AS n
FROM customer_loyalty_progress GROUP BY len ORDER BY len DESC;

SELECT LENGTH(loyalty_program_id) AS len, COUNT(*) AS n
FROM customer_loyalty_progress_order GROUP BY len ORDER BY len DESC;

SELECT LENGTH(loyalty_program_id) AS len, COUNT(*) AS n
FROM customer_rewards GROUP BY len ORDER BY len DESC;

-- Programmes dont l'id tronqué à 30 caractères est ambigu (partagé par
-- plusieurs id complets de customer_loyalty_programs)
SELECT LEFT(id, 30) AS truncated, COUNT(*) AS n
FROM customer_loyalty_programs
GROUP BY truncated
HAVING n > 1;
```

Si la première série de requêtes montre une concentration à 30 caractères, élargir la colonne ne
corrige pas rétroactivement les valeurs déjà tronquées en base ; en l'absence d'ambiguïté détectée
par la dernière requête, la valeur tronquée reste néanmoins interprétable comme référence à un
programme unique (les 30 premiers caractères d'un id `loyal-prog-<uuid>` identifient déjà le
programme sans collision tant qu'aucun autre programme ne partage ce préfixe) — mais toute requête
applicative qui compare le champ complet (`WHERE loyalty_program_id = ?` avec l'id complet) échouera
sur ces lignes historiques tant qu'elles n'ont pas été régénérées.

## 6. Code Go

Aucune modification nécessaire : aucune troncature Go n'a été trouvée pour
`loyalty_program_id`/`p.ID` dans `internal/modules/customers/` (`grep -rn "truncateVarchar|\[:30\]"`
sans résultat — la fonction `truncateVarchar` retirée au rapport 28 n'a pas été réintroduite).
Aucun fichier `.go` modifié par ce chantier ; `go build ./...` non exécuté (non requis par la
consigne, aucun code Go touché).

## 7. Méthode du balayage complet (§3)

```
grep -rn "GeneratePrefixedID" internal/ | grep -v _test.go
```

77 sites de production recensés. Pour chacun : préfixe extrait, longueur calculée
(`len(prefix) + 1 + 36`), colonne(s) cible(s) identifiée(s) en lisant l'INSERT correspondant (y
compris les colonnes qui recopient une valeur générée ailleurs, comme `loyalty_program_id` ou
`progress_id`), largeur cible relevée dans `04-schema-postgres-target.sql`, verdict
suffisant/insuffisant. Seuls 3 sites ont révélé un défaut, tous de la même famille (§3) — la
disproportion entre l'ampleur du balayage (77 sites) et le nombre de défauts trouvés (3, sur les
mêmes 3 colonnes d'une seule famille) confirme que les rapports 28/53 avaient déjà traité la
quasi-totalité des cas réels ; ce qui restait n'était pas un écart de largeur générique mais un
oubli précis (les colonnes de recopie de `customer_loyalty_programs.id`, jamais auditées comme
telles).

## 8. Vérification

- Validation `pglast` de `04-schema-postgres-target.sql` : OK, 457 statements, aucune erreur.
- `go build ./...` : non exécuté, aucun fichier `.go` modifié par ce chantier.
- Risque hors périmètre signalé au §3 : `haccp_traceability_records`/`haccp_traceability_photos`
  absentes de `04-schema-postgres-target.sql` malgré une migration MySQL déjà exécutée en
  production (`migrations/done/067_haccp_traceability.up.sql`) — mériterait un rapport dédié.
- Collision de numérotation signalée au §5 : `067_widen_audit_logs_id.up/down.sql` (rapport 53, en
  attente) porte le même numéro que `migrations/done/067_haccp_traceability` (déjà exécutée) — à
  renuméroter avant toute exécution de la migration du rapport 53.

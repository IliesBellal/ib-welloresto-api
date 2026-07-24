# 56 — Intégration `haccp_traceability_records`/`haccp_traceability_photos` + collision de numérotation 067/068 + audit élargi des tables manquantes

Contexte : le [rapport 55](55-generated-id-column-widths-full-audit.md) a identifié deux problèmes
hors de son périmètre pendant son balayage :

1. `haccp_traceability_records`/`haccp_traceability_photos` — créées en MySQL par
   [`migrations/done/067_haccp_traceability.up.sql`](../../migrations/done/067_haccp_traceability.up.sql)
   (déjà exécutée), **absentes** de [`04-schema-postgres-target.sql`](04-schema-postgres-target.sql)
   — même situation que `planning_day_comments` avant le [rapport 26](26-planning-day-comments-integration.md).
2. Une collision de numérotation : `migrations/067_widen_audit_logs_id` (rapport 53, en attente) et
   `migrations/done/067_haccp_traceability` (déjà exécutée) portent le même numéro **067**.

Ce chantier traite les deux, plus un troisième point demandé en vérification élargie (§ Partie 3).
Aucune donnée réelle citée ci-dessous — uniquement schéma, code et migrations. **Rien commité.**

---

## Partie 1 — Collision de numérotation

### 1.1 Recherche du prochain numéro réellement libre

Inventaire complet de `migrations/` (en attente) et `migrations/done/` (déjà exécutées) :

```
migrations/                          migrations/done/
062_location_varchar_ids.sql          ... 001 → 062_kiosks_device_id ...
063_floor_obstacles.up.sql            067_haccp_traceability.{up,down}.sql
064_locations_attributes.up.sql
065_planning_day_comments.{up,down}.sql
066_widen_varchar_columns.{up,down}.sql
067_widen_audit_logs_id.{up,down}.sql        <- collision avec done/067
068_widen_loyalty_program_id_columns.{up,down}.sql
```

Les deux dossiers partagent un seul et même espace de numérotation (confirmé au rapport 55 §5). Au
moment de ce chantier, les numéros **062 à 068 sont tous déjà pris** dans l'un des deux dossiers (062
est même utilisé deux fois — `migrations/062_location_varchar_ids.sql` vs
`migrations/done/062_kiosks_device_id.up.sql` — collision préexistante, non demandée par ce
chantier, signalée en aparté en fin de §1.3 mais **non corrigée** ici, hors périmètre explicite).
Le prochain numéro réellement libre, en ne comptant que la queue **067/068** concernée par ce
chantier, est donc **069**, puis **070**.

### 1.2 Renumérotation, dans l'ordre de production

Les deux migrations en collision/à recaler ont été produites dans cet ordre : `067_widen_audit_logs_id`
(rapport 53, commité dans `0b4509f` en même temps que `done/067_haccp_traceability` — c'est cette
coïncidence qui a créé la collision) puis `068_widen_loyalty_program_id_columns` (rapport 55, plus
récent, pas encore commité). Renuméroter uniquement `067_widen_audit_logs_id` en 069 aurait laissé
`068_widen_loyalty_program_id_columns` — produite après — numérotée *avant* elle (068 < 069) : les
deux ont donc été décalées de +2, pas seulement la première, pour préserver l'ordre de production :

| Ancien nom | Nouveau nom | Rapport d'origine |
|---|---|---|
| `migrations/067_widen_audit_logs_id.up.sql` / `.down.sql` | `migrations/069_widen_audit_logs_id.up.sql` / `.down.sql` | [53](53-audit-logs-column-width.md) |
| `migrations/068_widen_loyalty_program_id_columns.up.sql` / `.down.sql` | `migrations/070_widen_loyalty_program_id_columns.up.sql` / `.down.sql` | [55](55-generated-id-column-widths-full-audit.md) |

Renommage par simple déplacement de fichier (contenu SQL inchangé), plus une correction :
l'auto-référence dans le commentaire d'en-tête de `069_widen_audit_logs_id.down.sql`
(`-- Reverts 067_widen_audit_logs_id.up.sql.` → `-- Reverts 069_widen_audit_logs_id.up.sql.`).

### 1.3 Cross-références corrigées

Les rapports [53](53-audit-logs-column-width.md#6-migration-mysql-réelle-nécessaire) et
[55](55-generated-id-column-widths-full-audit.md#5-migration-mysql-réelle-nécessaire) contenaient des
liens markdown vers les anciens noms de fichiers (`067_widen_audit_logs_id.up.sql`,
`068_widen_loyalty_program_id_columns.up.sql`) : mis à jour vers `069_`/`070_` pour éviter des liens
morts, en conservant le récit historique intact (ce que chaque rapport croyait être vrai à l'époque
où il a été écrit — "prochain numéro libre après 066" pour le rapport 53, "068, prochain numéro
réellement libre" pour le rapport 55 — n'a pas été réécrit, seulement annoté d'un renvoi vers ce
rapport 56). Le passage du rapport 55 signalant `haccp_traceability_records`/`_photos` comme absentes
du schéma cible (§ site à part) a été annoté d'un renvoi similaire.

**Collision préexistante non traitée (062)** : `migrations/062_location_varchar_ids.sql` (en attente)
et `migrations/done/062_kiosks_device_id.up.sql` (déjà exécutée) partagent aussi le numéro 062. Elle
n'a pas été demandée par ce chantier (scope explicite : 067/068 uniquement) et n'a donc **pas** été
corrigée ici — signalée pour qu'elle ne soit pas découverte la nuit de la bascule non plus, mais elle
n'a jamais causé de conflit d'exécution réel (les deux migrations sont dans des dossiers différents à
des stades différents), contrairement à 067 où les deux fichiers vivaient tous les deux dans l'espace
« à exécuter avant la prochaine ».

---

## Partie 2 — Intégration des tables HACCP traçabilité

### 2.1 Traduction DDL — `04-schema-postgres-target.sql`

Source MySQL ([`migrations/done/067_haccp_traceability.up.sql`](../../migrations/done/067_haccp_traceability.up.sql), déjà exécutée) :

```sql
CREATE TABLE haccp_traceability_records (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    comment TEXT NULL,
    created_by VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    updated_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
    INDEX idx_haccp_traceability_records_merchant (merchant_id),
    INDEX idx_haccp_traceability_records_merchant_created (merchant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE haccp_traceability_photos (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    record_id VARCHAR(64) NOT NULL,
    photo_key VARCHAR(512) NOT NULL,
    position TINYINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT UTC_TIMESTAMP(),
    CONSTRAINT fk_haccp_traceability_photos_record FOREIGN KEY (record_id) REFERENCES haccp_traceability_records(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

Traduction ajoutée entre `haccp_settings` et `holiday_calendar` (ordre alphabétique du fichier :
`haccp_settings` < `haccp_traceability_*` < `holiday_calendar`) :

```sql
CREATE TABLE haccp_traceability_records (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    comment text,
    created_by varchar(64) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_haccp_traceability_records_idx_haccp_traceability_records_m ON haccp_traceability_records (merchant_id);
CREATE INDEX idx_haccp_traceability_records_idx_haccp_traceability_records_2 ON haccp_traceability_records (merchant_id, created_at);

CREATE TABLE haccp_traceability_photos (
    id varchar(64) NOT NULL,
    record_id varchar(64) NOT NULL,
    photo_key varchar(512) NOT NULL,
    position smallint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT fk_haccp_traceability_photos_record FOREIGN KEY (record_id) REFERENCES haccp_traceability_records (id) ON DELETE CASCADE
);
CREATE INDEX idx_haccp_traceability_photos_record_id ON haccp_traceability_photos (record_id);
```

Règles appliquées (identiques au reste du fichier, cf. [04-schema-mapping-notes.md](04-schema-mapping-notes.md)) :

- `TINYINT(1)` → `boolean` pour `enabled` (nom non ambigu, pas de revue manuelle nécessaire — absent
  de la liste des « TINYINT(1) ambigus » de `04-schema-mapping-notes.md`).
- `TINYINT` nu (non `UNSIGNED`) → `smallint` pour `position`, jamais converti en booléen (règle
  `TINYINT(3/4) -> SMALLINT`) ; pas de `CHECK (>= 0)` ajouté car la colonne n'est **pas** `UNSIGNED`
  côté MySQL (contrairement à `order_ratings.delivery_rating`/`product_ratings.rating`, qui eux le
  sont) — pas de perte de contrainte à compenser.
- `DATETIME` → `timestamptz`, `DEFAULT UTC_TIMESTAMP()` → `DEFAULT now()` (règle Tier 1 standard).
- `ON UPDATE UTC_TIMESTAMP()` retiré (pas d'équivalent déclaratif en PG) — note générique standard en
  en-tête de table, vérification du besoin réel de trigger en §2.2.
- `deleted_at DATETIME NULL` → `timestamptz` nullable, sans changement de sémantique.
- Noms d'index : règle `idx_/uq_<table>_<nom_mysql>` tronquée à 63 caractères avec dédoublonnage
  ([04-schema-mapping-notes.md:28](04-schema-mapping-notes.md)). Les deux noms MySQL
  (`idx_haccp_traceability_records_merchant`, `idx_haccp_traceability_records_merchant_created`)
  préfixés du nom de table donnent 70 et 78 caractères — tronqués à 63, et le second (dont les 63
  premiers caractères sont identiques au premier, `merchant`/`merchant_created` partageant le même
  préfixe) reçoit un suffixe `_2` pour éviter la collision, même mécanisme que
  `idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_2` déjà présent dans le fichier
  (`planning_shift_swap_requests`, ligne 2717).
- `haccp_traceability_photos.record_id` : **FK réelle conservée** (pas une candidate) — la seule
  autre FK réelle du fichier hors `merchant_translation_languages`/`product_ratings` avant ce
  chantier. MySQL/InnoDB indexe automatiquement les colonnes référençant une FK quand aucun index ne
  les couvre déjà ; PostgreSQL ne le fait pas — un index explicite (`idx_haccp_traceability_photos_record_id`)
  a donc été ajouté, même motif que `idx_product_ratings_idx_order_rating_id` déjà présent dans le
  fichier pour `product_ratings.order_rating_id`.
- **Déviation de l'ordre alphabétique strict** : `haccp_traceability_photos` (dont le nom trie
  *avant* `haccp_traceability_records`, "photos" < "records") est placée **après**
  `haccp_traceability_records` dans le fichier. Le fichier entier s'exécute comme une seule
  transaction (`BEGIN; ... COMMIT;`), et la contrainte `FOREIGN KEY (record_id) REFERENCES
  haccp_traceability_records (id)` exige que la table référencée existe déjà au moment de la création
  de la contrainte — l'ordre alphabétique pur aurait cassé l'exécution. C'est la même situation que
  `order_ratings`/`product_ratings` dans ce fichier, sauf que là l'ordre alphabétique coïncidait déjà
  avec l'ordre de dépendance (`o` < `p`) ; ici il fallait déroger explicitement.
- Note « FK candidate » standard pour `merchant_id` (liste reprise à l'identique des tables
  `haccp_settings`/`goods_receipts`/`temperature_readings` voisines).
- Collation `utf8mb4_unicode_ci` → note standard (collation PG par défaut, sensible à la casse).
- Pas de `COMMENT` MySQL sur ces colonnes → pas de `COMMENT ON COLUMN` généré.

#### Largeur des colonnes `helpers.GeneratePrefixedID`

Point explicitement demandé par la consigne, suite à la découverte du rapport 55 (écart transverse
sur les largeurs de colonnes `GeneratePrefixedID`) :

```go
// internal/helpers/ids.go
HACCPTraceabilityRecordIDPrefix = "haccp-trace"        // 11 caractères
HACCPTraceabilityPhotoIDPrefix  = "haccp-trace-photo"  // 17 caractères
```

`GeneratePrefixedID` = `prefix + "-" + uuid.New().String()` (36 caractères UUID) :

| Générateur | Longueur totale | Colonne cible | Largeur | Verdict |
|---|---|---|---|---|
| `HACCPTraceabilityRecordIDPrefix` | 11+1+36 = **48** | `haccp_traceability_records.id` | `varchar(64)` | suffisant, marge de 16 |
| `HACCPTraceabilityPhotoIDPrefix` | 17+1+36 = **54** | `haccp_traceability_photos.id` | `varchar(64)` | suffisant, marge de 10 |
| (copie de `records.id`) | 48 | `haccp_traceability_photos.record_id` | `varchar(64)` | suffisant, marge de 16 |

(Le rapport 55 avait avancé 55 caractères pour le préfixe photo au lieu de 54 — écart d'une unité
sans conséquence, aucun des deux ne dépasse `varchar(64)`.)

**Confirmation : la migration MySQL 067 avait déjà dimensionné ces colonnes en `varchar(64)` dès
l'origine** (pas `varchar(36)` comme `audit_logs.id` au rapport 53, ni `varchar(30)` comme
`customer_loyalty_progress.loyalty_program_id` au rapport 55) — aucun élargissement MySQL réel n'est
donc nécessaire ici, contrairement aux rapports 53/55. La traduction Postgre a repris `varchar(64)`
tel quel, déjà large d'emblée.

### 2.2 Audit `ON UPDATE CURRENT_TIMESTAMP` — `05-on-update-timestamp-audit.md`

Méthode identique au reste du document : localisation de tous les `UPDATE`/upserts touchant
`haccp_traceability_records` dans `internal/modules/haccp/repository.go`.

Seule `haccp_traceability_records.updated_at` porte un `ON UPDATE` MySQL — `haccp_traceability_photos`
n'a pas de colonne `updated_at` (seulement `created_at`, sans `ON UPDATE`), donc hors périmètre de cet
audit par construction.

Quatre fonctions touchent `haccp_traceability_records`, aucune n'est un `UPDATE` :

```go
// CreateTraceabilityRecord (repository.go:1721-1770) — seul point d'écriture
INSERT INTO haccp_traceability_records (id, merchant_id, comment, created_by, created_at, updated_at, enabled)
VALUES (?, ?, ?, ?, ?, ?, TRUE)
// created_at/updated_at posés côté Go (now := time.Now().UTC()), pas de fonction de date SQL

// ListTraceabilityRecords, GetTraceabilityRecord, HasTraceabilityRecords : SELECT / COUNT uniquement
```

**Classification : AUCUN UPDATE (INSERT seul)** — même famille que `goods_receipts.updated_at` et
`purchased_components.registration_date` déjà documentés. Ajoutée en ligne 11 du tableau récapitulatif
(entre `haccp_settings.updated_at` #10 et `holiday_calendar.updated_at`, décalée de #11 à #12) — les
lignes 11 à 41 existantes décalées de +1 (42 lignes au total désormais), ainsi que les sections de
détail numérotées après ce point (`### 14.` → `### 15.` (`kiosks`), `### 15.` → `### 16.`
(`kiosk_settings`), `### 17.` → `### 18.` (`marketing_categories`), `### 20.` → `### 21.` (`payments`),
`### 25.` → `### 26.` (`planning_revenue_forecasts`), `### 34.` → `### 35.` (`printers`), `### 37.` →
`### 38.` (`subscription_invoices`)). Bullet ajoutée dans la section « Cas particuliers — AUCUN
UPDATE ».

**Correction incidente** : la ligne de décompte en bas du tableau récapitulatif (« CONFIRMÉ = 17,
PARTIEL = 1, ABSENT = 8, AUCUN UPDATE = 15 », total 41) ne correspondait déjà **plus** au tableau réel
avant cet ajout (recompte indépendant : CONFIRMÉ=18, ABSENT=9, AUCUN UPDATE=13, total 41 — un écart
préexistant, sans lien avec `haccp_traceability_records`). Corrigée au passage :
**CONFIRMÉ = 18, PARTIEL = 1, ABSENT = 9, AUCUN UPDATE = 14** (total 42, recompte vérifié par script
après l'ajout de la nouvelle ligne — voir §2.5).

**Aucune action corrective nécessaire avant bascule Postgres** pour cette colonne : le seul chemin
d'écriture (`CreateTraceabilityRecord`) pose `updated_at = now` explicitement à la création ; comme
aucun `UPDATE` ultérieur n'existe dans le code Go pour cette table, le retrait de `ON UPDATE` en
Postgres ne change rien d'observable (identique au raisonnement pour `goods_receipts`).

### 2.3 Inventaires

**[03-table-usage-audit.md](03-table-usage-audit.md)**, section HACCP : ligne ajoutée

```
| `haccp_traceability_records`, `haccp_traceability_photos` ⁽²⁾ | `internal/modules/haccp/repository.go` |
```

avec une note ⁽²⁾ (le renvoi ⁽¹⁾ étant déjà pris par `planning_day_comments`) expliquant que ces deux
tables sont **vivantes et nouvelles**, créées après l'audit initial des 180 tables de production —
hors décompte "143/180" et hors liste des 37 orphelines, même situation que `planning_day_comments`.

**[07-module-inventory.md](07-module-inventory.md)** : le module `haccp` existait déjà comme ligne
scorée (rang 32, score 69, Tier 3) — les 5 fonctions de traçabilité ont été ajoutées à
`internal/modules/haccp/repository.go` **après** la conversion Tier 3 de ce module (commit `0b4509f`
« ready for staging », après `94e6bf0` « Tier 3 »), donc absentes du score original. Ligne recomptée
en entier (grep exhaustif, même méthode documentée dans le fichier) plutôt que patchée d'un delta :

| Métrique | Avant | Après | Delta |
|---|---|---|---|
| Sites SQL | 51 | 60 | +9 |
| Placeholders dynamiques | 6 | 9 | +3 |
| Fonctions de date MySQL | 2 (`UTC_TIMESTAMP`) | 2 (`UTC_TIMESTAMP`) | 0 |
| `ON DUPLICATE KEY UPDATE` | 0 | 0 | 0 |
| Procédure stockée | non | non | — |
| Tests | oui (3) | oui (4) | +1 |
| **Score** | **69** | **84** | +15 |

7 des 9 nouveaux sites SQL sont directement attribuables aux 5 nouvelles fonctions (2×`ExecContext`
dans `CreateTraceabilityRecord`, 2×`QueryContext`/`QueryRowContext` dans `ListTraceabilityRecords`,
1×`QueryRowContext` dans `GetTraceabilityRecord`, 1×`QueryContext` dans
`findTraceabilityPhotosByRecordIDs`, 1×`QueryRowContext` dans `HasTraceabilityRecords`) ; l'écart
résiduel de 2 n'a pas été réconcilié précisément avec l'ancien total (voir §2.6 — pas d'impact
pratique, le recompte est reproductible par le grep documenté). +1 placeholder dynamique
supplémentaire vs. les 2 attendus : `findTraceabilityPhotosByRecordIDs` construit sa clause `IN (...)`
avec `strings.Repeat("?,", ...)` **et** `fmt.Sprintf`, comptés séparément par le grep (même convention
que les 3 autres clauses `IN` déjà présentes dans ce fichier pour `zoneIDs`/`actionIDs`/`surfaceIDs`).
Le score (84) reste dans la fourchette Tier 3 (51–100) — pas de reclassement de tier — mais dépasse
désormais `customers` (82) : la ligne est déplacée de son ancien rang (32, entre `orders` et
`delivery_sessions`) à son nouveau rang (37, entre `customers` et `pos`), les rangs intermédiaires
(33→32, 34→33, 35→34, 36→35, 37→36) décalés en conséquence. Détail méthodologique complet en note
⁽²⁾ du fichier.

### 2.4 Vérification critique — le code Go passe-t-il déjà par `dbx.GetDB` ?

**Oui, déjà converti — aucune conversion nécessaire.**

Les 5 fonctions (`CreateTraceabilityRecord`, `ListTraceabilityRecords`, `GetTraceabilityRecord`,
`findTraceabilityPhotosByRecordIDs`, `HasTraceabilityRecords`,
[repository.go:1721-1919](../../internal/modules/haccp/repository.go#L1721-L1919)) utilisent toutes
`dbx.GetDB(ctx, r.db)` / `dbx.GetDB(txCtx, r.db)`, jamais `r.db` directement. Confirmé par
`git log -p` sur `repository.go` : ce code a été **ajouté dans le commit `0b4509f` (« ready for
staging »), après le commit `94e6bf0` (« Tier 3 »)** qui a converti tout le reste du module — la
traçabilité a donc été écrite nativement avec le pattern `dbx` déjà en place, pas de code MySQL brut à
migrer après coup.

Vérification des fonctions MySQL-spécifiques dans le texte SQL de ces 5 fonctions : aucune. Les
placeholders sont des `?` génériques (réécrits en `$1, $2...` par `dbx.Rebind` selon le dialecte actif
— voir [`internal/database/dbx/dialect.go`](../../internal/database/dbx/dialect.go)) ;
`created_at`/`updated_at` sont posés côté Go (`time.Now().UTC()`), jamais via `UTC_TIMESTAMP()`/`NOW()`
en SQL ; le littéral `TRUE` dans l'`INSERT` de `CreateTraceabilityRecord` est valide tel quel dans les
deux dialectes (accepté par MySQL comme alias de `1`, et type booléen natif en Postgres). Aucune
traduction de fonction n'était donc nécessaire.

#### Test réel contre Postgres Docker de dev

Le fichier [`postgres_integration_test.go`](../../internal/modules/haccp/postgres_integration_test.go)
(tagué `postgres_integration`, déjà présent depuis le Tier 3, étendu dans le même commit `0b4509f`
pour couvrir les 4 fonctions publiques de traçabilité — y compris un cas d'échec volontaire, `photo_key`
trop long déclenchant un rollback de la transaction) n'avait jamais pu être exécuté avec succès contre
Postgres tant que le schéma des deux tables n'existait pas côté cible.

Environnement : `docker-compose.postgres.yml` (Postgres 16, `localhost:5433`, base `welloresto_dev`).
Docker Desktop n'était pas démarré en début de chantier ; démarré, puis conteneur lancé — **volume
créé pour la première fois sur cette machine** (base entièrement vierge, pas de schéma préexistant).
Schéma chargé intégralement (fichier complet, une seule session `psql`, une seule transaction) :

```
docker compose -f docker-compose.postgres.yml up -d
docker exec -i welloresto-postgres-dev psql -U welloresto -d welloresto_dev -v ON_ERROR_STOP=1 \
  < docs/migration-postgres/04-schema-postgres-target.sql
→ 0 erreur, se termine par COMMIT (schéma complet, 462 statements)
```

Puis exécution du test réel :

```
DB_DIALECT=postgres POSTGRES_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev?sslmode=disable" \
  go test -tags postgres_integration ./internal/modules/haccp/... -run TestHACCPRepository_Postgres -v
=== RUN   TestHACCPRepository_Postgres
--- PASS: TestHACCPRepository_Postgres (0.18s)
PASS
```

`TestHACCPRepository_Postgres` est un test unique couvrant tout le module (settings, cleaning,
temperature, corrective actions **et** traçabilité) — le `PASS` global confirme que l'ajout du schéma
n'a rien cassé ailleurs, et que les 4 fonctions de traçabilité (y compris le cas d'échec `photo_key`
trop long avec rollback, et le `ON DELETE CASCADE` implicite testé via le cleanup) fonctionnent contre
Postgres réel sans aucune modification de `repository.go`. `go build ./...` et `go vet
./internal/modules/haccp/...` : OK.

### 2.5 Revalidation `pglast`

Même méthode que les rapports [13](13-merchant-id-schema-update.md)/[18](18-order-id-schema-update.md)/[26](26-planning-day-comments-integration.md)/[28](28-varchar-widening.md)/[53](53-audit-logs-column-width.md)/[55](55-generated-id-column-widths-full-audit.md) :

```
python3 -c "
import pglast
with open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8') as f:
    sql = f.read()
stmts = pglast.parse_sql(sql)
print('PARSE OK -', len(stmts), 'statements')
"
→ PARSE OK - 462 statements
```

457 (dernier compte, rapport 55) + 5 nouveaux (2 `CREATE TABLE` + 3 `CREATE INDEX`) = 462. Cohérent.

### 2.6 Écarts non réconciliés (transparence méthodologique)

Deux écarts mineurs rencontrés pendant ce chantier n'ont pas pu être réconciliés précisément avec les
valeurs historiques des rapports précédents, sans impact pratique :

- Le recompte des sites SQL du module `haccp` (§2.3) trouve +9 par rapport à l'ancien total, alors que
  seuls 7 sites sont directement attribuables au code de traçabilité ajouté. Écart résiduel de 2, non
  investigué plus avant (le recompte complet, reproductible par grep, est considéré plus fiable qu'un
  ancien total qu'on ne peut plus recalculer dans son contexte d'origine).
- Le nombre de fichiers de test du module (§2.3) passe de 3 à 4 dans ce recompte, mais les 4 fichiers
  existants (`cleaning_computed_test.go`, `date_normalization_test.go`, `postgres_integration_test.go`,
  `status_test.go`) existaient déjà tous au commit `94e6bf0` (Tier 3) d'après `git log
  --diff-filter=A` — l'écart ne vient donc pas non plus de ce chantier.

Dans les deux cas, la valeur retenue est le recompte frais et reproductible (commandes `grep`
documentées dans ce rapport), pas un ajustement du delta sur l'ancienne valeur.

---

## Partie 3 — Vérification élargie : d'autres tables manquantes ?

### Méthode

Extraction de tous les `CREATE TABLE` de `migrations/done/*.sql` (69 occurrences brutes, dont 2 en
commentaire et 1 en `CREATE TABLE ... LIKE ...` sans parenthèses — filtrées manuellement), noms de
table dédupliqués, chacun vérifié contre `04-schema-postgres-target.sql` :

```
grep -rhoiE "CREATE TABLE( IF NOT EXISTS)? \`?[a-zA-Z_0-9]+\`?" migrations/done/*.sql \
  | sed -E "s/CREATE TABLE( IF NOT EXISTS)? //I; s/\`//g" | tr 'A-Z' 'a-z' | sort -u
```

61 noms de table uniques trouvés (hors bruit de commentaires French matchés par erreur). Trois sont
absents de `04-schema-postgres-target.sql` :

### Résultat : 1 vraie découverte, 2 faux positifs

| Table | Migration | Verdict |
|---|---|---|
| `cleaning_tasks` | `011_haccp_cleaning_and_reception.sql` | **Faux positif** — `DROP TABLE IF EXISTS cleaning_tasks;` dans `012_haccp_cleaning_redesign.sql` (ligne 5) : table créée puis supprimée avant la refonte du module cleaning, n'existe plus en production. Absence de `04` correcte, pas un trou. |
| `booked_location_dedup` | `052_booked_location_unique.up.sql` | **Faux positif** — table de travail transitoire : `CREATE TABLE booked_location_dedup LIKE booked_location`, `INSERT ... SELECT DISTINCT`, puis `RENAME TABLE booked_location TO booked_location_old, booked_location_dedup TO booked_location`, `DROP TABLE booked_location_old`. À la fin de la migration, `booked_location_dedup` n'existe plus sous ce nom (devenu `booked_location`, déjà présent dans `04`). |
| **`discount_redemptions`** | `041_cart_discounts.up.sql` | **Vraie découverte** — voir ci-dessous |

### `discount_redemptions` — troisième trou, même famille que `planning_day_comments`/`haccp_traceability`

`migrations/done/041_cart_discounts.up.sql` (commit du 2026-06-21, dans `migrations/done/` donc déjà
exécutée) crée une table entière **et** modifie deux tables existantes :

```sql
ALTER TABLE discounts
    ADD COLUMN discount_scope ENUM('PRODUCT', 'ORDER_TOTAL') NOT NULL DEFAULT 'PRODUCT' AFTER discount_code,
    ADD COLUMN max_redemptions INT NULL DEFAULT NULL,
    ADD COLUMN max_redemptions_per_customer INT NULL DEFAULT NULL;

CREATE TABLE discount_redemptions (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    discount_id          VARCHAR(64) NOT NULL,
    order_id             BIGINT UNSIGNED NOT NULL,
    merchant_id          VARCHAR(64) NOT NULL,
    customer_id          VARCHAR(64) NULL,
    amount_applied_cents INT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_discount_order (discount_id, order_id),
    KEY idx_discount_redemptions_discount (discount_id),
    KEY idx_discount_redemptions_customer (discount_id, customer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE orders
    ADD COLUMN cart_discount_id VARCHAR(64) NULL DEFAULT NULL AFTER HT,
    ADD COLUMN cart_discount_code VARCHAR(64) NULL DEFAULT NULL AFTER cart_discount_id,
    ADD COLUMN cart_discount_amount INT NOT NULL DEFAULT 0 AFTER cart_discount_code;
```

**Aucune des trois modifications n'apparaît dans `04-schema-postgres-target.sql`** :
- `discount_redemptions` : table absente.
- `discounts` ([`04-schema-postgres-target.sql:1099`](04-schema-postgres-target.sql#L1099)) :
  colonnes `discount_scope`/`max_redemptions`/`max_redemptions_per_customer` absentes.
- `orders` : colonnes `cart_discount_id`/`cart_discount_code`/`cart_discount_amount` absentes
  (`grep cart_discount` sur le fichier : 0 résultat).

**Ni la table ni les colonnes n'apparaissent non plus dans `wello-resto-mysql-ddl.md`** (le dump
phpMyAdmin du 2026-07-13 audité pour générer `04`) — exactement la même situation que
`planning_day_comments`/`haccp_traceability` : la migration existait déjà dans le repo Git avant la
date du dump (commit du 2026-06-21, cataloguée dans le tout premier audit,
[01-audit.md](01-audit.md) ligne 28, dès 2026-07-13), mais n'avait apparemment pas encore été
**exécutée contre la base MySQL réelle** au moment où le dump a été pris — `migrations/done/` indique
qu'elle l'a été *depuis*, mais après la date du dump. Le décalage entre écriture de la migration et
exécution réelle en base (fréquent avec un hébergement à accès limité type Hostinger, cf.
`CLAUDE.md`) explique ce troisième trou.

**Nuance importante par rapport à `haccp_traceability` : cette table n'est pas « vivante » au même
sens.** Recherche exhaustive dans `internal/` : aucune requête SQL (`repository.go` d'aucun module)
ne référence `discount_redemptions` ni les colonnes `cart_discount_id`/`cart_discount_code`/
`cart_discount_amount`/`discount_scope`/`max_redemptions*`. Les seules occurrences sont des champs de
DTO Go (`CartDiscountID`/`CartDiscountCode`/`CartDiscountAmount` dans
`internal/models/create_order_models.go`, `internal/models/orders_model.go`,
`internal/models/request_objects.go`) — la fonctionnalité « code promo panier » semble donc
**schéma-only et DTO-only à ce stade** : le schéma MySQL et les structures de requête existent, mais
aucun repository ne lit ni n'écrit encore ces colonnes/cette table. Risque réel mais différé : si la
fonctionnalité est câblée côté repository avant la bascule Postgres (ou si le frontend envoie déjà ces
champs en s'attendant à ce qu'ils soient persistés), la table manquante casserait silencieusement la
persistance sans qu'aucun test actuel ne le détecte, puisqu'aucun test n'exerce ce chemin non plus.

**Hors périmètre de ce chantier** (Partie 2 ne couvrait explicitement que
`haccp_traceability_records`/`_photos`) — **non corrigé ici**, seulement documenté pour éviter la
découverte en pleine nuit de bascule demandée par la consigne. Mériterait son propre chantier dédié,
sur le modèle de celui-ci : traduction DDL de la table + des deux `ALTER TABLE`, audit `ON UPDATE`
(aucune colonne concernée ici, `discount_redemptions` n'a que `created_at` sans `ON UPDATE`),
inventaires 03/07 (avec la nuance « schéma présent, code applicatif pas encore branché » plutôt que
« vivante »), et vérification du code Go (aujourd'hui : rien à convertir, puisque rien n'y accède).

### Pas d'autre trou trouvé

Aucune autre table créée dans `migrations/done/` n'est absente de `04-schema-postgres-target.sql` —
le balayage couvre l'intégralité des fichiers `done/` (104 fichiers `.sql`, numéros 001 à 062 puis
067), pas seulement les migrations les plus récentes.

---

## Résumé

| Étape | Fichier(s) | Statut |
|---|---|---|
| Renumérotation collision | `migrations/069_widen_audit_logs_id.*`, `migrations/070_widen_loyalty_program_id_columns.*` | ✅ renommés, ordre de production préservé |
| Cross-références | `53-audit-logs-column-width.md`, `55-generated-id-column-widths-full-audit.md` | ✅ liens corrigés, récit historique conservé |
| Traduction DDL | `04-schema-postgres-target.sql` | ✅ 2 tables ajoutées, FK réelle conservée, ordre alphabétique dérogé (justifié) |
| Largeur `GeneratePrefixedID` | idem | ✅ déjà `varchar(64)` d'emblée côté MySQL, aucun élargissement nécessaire |
| Audit `ON UPDATE` | `05-on-update-timestamp-audit.md` | ✅ AUCUN UPDATE (INSERT seul), aucune action corrective ; décompte stale corrigé au passage |
| Inventaire tables | `03-table-usage-audit.md` | ✅ ajoutées (vivantes, nouvelles, hors décompte 143/180) |
| Inventaire modules | `07-module-inventory.md` | ✅ `haccp` recompté (69→84), reclassé rang 32→37 (Tier inchangé) |
| Code Go | `internal/modules/haccp/repository.go` | ✅ déjà `dbx.GetDB` natif, aucune modification nécessaire |
| Test réel Postgres | `postgres_integration_test.go` | ✅ PASS contre Postgres Docker de dev (schéma chargé pour la première fois) |
| Validation `pglast` | `04-schema-postgres-target.sql` | ✅ 462 statements, aucune erreur |
| `go build`/`go vet` | tout le repo / module `haccp` | ✅ OK |
| Vérification élargie | `migrations/done/*.sql` (61 tables uniques) | ✅ 1 vraie découverte (`discount_redemptions` + colonnes `discounts`/`orders`), 2 faux positifs écartés, aucune autre |

**Aucun fichier `.go` modifié** — le code de traçabilité était déjà conforme (`dbx.GetDB` natif depuis
sa création). **Rien commité** (conformément à la consigne) ; le conteneur Docker
`welloresto-postgres-dev` reste démarré avec le schéma complet chargé (volume créé pour la première
fois sur cette machine — base de dev locale prête pour de prochains tests d'intégration, sans données
autres que celles insérées puis nettoyées par le test ci-dessus).

**Reste ouvert, hors périmètre explicite de ce chantier** : la collision de numérotation préexistante
sur 062 (§1.3), et surtout `discount_redemptions`/colonnes `cart_discount_*`/`discount_scope`
(§ Partie 3) — mériterait un rapport dédié avant la bascule, sur le même modèle que celui-ci.

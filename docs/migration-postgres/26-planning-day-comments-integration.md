# 26 — Intégration de `planning_day_comments` à la préparation Postgres

Contexte : `planning_day_comments` (migration `065_planning_day_comments.{up,down}.sql`) et le module
`internal/modules/planning/daycomments/` ont été ajoutés **après** les audits initiaux
([01](01-audit.md), [03](03-table-usage-audit.md), [04](04-schema-postgres-target.sql),
[05](05-on-update-timestamp-audit.md), [07](07-module-inventory.md)) — la table et le module sont
déjà fonctionnels sur MySQL, mais absents de la préparation Postgres. Objectif de ce chantier :
rattraper les 4 documents concernés sans toucher au code du module (déjà écrit et fonctionnel).
**Aucun fichier `.go` modifié.**

## 1. Traduction DDL — `04-schema-postgres-target.sql`

Source MySQL (`migrations/065_planning_day_comments.up.sql`) :

```sql
CREATE TABLE IF NOT EXISTS planning_day_comments (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  comment_date DATE NOT NULL,
  comment TEXT NOT NULL,
  created_by VARCHAR(64) NULL,
  updated_by VARCHAR(64) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_planning_day_comments_merchant_date (merchant_id, comment_date),
  KEY idx_planning_day_comments_merchant_range (merchant_id, comment_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

Traduction ajoutée dans `04-schema-postgres-target.sql`, insérée en ordre alphabétique entre
`planned_shifts` et `planning_holiday_overrides` (le fichier est trié alphabétiquement par nom de
table de bout en bout — `planning_day_comments` < `planning_holiday_overrides` — insertion à cet
endroit plutôt qu'en fin de bloc `planning_*` pour respecter cette règle) :

```sql
-- ---------------------------------------------------------------------
-- planning_day_comments
--   nouvelle table (migration 065, non presente dans le dump wello-resto-mysql-ddl.md audite) ; module internal/modules/planning/daycomments
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes) ; CONFIRME cependant, voir docs/migration-postgres/05-on-update-timestamp-audit.md #41 (Upsert du module set explicitement updated_at)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | ... (liste standard reprise identique aux autres tables planning_*)
-- ---------------------------------------------------------------------
CREATE TABLE planning_day_comments (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    comment_date date NOT NULL,
    comment text NOT NULL,
    created_by varchar(64),
    updated_by varchar(64),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_planning_day_comments_uq_planning_day_comments_merchant_date ON planning_day_comments (merchant_id, comment_date);
CREATE INDEX idx_planning_day_comments_idx_planning_day_comments_merchant_ra ON planning_day_comments (merchant_id, comment_date);
```

Règles appliquées (identiques au reste du fichier, cf. [04-schema-mapping-notes.md](04-schema-mapping-notes.md)) :

- `VARCHAR(64)` conservé tel quel pour `id`/`merchant_id`/`created_by`/`updated_by` (pas de
  changement de type, contrairement au cas `merchant.id` documenté en [13](13-merchant-id-schema-update.md)).
- `DATE` → `date`, `TEXT` → `text` (mapping direct, aucune ambiguïté).
- `DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP` → `timestamptz NOT NULL DEFAULT now()` pour
  `created_at`/`updated_at`, conforme à la règle transverse Tier 1 (`UTC_TIMESTAMP()`/`CURRENT_TIMESTAMP` MySQL →
  `now()` Postgres, colonnes cibles en `timestamptz` donc `now()` correct sans `AT TIME ZONE`).
- `ON UPDATE CURRENT_TIMESTAMP` de `updated_at` retiré (pas d'équivalent déclaratif en Postgres) —
  note générique standard ajoutée en en-tête de table, cf. §2 ci-dessous pour la vérification que le
  code compense bien cette perte.
- **Noms d'index** : suit la règle `idx_/uq_<table>_<nom_mysql>` tronqué à 63 caractères
  ([04-schema-mapping-notes.md:28](04-schema-mapping-notes.md)). La contrainte unique
  (`uq_planning_day_comments_uq_planning_day_comments_merchant_date`, 63 caractères) tient
  exactement sans troncature ; l'index secondaire déborde de 3 caractères
  (`idx_planning_day_comments_idx_planning_day_comments_merchant_range` = 66 c.) et a donc été
  tronqué à `idx_planning_day_comments_idx_planning_day_comments_merchant_ra` (63 c., pas de
  collision avec un autre nom de cette table — aucun dédoublonnage nécessaire).
- **Note FK candidate** `merchant_id` : liste standard reprise à l'identique des tables `planning_*`
  voisines (même heuristique que le reste du fichier).
- **Note collation** : `utf8mb4_unicode_ci` (charset de la migration 065) → note standard PG
  "collation par défaut sensible à la casse", identique aux autres tables `planning_*`.
- Aucune colonne booléenne dans cette table → pas de note heuristique BOOLEAN.
- Aucune note "FK candidate" pour `created_by`/`updated_by` : l'heuristique du fichier ne détecte
  que les colonnes nommées littéralement `merchant_id`/`user_id` (cf. `planned_shifts.user_id` qui,
  lui, a une note) ; `created_by`/`updated_by` ne matchent aucun nom de PK cible et n'ont, de fait,
  aucune note ailleurs dans le fichier pour ce même motif (vérifié par grep).

## 2. Audit `ON UPDATE CURRENT_TIMESTAMP` — `05-on-update-timestamp-audit.md`

Méthode identique au reste du document : localisation de tous les `UPDATE`/upserts touchant la
table dans `internal/modules/planning/daycomments/repository.go`, vérification si `updated_at`
apparaît explicitement dans la clause `SET`/`ON DUPLICATE KEY UPDATE`.

Un seul chemin de mutation existe pour cette table — pas d'`UPDATE` classique, uniquement un upsert :

```go
// internal/modules/planning/daycomments/repository.go:61-83 (Upsert)
now := time.Now().UTC()
...
_, err := db.ExecContext(ctx, `
    INSERT INTO planning_day_comments (
        id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    ON DUPLICATE KEY UPDATE
        comment = VALUES(comment),
        updated_by = VALUES(updated_by),
        updated_at = VALUES(updated_at)
`, ..., now, now)
```

`updated_at` est explicitement mis à jour (`updated_at = VALUES(updated_at)`, avec `now` calculé
côté Go à chaque appel) — **classification CONFIRMÉ**, ajoutée en ligne 21 du tableau récapitulatif
de [05-on-update-timestamp-audit.md](05-on-update-timestamp-audit.md) (les lignes 21 à 41
existantes ont été décalées de +1, ainsi que les 3 sections de détail numérotées après ce point —
`### 24.` → `### 25.` (`planning_revenue_forecasts`), `### 33.` → `### 34.` (`printers`),
`### 36.` → `### 37.` (`subscription_invoices`) ; décompte global mis à jour : **CONFIRMÉ 16 → 17**,
41 colonnes auditées au total contre 40).

**Aucune action corrective nécessaire avant bascule Postgres** pour cette table : contrairement aux
cas ABSENT/PARTIEL du même document (ex. `kiosks.updated_at`, `employees.updated_at`), le retrait de
`ON UPDATE CURRENT_TIMESTAMP` ne changera aucun comportement observable, le code applicatif portant
déjà l'entière responsabilité de rafraîchir `updated_at`.

## 3. Inventaire des tables — `03-table-usage-audit.md`

Ajoutée à la section "Planning" de l'inventaire des tables vivantes (§1), avec une note distincte
(⁽¹⁾) car cette table est **postérieure** à l'audit initial des 180 tables de production : elle
n'entre ni dans le décompte "143/180" référencées, ni dans la liste des 37 orphelines — c'est une
addition après coup, pas une table pré-existante reclassée.

```
| `planning_day_comments` ⁽¹⁾ | `internal/modules/planning/daycomments/repository.go` |
```

## 4. Inventaire des modules — `07-module-inventory.md`

Score de risque calculé avec la même formule que le reste du document (`08-conversion-pattern-reference.md`) :

```
score = (nb sites SQL) + 2×(placeholders dyn.) + 3×(fonctions date MySQL) + 1×(ON DUPLICATE) + 5×(procédure stockée)
```

Comptage sur `internal/modules/planning/daycomments/repository.go` (seul fichier avec des appels
`database/sql` du module — `handler.go`/`service.go` n'en ont aucun) :

| Critère | Valeur | Détail |
|---|---|---|
| Sites SQL | 4 | `QueryContext` (`ListByDateRange`), `QueryRowContext` (`GetByDate`), `ExecContext` ×2 (`Upsert`, `Delete`) |
| Placeholders dynamiques | 0 | Aucun `strings.Repeat`/`fmt.Sprintf` construisant du SQL |
| Fonctions de date MySQL | 0 | `created_at`/`updated_at` posés côté Go (`time.Now().UTC()`), aucune fonction de date dans le texte SQL |
| `ON DUPLICATE KEY UPDATE` | 1 | `Upsert` |
| Procédure stockée | non | — |
| Tests | oui (1) | `service_test.go` |

**Score = 4 + 0 + 0 + 1 + 0 = 5** → **Tier 1** (score ≤ 10), confirmé par le calcul, pas juste par
intuition sur sa simplicité.

Ajouté au tableau de synthèse avec un rang partagé (6) avec `bookingcore` (même score 5), juste
après ce dernier — tous les rangs des lignes suivantes ont été recalculés (classement par
compétition 1-2-2-4, comme le reste du tableau) pour rester cohérents avec l'insertion d'une ligne
supplémentaire. Ajouté à la liste des modules Tier 1 recommandés dans la section "Ordre de
conversion recommandé".

Contrairement aux autres modules de la table `planning_*` (regroupés dans l'agrégat `planning` à
182, Tier 4, car ils forment un seul owner de schéma historique), `planning/daycomments` est traité
comme une ligne indépendante : c'est un sous-package ajouté après cet audit, avec sa propre table
neuve et aucune dépendance vers les autres sous-packages `planning/*` — rien ne justifie de le
noyer dans le score agrégé Tier 4 alors qu'il peut être converti en isolation, en Tier 1.

## 5. Validation syntaxique (`pglast`)

Revalidation du fichier complet avec le même parseur Postgres que les chantiers précédents
([13](13-merchant-id-schema-update.md), [18](18-order-id-schema-update.md)) :

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

Le bloc `planning_day_comments` isolé (`CREATE TABLE` + 2 `CREATE INDEX`) a également été validé
séparément : 3 statements, aucune erreur de parsing (`CreateStmt`, `IndexStmt` × 2).

## Résumé

| Étape | Fichier | Statut |
|---|---|---|
| Traduction DDL | `04-schema-postgres-target.sql` | ✅ ajoutée, ordre alphabétique respecté |
| Audit `ON UPDATE` | `05-on-update-timestamp-audit.md` | ✅ CONFIRMÉ, aucune action corrective requise |
| Inventaire tables | `03-table-usage-audit.md` | ✅ ajoutée (table vivante, nouvelle, hors décompte 180) |
| Inventaire modules | `07-module-inventory.md` | ✅ score 5, Tier 1, ligne indépendante de l'agrégat `planning` |
| Validation `pglast` | `04-schema-postgres-target.sql` | ✅ 457 statements, aucune erreur |

**Aucun fichier `.go` du module `internal/modules/planning/daycomments/` n'a été modifié** — le
module reste tel qu'écrit et fonctionnel sur MySQL. Ce chantier ne fait que rattraper la
documentation de préparation Postgres pour qu'elle reflète l'état réel du schéma de production.

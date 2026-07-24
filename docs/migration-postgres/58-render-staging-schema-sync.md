# 58 — Synchronisation du schéma Postgres de staging Render avec `04-schema-postgres-target.sql`

Date : 2026-07-24
Branche : `staging`

## Objectif

Le schéma cible (`04-schema-postgres-target.sql`) a évolué depuis le chargement initial confirmé
au [rapport 48](48-render-staging-schema-check.md) (181 tables) et le chargement complet des
données réelles au [rapport 51](51-render-staging-chunked-load.md) (147/147 tables, 472 774
lignes) : les rapports [53](53-audit-logs-column-width.md), [55](55-generated-id-column-widths-full-audit.md),
[56](56-haccp-traceability-integration.md) et [57](57-discount-redemptions-schema.md) ont ajouté
des largeurs de colonnes et deux tables/un ensemble de colonnes au fichier cible, sans jamais les
appliquer contre l'instance Render elle-même. Objectif de ce chantier : combler cet écart —
**uniquement les deltas manquants**, sans toucher aux 181 tables déjà en place ni aux données déjà
chargées. Aucune donnée réelle n'est citée ci-dessous, aucune information de connexion (hôte, port,
identifiants) n'y figure. Rien n'a été commité.

## 0. Note de méthode — accès à la chaîne de connexion

`RENDER_STAGING_DATABASE_URL` n'était pas positionnée dans un shell frais au début de cette
session. Une tentative de `setx` côté utilisateur n'a pas suffi : les outils de cette session
s'exécutent comme processus enfants d'un harness déjà démarré, qui hérite du bloc d'environnement
figé à son propre lancement — une modification du registre par `setx` ne se propage qu'aux
**futures** sessions de connexion, pas aux processus enfants d'une session déjà en cours. Sur
proposition de l'utilisateur, la variable `POSTGRES_URL` déjà présente dans `.vscode/launch.json`
(fichier local, couvert par `.gitignore` ligne 2, jamais commité) a été utilisée à la place —
c'est la même instance de staging Render que celle des rapports 48 à 54.

Méthode reprise du [rapport 51 §0](51-render-staging-chunked-load.md#0-note-de-process--rotation-de-mot-de-passe) :
la chaîne de connexion a été lue une seule fois et écrite dans un fichier local temporaire hors
dépôt ; tous les scripts Go jetables suivants n'ont référencé que le **chemin** de ce fichier,
jamais la valeur en clair dans une commande shell. Fichier supprimé en fin de session (§6).

## 1. Audit en lecture seule — état avant modification

Connexion `pgx/v5`, session `default_transaction_read_only = on`, aucune écriture :

| Élément vérifié | État constaté |
|---|---|
| Nombre de tables (`information_schema.tables`, `BASE TABLE`) | **181** — inchangé depuis le rapport 48 |
| `audit_logs.id` | **`character varying(64)`** — déjà large, migration 069 déjà appliquée sur cette instance |
| `customer_loyalty_progress.loyalty_program_id` | `character varying(30)` — migration 070 **pas encore appliquée** |
| `customer_loyalty_progress_order.loyalty_program_id` | `character varying(30)` — idem |
| `customer_rewards.loyalty_program_id` | `character varying(30)` — idem |
| `haccp_traceability_records` | absente |
| `haccp_traceability_photos` | absente |
| `discount_redemptions` | absente |
| `discounts.discount_scope` / `max_redemptions` / `max_redemptions_per_customer` | absentes |
| `orders.cart_discount_id` / `cart_discount_code` / `cart_discount_amount` | absentes |

**Point notable** : `audit_logs.id` était déjà à `varchar(64)` sur Render staging avant ce
chantier — fait non documenté jusqu'ici pour cette instance précisément (le
[rapport 54](54-tasks-full-execution-validation.md), daté du même jour, confirme la même largeur
mais contre le **Docker de dev jetable**, pas contre Render ; les rapports 48-51, seuls rapports
antérieurs à toucher Render, ne vérifiaient pas cette colonne). Conformément à la consigne
« uniquement ce qui manque », cette colonne n'a **pas** été retouchée par ce chantier.

## 2. Comptage de référence avant modification

Comptage `SELECT count(*)` sur les 181 tables (pas seulement les 147 documentées au rapport 51,
pour une garantie plus large) :

```
tables=181  non_empty_tables=137  total_rows=472887
```

Écart de +113 lignes par rapport au total du rapport 51 (472 774, du 2026-07-23) : attendu et sans
rapport avec ce chantier — la tâche cron tourne en continu sur staging (`CLAUDE.md`, confirmé au
rapport 54 le même jour), donc un jour d'activité réelle sur l'instance explique cette dérive.
Non investiguée davantage, hors périmètre : ce chantier prend ce comptage comme **référence propre
avant/après**, pas comme une revalidation du chiffre du rapport 51.

## 3. Delta appliqué

Script SQL unique, construit en recopiant chaque instruction directement depuis
`04-schema-postgres-target.sql` (lignes 39, 1104-1116, 1132/1148-1149, 1531-1543, 1554-1563,
2364-2366 — jamais reformulé de mémoire), validé par `pglast` avant exécution (22 instructions,
0 erreur), exécuté dans **une seule transaction** (`BEGIN;`/`COMMIT;`) contre Render via un script
Go jetable (`pgx/v5`) :

| Étape | Détail |
|---|---|
| Migration 069 (`audit_logs.id`) | **Non rejouée** — déjà à `varchar(64)` (§1) |
| Migration 070 (`loyalty_program_id` ×3) | `ALTER TABLE ... ALTER COLUMN loyalty_program_id TYPE varchar(64)` sur les 3 tables |
| `haccp_traceability_records` | `CREATE TABLE` + 2 index (`merchant_id`, `merchant_id, created_at`) |
| `haccp_traceability_photos` | `CREATE TABLE` + `FOREIGN KEY (record_id) REFERENCES haccp_traceability_records ON DELETE CASCADE` + index sur `record_id` |
| `discounts_discount_scope_enum` | `CREATE TYPE ... AS ENUM ('PRODUCT', 'ORDER_TOTAL')` |
| `discounts` | 3 colonnes ajoutées (`discount_scope` + defaut `'PRODUCT'`, `max_redemptions`, `max_redemptions_per_customer`) |
| `orders` | 3 colonnes ajoutées (`cart_discount_id`, `cart_discount_code`, `cart_discount_amount` + défaut `0`) |
| `discount_redemptions` | `CREATE TABLE` + 1 index unique + 2 index simples |

19 instructions exécutées (le script encadrant `BEGIN;`/`COMMIT;` retiré au profit d'une
transaction Go explicite), toutes `OK`, `ALL_OK` en sortie. Aucune instruction destructive
(`DROP`, `DELETE`, `TRUNCATE`) dans tout le script — uniquement `CREATE TABLE`/`CREATE INDEX`/
`CREATE TYPE`/`ALTER TABLE ... ADD COLUMN`/`ALTER TABLE ... ALTER COLUMN ... TYPE` (élargissement).

## 4. Vérification après modification

Ré-audit en lecture seule, même méthode qu'au §1 :

```
table_count = 184
audit_logs.id = character varying(64)
customer_loyalty_progress.loyalty_program_id = character varying(64)
customer_loyalty_progress_order.loyalty_program_id = character varying(64)
customer_rewards.loyalty_program_id = character varying(64)
haccp_traceability_records = PRESENT
haccp_traceability_photos = PRESENT
discount_redemptions = PRESENT
discounts.discount_scope / max_redemptions / max_redemptions_per_customer = PRESENT
orders.cart_discount_id / cart_discount_code / cart_discount_amount = PRESENT
```

**184 tables = 181 + 3**, conforme à l'attendu.

Re-comptage complet des lignes sur les 184 tables :

```
tables=184  non_empty_tables=137  total_rows=472887
```

Total et nombre de tables non vides **strictement identiques** au comptage de référence du §2.
Diff table par table (comptage avant vs après, les 184 lignes de sortie comparées une à une) :
**seule différence, les 3 nouvelles tables apparues à 0 ligne** (`discount_redemptions`,
`haccp_traceability_photos`, `haccp_traceability_records`) — **0 écart** sur les 181 tables
préexistantes, aucune ligne perdue ni ajoutée par erreur.

## 5. Chargement de données réelles — non effectué (décision explicite)

Consigne initiale : régénérer les fichiers de données pour `haccp_traceability_records`/
`haccp_traceability_photos` en s'appuyant sur le dump réel local, `discount_redemptions` restant
probablement vide.

Vérification avant génération (`data-migration/transform_mysql_csv.py inspect-dump`, puis
recherche directe de `CREATE TABLE` par nom de table dans le dump brut) : **les trois tables sont
absentes de `data-migration/migration_welloresto_data.sql`** (fichier local réel, gitignored,
daté du 2026-07-20, jamais régénéré depuis — même fichier utilisé sans changement par tous les
rapports 33 à 51). Ce dump est donc antérieur à l'exécution réelle en MySQL production des
migrations `migrations/done/067_haccp_traceability.up.sql` et `migrations/done/041_cart_discounts.up.sql`
(toutes deux déjà exécutées côté MySQL d'après `migrations/done/`, mais pas encore reflétées dans
cet export figé) — **hypothèse de la consigne invalidée pour les 2 tables HACCP** (elle tenait
pour `discount_redemptions`, dont l'absence de données était bien anticipée).

Point remonté à l'utilisateur avant toute action supplémentaire : extraire ces données
nécessiterait une connexion directe à MySQL **production** (pas seulement au staging Postgres
concerné par ce chantier) — action distincte et plus sensible que celle prévue initialement.
**Décision de l'utilisateur : laisser les 3 tables vides, aucune connexion à MySQL production
effectuée.** Les 3 tables restent donc schéma-prêtes mais sans donnée, sur le même statut que
`discount_redemptions` documenté au rapport 57 (« schéma prêt, non câblé/non peuplé »).

## 6. Nettoyage

Supprimés en fin de session : les trois programmes Go jetables (`tools/render_audit`,
`tools/render_rowcounts`, `tools/render_apply_delta` — jamais commités, créés uniquement pour ce
chantier), le fichier local contenant la chaîne de connexion, les deux fichiers de comptage
avant/après. `git status` après nettoyage : identique à l'état de début de session, aucune trace
des artefacts de ce chantier. Aucun fichier du dépôt autre que ce rapport n'a été modifié.

## 7. Synthèse

| Étape | Résultat |
|---|---|
| Audit avant modification | ✅ 181 tables ; `audit_logs.id` déjà large ; 3× `loyalty_program_id`, 2 tables HACCP, `discount_redemptions`, 6 colonnes `discounts`/`orders` confirmées manquantes |
| Comptage de référence avant | ✅ 181 tables, 472 887 lignes |
| Delta appliqué | ✅ 19 instructions, transaction unique, `ALL_OK` — migration 069 non rejouée (déjà appliquée), reste du delta appliqué intégralement |
| Vérification après | ✅ 184 tables ; toutes les colonnes/tables attendues `PRESENT` |
| Comptage de référence après | ✅ 184 tables, 472 887 lignes — **0 écart** sur les 181 tables préexistantes (diff table par table) |
| Chargement données réelles (HACCP) | ❌ **Non fait** — tables absentes du dump local (antérieur aux migrations MySQL correspondantes) ; décision utilisateur : rester vide, pas d'accès MySQL production |
| Chargement données réelles (`discount_redemptions`) | ❌ Non fait — attendu vide, confirmé absent du dump également |
| Nettoyage | ✅ tous les artefacts locaux supprimés, `git status` identique à l'état initial |
| Fichiers commités | Aucun |

**Le schéma de staging Render est désormais aligné sur `04-schema-postgres-target.sql`** (184
tables, toutes les largeurs/colonnes/tables du delta présentes), **sans aucun impact sur les
données déjà chargées** (0 écart vérifié table par table). Point ouvert pour une prochaine
session : les 2 tables HACCP traçabilité et `discount_redemptions` restent vides sur Render tant
qu'aucun export MySQL production plus récent n'est disponible localement ou qu'une connexion
directe à la production n'est explicitement demandée et autorisée.

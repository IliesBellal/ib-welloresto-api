# 62 — Rechargement de staging Render depuis un extrait de production frais (données seules, structure intacte)

Date : 2026-08-07
Branche : `staging`

## Objectif

Vider **uniquement les données** du schéma Postgres de staging Render — aucune modification de
structure : tables, colonnes, contraintes, index, séquences restent identiques — puis recharger un
extrait frais de données réelles de production, chaque étape chronométrée.

**Aucune donnée réelle n'est citée dans ce rapport, et aucune information de connexion (hôte, port,
identifiants) n'y figure.** Aucun fichier du dépôt n'a été modifié hors ce rapport, rien n'a été
commité.

## 0. Note de méthode — accès à la chaîne de connexion

Méthode identique aux [rapports 51 §0](51-render-staging-chunked-load.md) et
[58 §0](58-render-staging-schema-sync.md) : `POSTGRES_URL` n'était pas positionnée dans
l'environnement du harness. La valeur a été lue **une seule fois** depuis `.vscode/launch.json`
(fichier local couvert par `.gitignore`, jamais commité) et écrite dans un fichier temporaire hors
dépôt. Tous les outils de cette session n'ont référencé que le **chemin** de ce fichier
(`PGURL_FILE`), jamais la valeur en clair dans une commande. Fichier supprimé en fin de session
(§8).

## 1. Écart constaté d'entrée : 187 tables, 3 FK (et non 184 / 2)

La consigne annonçait 184 tables et 2 clés étrangères. L'audit en lecture avant toute modification
donne :

| Élément | Attendu par la consigne | Constaté |
|---|---|---|
| Tables (`information_schema.tables`, `BASE TABLE`, schéma `public`) | 184 | **187** |
| Clés étrangères (`pg_constraint.contype = 'f'`) | 2 | **3** |

Les 3 FK réelles sont :

| Table portante | Référence | Action |
|---|---|---|
| `merchant_translation_languages` | `available_languages(code)` | — |
| `product_ratings` | `order_ratings(id)` | `ON DELETE CASCADE` |
| `haccp_traceability_photos` | `haccp_traceability_records(id)` | `ON DELETE CASCADE` |

La troisième est celle introduite par le [rapport 56](56-haccp-traceability-integration.md) et
appliquée sur l'instance par le [rapport 58](58-render-staging-schema-sync.md), postérieurement aux
rapports qui donnaient le chiffre de 2. L'écart 184 → 187 relève de la même dérive : le rapport 48
comptait 181 tables, le rapport 58 en a ajouté 3 (`haccp_traceability_records`,
`haccp_traceability_photos`, `discount_redemptions`) puis d'autres travaux ont suivi. **Le chiffre
de référence retenu pour cette session est donc l'état réel mesuré avant TRUNCATE (187), pas la
valeur de la consigne** — l'invariant vérifié étant « structure strictement identique avant/après »,
et non « égale à un nombre fixé d'avance ».

`CASCADE` a de toute façon été employé sur le `TRUNCATE`, ce qui rend l'ordre des tables et le
nombre exact de FK sans effet sur le résultat.

## 2. Empreinte structurelle de référence (avant modification)

Empreinte prise sur cinq inventaires triés puis hachés en SHA-256 :

| Inventaire | Source | Cardinalité |
|---|---|---|
| Tables | `information_schema.tables` (`BASE TABLE`) | 187 |
| Colonnes | `information_schema.columns` (type, longueur, précision, nullabilité, défaut, identity) | 1 886 |
| Contraintes | `pg_constraint` + `pg_get_constraintdef` | 1 588 |
| Index | `pg_indexes` (`indexdef` complet) | 298 |
| Séquences | `pg_sequences` (type, `start_value`, `increment_by`) | 84 |

`SHA256 = ff1ff058aba7cedd0311aa1973161b17d878bebb32c4e3fd1236417ef5020e0f`

Comptage de données avant : **187 tables, 141 non vides, 474 106 lignes**.

Répartition des 1 588 contraintes : 1 391 `NOT NULL`, 183 clés primaires, 11 `CHECK`, 3 clés
étrangères. Quatre tables sont donc sans clé primaire — état préexistant, hors périmètre.

## 3. Étape 1 — TRUNCATE des données seules

Une seule instruction, les 187 tables listées ensemble pour que `CASCADE` n'ait aucun ordre à
respecter :

```sql
TRUNCATE TABLE public."<table_1>", …, public."<table_187>" RESTART IDENTITY CASCADE;
```

```
tables_targeted=187
TRUNCATE_OK elapsed=2.0970911s
```

**Durée : 2,10 s.**

## 4. Étape 2 — Vérification après vidage

| Vérification | Résultat |
|---|---|
| Empreinte structurelle recalculée | `SHA256 = ff1ff05…20e0f` — **identique** |
| Diff ligne à ligne des 5 inventaires (4 043 lignes) | **0 différence** |
| Tables | 187 — inchangé |
| Colonnes / contraintes / index / séquences | 1 886 / 1 588 / 298 / 84 — inchangés |
| Lignes | **0 sur les 187 tables** (`NON_EMPTY=0`, `TOTAL_ROWS=0`) |
| Séquences remises à zéro | **84/84** avec `last_value IS NULL` (jamais appelée → prochain `nextval` = `start_value`) |

`RESTART IDENTITY` a donc bien porté sur l'intégralité des séquences, condition nécessaire au
rechargement d'identifiants identiques à la source.

## 5. Étape 3 — Régénération des fichiers SQL depuis l'export frais

### Hypothèse assumée sur la fraîcheur de l'export

La consigne prévoyait d'attendre une confirmation explicite du dépôt de l'export. Le fichier
présent dans `data-migration/migration_welloresto_data.sql` porte un en-tête de génération daté du
**06/08/2026 21:23** et une date de dépôt du **07/08/2026 00:55**, toutes deux postérieures à
l'ensemble des travaux antérieurs ; son volume de lignes diffère de celui de tous les rapports
précédents (§6). Il a donc été traité comme l'extrait frais attendu et la session a enchaîné sans
attendre. **Si ce fichier n'était pas l'export voulu, il suffit de redéposer le bon et de rejouer
les étapes 1 → 4 : elles sont idempotentes.**

```
python data-migration/transform_mysql_csv.py generate-all-sql \
  --dump data-migration/migration_welloresto_data.sql --output-dir <hors dépôt>
```

| Élément | Valeur |
|---|---|
| Durée (1ʳᵉ passe) | **46,6 s** |
| Durée (2ᵉ passe, uniquement pour capturer le rapport JSON) | 42,2 s |
| Fichiers produits | **147** (`001_…sql` → `147_…sql`), 244 Mio |
| Lignes attendues au total | **486 898** |
| Tables porteuses de lignes | 137 (10 tables générées mais vides à la source) |
| `failed_tables` | **aucune** |
| Tables ignorées (orphelines, hors schéma cible) | 33 |
| Tables ignorées (non mappées) | 3 — `outbound_messages`, `planning_published_shift_snapshots`, `user_status_view` |
| Tables nécessitant `OVERRIDING SYSTEM VALUE` | 58 |
| Lignes écartées (clé nulle) | 2, sur `orderitems` |
| Colonnes source écartées | `customer.is_migrated`, `orders.isDelivery`, `planning_settings.planning_sms_notifications_enabled` |

Les tables ignorées et les colonnes écartées correspondent aux décisions déjà actées aux rapports
[30](30-final-orphan-tables-list.md) et [35](35-dead-columns-removal.md) — aucun changement de
comportement du générateur dans cette session.

## 6. Étape 4 — Chargement sur staging

Le chargeur en instructions séparées du [rapport 51](51-render-staging-chunked-load.md) n'existait
plus dans l'arbre (supprimé en fin de session à l'époque, conformément à la politique de nettoyage).
Il a été réimplémenté à l'identique de sa spécification : découpage de chaque fichier en
instructions individuelles par une machine à états qui suit les littéraux `'…'` (échappement `''`)
et les commentaires `--`, puis re-découpage défensif de toute instruction `INSERT` dépassant
`MAX_STATEMENT_BYTES` (2 Mio) en plusieurs `INSERT` partageant le même en-tête de colonnes.

**Validation locale avant toute connexion** (mode `DRY_RUN`, aucun accès réseau) :

```
FILES=147 STATEMENTS=1501 OVERSIZE_REMAINING=0 ELAPSED=3.416s
```

**0 instruction restant au-dessus du seuil** après ajustement — même résultat qu'au rapport 51.

Chargement effectif :

```
OK 147/147 147_without.sql (10 statements) 874ms
FILES=147 STATEMENTS=1501 OVERSIZE_REMAINING=0 ELAPSED=4m48.842s
ALL_OK
```

**147/147 fichiers chargés, aucune erreur. Durée : 4 min 48,8 s.**

## 7. Étape 6 — Vérifications

### 7.1 Comptages exacts vs le nouvel extrait

```
tables_attendues=147  total_attendu=486898  total_chargé=486898  écarts=0
```

| Contrôle | Résultat |
|---|---|
| Tables comparées ligne à ligne au rapport de génération | **147/147, 0 écart** |
| Total | **486 898 / 486 898** |
| Tables hors périmètre de génération contenant des lignes | **0** (les 40 tables non générées sont restées vides) |
| Total base entière | 486 898 — soit exactement la somme attendue, aucune ligne parasite |

Comparaison avec l'extrait précédent : **474 106 → 486 898 lignes (+12 792)**, et 141 → 137 tables
non vides. L'écart de volume confirme au passage qu'il s'agit bien d'un extrait différent, plus
récent. La baisse du nombre de tables non vides s'explique par le périmètre : le comptage « avant »
portait sur les 187 tables de l'instance (dont certaines alimentées par les tests applicatifs et
les travaux des rapports 53-61), le comptage « après » sur les seules 147 tables générées.

### 7.2 Structure toujours intacte après chargement

Empreinte recalculée après le chargement complet :
`SHA256 = ff1ff058aba7cedd0311aa1973161b17d878bebb32c4e3fd1236417ef5020e0f` — **identique à la
référence prise avant le TRUNCATE**, diff des 5 inventaires vide. Le cycle
TRUNCATE → génération → chargement n'a modifié **aucun** élément de structure.

### 7.3 Séquences identity resynchronisées

Pour chaque séquence rattachée à une colonne (via `pg_depend`), comparaison de `last_value` au
`max()` réel de la colonne :

| Verdict | Nombre |
|---|---|
| `last_value` **exactement égal** à `max(colonne)` | **57** |
| `last_value` supérieur à `max(colonne)` | 0 |
| Table vide → séquence jamais appelée (prochain `nextval` = 1) | 27 |
| **Séquence en retard sur les données (collision au prochain `nextval`)** | **0** |

Les 57 séquences des tables chargées pointent donc exactement sur le maximum réel : le prochain
`nextval()` rendra `max + 1`, sans collision possible avec une ligne existante.

### 7.4 Vérifications applicatives (repository layer, `DB_DIALECT=postgres`)

Programme Go autonome appelant les **mêmes constructeurs et méthodes que l'application**, sur des
identifiants réels choisis à l'exécution dans les données chargées (jamais imprimés) :

| # | Vérification | Module | Durée | Résultat |
|---|---|---|---|---|
| 1 | `GetOrder` (commande la plus fournie du marchand principal) | `orders` | 803 ms | ✅ PASS — 17 lignes, 1 paiement |
| 2 | `GetOrder` sur une commande porteuse d'emplacement | `orders` | 306 ms | ✅ PASS — 2 emplacements joints |
| 3 | `GetCashRegisterReport` | `cash_registers` | 301 ms | ✅ PASS |
| 4 | `GetUserByToken` | `auth` | 89 ms | ✅ PASS |
| 5 | `FetchActiveSlots` + `ComputePOSStatus` | `openinghours` | 44 ms | ✅ PASS — 11 créneaux |
| 6 | `ListPlanningShiftsTeamWeekView` | `planning/schedule` | 48 ms | ✅ PASS — 13 shifts |
| 7 | `GetAttributes` | `menu` | 136 ms | ✅ PASS — 50 attributs |
| 8 | `INSERT` identity réel sur `qrcodes` (transaction annulée) | — | 135 ms | ✅ PASS — `nextval > max` chargé |

**8/8 PASS.**

Deux précisions de méthode :

- La vérification 2 a été ajoutée après coup : la sous-requête `locations` de l'agrégateur de
  commandes n'était pas exercée par la vérification 1, le marchand ayant le plus de commandes
  (18 974) n'ayant aucune ligne dans `order_location` — seuls 8 marchands en utilisent. Sans elle,
  cette jointure serait restée non testée.
- La vérification 4 s'exécute sur un jeton volontairement inexistant : elle valide l'exécution de la
  requête contre le schéma et les données réels et le signalement correct de l'absence, sans
  dépendre de Redis (où résident les jetons réels).
- La vérification 8 insère puis **annule** la transaction : elle prouve qu'un `nextval()` réel rend
  une valeur strictement supérieure au maximum chargé, sans laisser de ligne. Aucun test marqué
  `postgres_integration` n'a été exécuté contre Render, conformément à la consigne du rapport 51.

**Aucun résidu** : 0 ligne portant l'identifiant sentinelle utilisé, et re-comptage complet des
187 tables après cette phase strictement identique au comptage post-chargement (486 898 lignes,
diff vide).

### 7.5 Un incident de harness, aucun défaut applicatif

Le premier passage des vérifications a produit deux échecs
(`locations query error: invalid input syntax for type integer: ""` sur `GetOrder`, et « aucune
caisse chargée » sur `GetCashRegisterReport`). **Les deux venaient du programme de vérification, pas
de la migration** : ses requêtes de sélection d'identifiants utilisaient `ORDER BY id`, colonne
inexistante sur ces tables dont les clés primaires sont `order_id` et `cash_register_id` ; l'erreur
était avalée et rendait une chaîne vide, ensuite passée comme identifiant à la couche repository.
Le sélecteur a été corrigé (colonnes réelles) et rendu **fatal en cas d'absence de correspondance**,
précisément pour qu'un défaut d'outillage ne puisse plus se présenter comme un défaut de migration.
Aucune modification du code applicatif n'a été nécessaire.

## 8. Chronométrage — synthèse

| Étape | Durée mesurée |
|---|---|
| Empreinte structurelle avant + comptage des 187 tables | 15,8 s |
| **1. TRUNCATE des 187 tables (`RESTART IDENTITY CASCADE`)** | **2,10 s** |
| **2. Vérification post-vidage (empreinte + diff + comptages + séquences)** | **~17 s** |
| **3. Génération des 147 fichiers SQL depuis l'export** | **46,6 s** |
| Validation locale du découpage en instructions (`DRY_RUN`) | 3,4 s |
| **4. Chargement des 147 fichiers sur staging** | **4 min 48,8 s** |
| **6a. Comptages exacts des 187 tables** | **11,4 s** |
| 6b. Empreinte structurelle après chargement | 5,6 s |
| 6c. Contrôle des 84 séquences identity | 1,7 s |
| 6d. 8 vérifications applicatives | 5,0 s |
| **Total du cycle (TRUNCATE → chargement → vérifications)** | **≈ 6 min 37 s** |

Le chargement représente à lui seul 73 % du temps total ; la génération 12 % ; le vidage lui-même
est négligeable (0,5 %).

## 9. Nettoyage

Supprimés en fin de session : les 147 fichiers `.sql` régénérés (244 Mio), le rapport JSON de
génération, le journal de chargement, les empreintes structurelles et fichiers de comptage, **le
fichier temporaire contenant la chaîne de connexion**, les binaires de compilation, et les trois
programmes Go jetables créés sous `tools/` (`pgops`, `pgload`, `pgcheck` — nécessaires sous le
module pour importer les packages `internal/…`, jamais commités). `git status` ne montre aucune
trace de ces artefacts après nettoyage.

## 10. Synthèse

| Point | Résultat |
|---|---|
| Vidage des données seules | ✅ 187 tables, `TRUNCATE … RESTART IDENTITY CASCADE`, **2,10 s** |
| Structure préservée | ✅ Empreinte SHA-256 **identique** avant TRUNCATE, après TRUNCATE et après chargement — 0 différence sur 4 043 lignes d'inventaire |
| Base effectivement vide après vidage | ✅ 0 ligne sur 187 tables, 84/84 séquences remises à zéro |
| Génération depuis l'export frais | ✅ 147 fichiers, 486 898 lignes attendues, 0 échec, **46,6 s** |
| Chargement sur staging | ✅ **147/147, ALL_OK**, 1 501 instructions, **4 min 48,8 s** |
| Comptages vs extrait | ✅ **486 898 / 486 898, 0 écart**, aucune ligne hors périmètre |
| Séquences identity | ✅ 57/57 exactement resynchronisées, 0 en retard |
| Vérifications applicatives | ✅ **8/8 PASS**, 0 résidu |
| Écart de consigne relevé | 187 tables et 3 FK constatées (contre 184 / 2 annoncées) — dérive documentée §1, sans effet grâce à `CASCADE` |
| Fichiers commités | Aucun |

**Le cycle complet « vidage des données seules → rechargement d'un extrait de production frais » est
opérationnel et vérifié de bout en bout sur staging Render, en ≈ 6 min 37 s, sans aucune
modification de structure.**

# 42 — Correctif `planning_shifts.status` et 4ᵉ répétition générale de chargement complet

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Traiter le sentinel `ENUM` vide cartographié au rapport [41](41-enum-empty-value-audit.md)
(`planning_shifts.status`, 7 lignes sur 54) par une règle de génération scopée, puis rejouer le
protocole de répétition générale (rapports [36](36-full-data-load-rehearsal.md)→
[40](40-parent-order-id-and-full-integer-scan.md)) jusqu'au bout si possible. **Aucune donnée réelle
n'est citée.** Rien n'a été commité.

## 1. Règle de génération ajoutée

[data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py), même pattern que
les frozensets `ZERO_DATE_TO_NULL_COLUMNS`/`ZERO_DATE_TO_EPOCH_COLUMNS` (rapport 37) — scopé strictement
à `(planning_shifts, status)`, pas une règle générique sur les colonnes `ENUM` :

```python
EMPTY_ENUM_DEFAULT_COLUMNS: Dict[Tuple[str, str], str] = {
    ("planning_shifts", "status"): "planned",
}
```

Câblé dans `SqlFieldRules` (nouveau champ `empty_enum_default_fields`, un dict `colonne → défaut`
filtré par table, même construction que les autres champs scopés) et dans `format_sql_value` :

```python
# Empty-string ENUM sentinel: scoped to the exact (table, column) pair audited in
# doc 41, checked before any other rule so it can't be shadowed by the generic
# text-quoting fallback below (which would otherwise emit '' verbatim - not a
# valid label for a Postgres enum type).
if value.strip() == "" and lowered in rules.empty_enum_default_fields:
    return _sql_quote_string(rules.empty_enum_default_fields[lowered])
```

Vérifié directement, avant toute régénération complète :

```
>>> rules_shifts.empty_enum_default_fields
{'status': 'planned'}
>>> format_sql_value('status', '', rules_shifts)
"'planned'"
>>> rules_kiosks.empty_enum_default_fields   # autre table, autre colonne ENUM
{}
>>> format_sql_value('status', '', rules_kiosks)
"''"    # inchangé — la règle ne s'applique qu'à planning_shifts.status
```

La colonne cible reste `NOT NULL` sans `CHECK` ajouté ; aucune autre colonne `ENUM` du schéma n'est
affectée par cette règle (confirmé ci-dessus et revérifié après régénération complète, §2).

## 2. Régénération des 147 fichiers

`generate-all-sql` relancé sur le dump réel avec le générateur corrigé :

- **147/147 tables générées, 0 échec** (`failed_tables: {}`).
- **0 occurrence restante** de `''` sur `planning_shifts.status` dans le fichier de sortie — reparsing
  complet des 54 tuples `INSERT` de `101_planning_shifts.sql` : 0 vide, 54 valeurs dans les 4 libellés
  déclarés (dont les 7 anciennement vides, désormais `'planned'` — les 47 lignes qui avaient déjà cette
  valeur dans le dump restent inchangées, portant le total à 54 occurrences de `'planned'`).
- **Total toutes tables : 472 774 lignes** — identique aux rapports 38/39/40, dump source inchangé.
- `dropped_null_key_rows_by_table: {"orderitems": 2}` et
  `dropped_source_columns_by_table: {"customer": ["is_migrated"], "orders": ["isDelivery"]}` —
  inchangés, les deux règles antérieures toujours actives et inchangées.
- `planning_shifts` : 54 lignes (inchangé — cette règle reformate une valeur, ne supprime ni n'ajoute
  aucune ligne).

## 3. Chargement complet contre le Postgres Docker de dev

### Reset et schéma

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Conteneur recréé, prêt (`pg_isready`) en 2 s. Schéma cible chargé via `psql -v ON_ERROR_STOP=1` :
**0 erreur, 181 tables de base créées** — état courant du fichier, identique aux rapports précédents.

### Chargement séquentiel

Même protocole que les rapports 36/38/39/40 : un `psql -v ON_ERROR_STOP=1` par fichier, dans l'ordre
numérique, arrêt immédiat au premier échec.

**Le blocage du rapport 40 est confirmé résolu en conditions réelles** : `101_planning_shifts.sql`
charge maintenant intégralement (54 lignes, statut vérifié après coup — voir §3.3). Le chargement
dépasse ce point et progresse jusqu'au fichier **114/147** avant de rencontrer un **nouveau blocage,
d'une nature entièrement différente**, sans rapport avec les colonnes `ENUM`, `integer` ou `varchar`
déjà traitées.

### 3.1 — Nouveau blocage : `114_qrcodes.sql`, ligne 70

```
FAILED: 114_qrcodes.sql (exit 3)
BEGIN
INSERT 0 59
COMMIT
psql:/tmp/load/114_qrcodes.sql:70: ERROR:  column "QR_id" of relation "qrcodes" does not exist
```

**Point capital : ce n'est pas l'`INSERT` qui échoue.** La séquence `BEGIN` → `INSERT 0 59` → `COMMIT`
s'exécute et se valide intégralement — les 59 lignes de `qrcodes` sont bien committées en base
(confirmé : `SELECT count(*) FROM qrcodes` = 59, identique au comptage attendu). L'erreur survient sur
la **dernière instruction du fichier**, exécutée après le `COMMIT`, hors transaction :

```sql
SELECT setval(pg_get_serial_sequence('qrcodes', 'QR_id'), COALESCE(MAX(QR_id), 1), MAX(QR_id) IS NOT NULL) FROM qrcodes;
```

### 3.2 — Diagnostic exact (structurel, sans donnée)

- **Table** : `qrcodes`. **Colonne identity concernée** : déclarée `QR_id` (casse mixte) dans
  [04-schema-postgres-target.sql](04-schema-postgres-target.sql) (`QR_id integer GENERATED ALWAYS AS
  IDENTITY NOT NULL`) et dans [wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md) (`` `QR_id`
  int(11) NOT NULL``) — casse mixte héritée du MySQL source des deux côtés.
- **Cause exacte** : un identifiant Postgres non guillemeté (`QR_id` dans une clause `CREATE TABLE`,
  une `PRIMARY KEY`, ou une liste de colonnes d'un `INSERT`/`SELECT`) est **automatiquement replié en
  minuscules** par le parseur SQL — la colonne physique réelle est donc `qr_id` (vérifié :
  `information_schema.columns` ne connaît que `qr_id`, et `SELECT MAX(QR_id) FROM qrcodes` — identifiant
  non guillemeté — fonctionne, replié correctement). **Mais `pg_get_serial_sequence(table, column)`
  attend son 2ᵈ argument comme une chaîne de caractères ordinaire, pas un identifiant SQL parsé** — elle
  n'est **jamais** repliée en minuscules, et la fonction compare la chaîne telle quelle, sensible à la
  casse, au nom de colonne réellement stocké au catalogue. Le générateur
  ([transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py),
  `_setval_columns_for`/`SqlTableWriter.finalize`) construit ce 2ᵈ argument à partir de la casse
  d'origine du nom de colonne tel que déclaré dans le dump MySQL (`QR_id`, casse mixte) plutôt qu'à
  partir de sa forme repliée par Postgres (`qr_id`) — d'où `pg_get_serial_sequence('qrcodes', 'QR_id')`
  qui ne trouve aucune correspondance et lève l'erreur. Reproduit isolément et confirmé :
  `pg_get_serial_sequence('qrcodes', 'QR_id')` échoue avec exactement ce message,
  `pg_get_serial_sequence('qrcodes', 'qr_id')` (minuscule) réussit et renvoie
  `public.qrcodes_qr_id_seq`.
- **Ce n'est ni une colonne `ENUM`, ni une colonne `integer`/`varchar` issue d'une conversion de type**
  — c'est un bug de génération distinct : une chaîne de caractères passée en argument de fonction ne
  suit pas la même règle de repliement de casse qu'un identifiant SQL parsé. Sans rapport avec les
  chantiers des rapports 37 (sentinel de date), 38 (`orderitems.order_id`), 40 (`parent_order_id`) ou
  41 (`planning_shifts.status`).

### 3.3 — Volume et portée

- **1 seule colonne identity dans tout le schéma cible porte un nom de casse mixte** : balayage complet
  des 181 tables / colonnes `GENERATED ALWAYS AS IDENTITY` du schéma cible — `qrcodes.QR_id` est la
  **seule** occurrence (toutes les autres colonnes identity du schéma sont déjà tout-minuscules,
  d'où l'absence de ce bug sur les 113 fichiers chargés avant celui-ci, chacun avec son propre
  `setval()` réussi).
- **Impact données : nul.** Les 59 lignes de `qrcodes` sont intégralement committées avec les bonnes
  valeurs (vérifié par comptage). Seule l'instruction de resynchronisation de séquence, placée après le
  `COMMIT`, échoue.
- **Impact différé (non corrigé)** : la séquence `qrcodes_qr_id_seq` reste à son état par défaut
  post-`CREATE TABLE` (`last_value = 1`, `is_called = false`) alors que le maximum réel chargé dans
  `qr_id` est nettement supérieur. Une future insertion applicative sur `qrcodes` s'appuyant sur la
  génération identity obtiendrait `nextval() = 1`, en collision directe avec la ligne `qr_id = 1` déjà
  chargée — violation de clé primaire à la première tentative. Ce risque ne s'est pas manifesté dans
  cette session (aucune insertion applicative testée sur `qrcodes`), mais existe tant que cette
  instruction n'est pas rejouée avec succès.
- **Fichiers restants jamais atteints** : 33 fichiers (`115_receipts.sql` à `147_without.sql`),
  conformément à la consigne d'arrêt immédiat, sans enchaîner.

**Aucune correction n'a été appliquée pour ce nouveau blocage** — ni au générateur, ni au schéma, ni au
Postgres de dev (la séquence `qrcodes_qr_id_seq` reste non resynchronisée) — conformément à la consigne
(« si un nouveau blocage apparaît, arrête-toi et documente-le... sans le corriger toi-même »).

## 4. Vérifications faites malgré l'arrêt

### 4.1 — Comptages sur les tables chargées

114 fichiers ont été tentés (`001_...` à `114_qrcodes.sql`, ce dernier avec ses données committées
malgré l'échec de son instruction finale). Comptage exact sur les 114 tables correspondantes, comparé
aux `row_counts` du rapport de génération :

**113/114 tables : comptage strictement identique. 1 écart, expliqué et non lié à la migration
(§4.3, `labels`).** Aucune autre table parmi les 114 tentées ne présente d'écart — en particulier
`planning_shifts` (54/54, tous valides — voir §3, aucune valeur vide) et `qrcodes` (59/59, malgré
l'échec de son `setval()`).

Les 33 tables non tentées ont, comme attendu, 0 ligne chacune (base non atteinte) — hormis
`subscription_invoices` et `user_vacations`, qui ont 0 ligne attendue de toute façon (non discriminant).

### 4.2 — Requêtes applicatives réelles (Go, `DB_DIALECT=postgres`)

Exécutées via les suites de tests d'intégration existantes du dépôt (tag `postgres_integration`,
`POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev`), qui exercent
réellement le code Go de production contre ce Postgres de dev :

| Fonction | Module | Résultat |
|---|---|---|
| `GetOrder` | `internal/modules/orders` | ✅ `TestOrdersRepository_Postgres` — PASS |
| `GetCashRegisterReport` | `internal/modules/cash_registers` | ✅ `TestGetCashRegisterReport_Postgres` — PASS |
| `GetUserByToken` | `internal/modules/users` | ✅ `TestUsersRepository_Postgres` — PASS (fixture propre au test — la table `users` du dump n'est pas atteinte par le chargement en masse, cf. §3.3 ; le test ne dépend donc pas des données en masse) |
| `ComputePOSStatus` (+ `FetchActiveSlots`) | `internal/modules/openinghours` | ✅ `TestFetchActiveSlots_Postgres` — PASS |
| Suite complète `planning/schedule` | `internal/modules/planning/schedule` | ✅ `TestScheduleRepository_Postgres` — PASS (pertinent pour ce chantier : confirme que le correctif `planning_shifts.status` n'a rien cassé côté lecture/écriture applicative Go) |
| `CreateOrder` (cycle complet commande) | `internal/modules/order_life_cycle` | ✅ `TestOrderLifeCycleRepository_Postgres` — PASS (voir §4.4, utilisé aussi pour la vérification de séquence) |

**6/6 appels représentatifs réussissent contre le Postgres de dev réellement chargé.**

### 4.3 — Note méthodologique : effets de bord des tests d'intégration partagés

Deux suites parmi celles listées en §4.2 ont eu un effet de bord sur des données de référence
partagées, sans rapport avec la correction de ce rapport — signalés ici par souci de traçabilité, ni
corrigés ni imputables au générateur ou au schéma :

- **`labels` (−3 par rapport à l'attendu)** : `TestGetCashRegisterReport_Postgres`
  ([internal/modules/cash_registers/postgres_integration_test.go:43](../../internal/modules/cash_registers/postgres_integration_test.go#L43))
  exécute, avant et après son propre scénario, un nettoyage non scopé à ses propres données
  (`DELETE FROM labels WHERE label_type = 'delivery_type' AND lang = 'FR' AND label_value IN ('IN',
  'TAKE_AWAY','DELIVERY')`) — ce filtre correspondait exactement à 3 lignes déjà présentes dans le
  chargement en masse (labels de type de livraison), supprimées avant que le test insère ses propres
  lignes identiques, puis re-supprimées par son propre nettoyage final. Les 3 lignes d'origine ne sont
  donc plus présentes dans **cette instance de dev jetable** (le dump sur disque n'est pas affecté ; un
  rechargement les restituerait). Comportement préexistant du fichier de test, hors périmètre de cette
  tâche, non modifié.
- **`hours_of_operation` (+5 constatés puis nettoyés manuellement)** :
  `TestFetchActiveSlots_Postgres`
  ([internal/modules/openinghours/repository_pg_test.go](../../internal/modules/openinghours/repository_pg_test.go))
  enregistre son nettoyage via `t.Cleanup`, mais ferme sa connexion via un `defer database.Close()`
  classique déclaré *avant* — les `defer` s'exécutent au retour de la fonction de test, avant les
  callbacks `t.Cleanup` de la phase de teardown : le `DELETE` de nettoyage s'exécute donc sur une
  connexion déjà fermée, et son erreur est silencieusement ignorée (`_, _ = ...`). Constaté (5 lignes
  `merchant_id = 'test-openinghours-pg'` restées après un `PASS`), nettoyé manuellement pour ne pas
  fausser les comptages du §4.1. Comportement préexistant du fichier de test, hors périmètre de cette
  tâche, non modifié.

Ces deux observations concernent la suite de tests existante, pas la chaîne de génération/chargement
qui est l'objet de ce rapport — mentionnées pour expliquer intégralement le seul écart du §4.1.

### 4.4 — Resynchronisation des séquences identity : vérification par insertion applicative réelle

Sur `orders` (chargée intégralement au fichier `090_orders.sql`, bien avant le blocage `qrcodes`),
`TestOrderLifeCycleRepository_Postgres` exécute un `CreateOrder` complet (commande + articles + extra +
without + localisation + commentaires + paiement) via le code applicatif réel
(`OrdersLifeCycleRepository.CreateOrder`, `dbx.InsertReturningID`) :

| Mesure | Avant le test | Après le test |
|---|---:|---:|
| `MAX(order_id)` sur `orders` | 33 255 | 33 255 (la ligne de test a été retirée par le nettoyage du test lui-même) |
| `orders_order_id_seq` (`last_value`) | 33 264 | **33 265** |
| Doublons de `order_id` sur `orders` | — | **0** |

La séquence avance exactement de 1 (un seul `nextval()` consommé par l'unique commande créée), sans
aucune collision de clé primaire — **la resynchronisation issue de `090_orders.sql` fonctionne
correctement en conditions réelles**, avec une nouvelle valeur strictement supérieure à toutes les
valeurs chargées en masse. (`last_value` était déjà à 33 264 avant ce test précis car
`TestOrdersRepository_Postgres`, exécuté plus tôt dans cette même session, insère et nettoie ses propres
lignes de commande — cohérent, aucune anomalie.)

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés (contenant de vraies données), leur copie dans le conteneur Postgres
de dev (`/tmp/load` et fichiers de log associés), et les artefacts d'analyse locaux (requêtes de
comptage, rapport JSON de génération) ont tous été supprimés à la fin de la session. Les 5 lignes de
test résiduelles sur `hours_of_operation` ont été nettoyées manuellement (§4.3). Le Postgres de dev est
laissé dans l'état atteint par cette répétition (114 tables avec des données, dont `labels` avec 3
lignes de moins que l'attendu pour la raison expliquée au §4.3 ; `qrcodes_qr_id_seq` non resynchronisée ;
33 tables encore vides) — aucune remise à zéro supplémentaire n'a été demandée après l'arrêt, cohérent
avec les rapports précédents. **Rien n'a été commité.**

## 6. Synthèse

| Étape | Résultat |
|---|---|
| Règle ajoutée | `EMPTY_ENUM_DEFAULT_COLUMNS` scopée à `(planning_shifts, status)`, `'' → 'planned'` — même pattern que les rapports 37/38 |
| Régénération | 147/147, 0 échec, 0 `''` restant sur `planning_shifts.status`, 472 774 lignes au total (inchangé) |
| Blocage du rapport 40 (`orders.parent_order_id`) | Reste résolu (rapport 40) |
| Blocage du rapport 41 (`planning_shifts.status`) | **Résolu et vérifié en conditions réelles** — 101/147 charge intégralement, 54/54 lignes valides en base |
| Chargement | Progresse de 90/147 (rapport 39/40) à **114/147** |
| Nouveau blocage (114/147, `qrcodes`) | Nature différente — pas une colonne `ENUM`/`integer`/`varchar`, mais `pg_get_serial_sequence()` sensible à la casse sur un nom de colonne identity à casse mixte (`QR_id`, 1 seule occurrence dans tout le schéma) ; **données intactes** (59/59 lignes committées), seule la resynchronisation de séquence échoue ; **non corrigé** |
| Comptages sur les tables tentées | 113/114 exacts ; 1 écart (`labels`, −3) expliqué comme effet de bord de test, sans lien avec la migration |
| Requêtes applicatives Go réelles | 6/6 : `GetOrder`, `GetCashRegisterReport`, `GetUserByToken`, `ComputePOSStatus`, suite `planning/schedule`, `CreateOrder` — toutes PASS |
| Resynchronisation de séquence (`orders`) | Vérifiée par insertion réelle : +1 sur la séquence, 0 doublon, ID généré strictement supérieur au maximum chargé |
| Fichiers commités | Aucun |

Point bloquant pour la suite : `qrcodes.QR_id` — corriger la construction de l'argument texte de
`pg_get_serial_sequence()` dans le générateur (utiliser la forme repliée en minuscules du nom de
colonne, cohérente avec ce que Postgres attend d'un identifiant non guillemeté) pour permettre au
chargement de dépasser le fichier 114/147. Une fois ce point traité, les 33 fichiers restants
(`115_receipts.sql` à `147_without.sql`) n'ont encore fait l'objet d'aucune tentative de chargement dans
cette session.

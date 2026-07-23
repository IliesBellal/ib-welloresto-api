# 39 - Répétition générale de chargement complet v3, avec les correctifs des rapports 37/38 (structurel uniquement, aucune donnée réelle)

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Troisième répétition générale de chargement complet, rejouant le protocole des rapports
[36](36-full-data-load-rehearsal.md) et [38](38-full-data-load-rehearsal-v2.md) avec les 147 fichiers
`.sql` régénérés depuis le dump réel, incluant les correctifs déjà en place dans
[data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py) : sentinel de date
`0000-00-00` (rapport [37](37-zero-date-sentinel-audit.md)) et exclusion des 2 lignes
`orderitems.order_id` (rapport 38, section 7). Même protocole : reset complet du Postgres de dev,
régénération des 147 fichiers dans un dossier temporaire hors dépôt, chargement séquentiel strict
(`ON_ERROR_STOP=1`, arrêt immédiat sans enchaîner sur le fichier suivant en cas d'échec), puis, si tout
charge, vérifications complètes (comptages sur les 147 tables, requêtes applicatives Go, resynchronisation
des séquences identity).

**Résultat : nouvelle progression.** Le chargement dépasse le point de blocage du rapport 38 (fichier
89/147, `orderitems`) — confirmant que les deux correctifs 37/38 tiennent en conditions réelles — et
atteint le fichier **90/147** avant de s'arrêter sur une erreur Postgres bloquante d'une troisième nature,
différente des deux précédentes : une erreur de syntaxe SQL (`syntax error at or near ":"`), causée par un
jeton non numérique non guillemeté sur une colonne cible entière. Conformément à la consigne, l'exécution
ne s'est pas poursuivie au-delà de ce point : les vérifications complètes (comptages sur 147 tables,
requêtes applicatives par tier, séquences identity) n'ont donc **pas** été exécutées, à l'exception d'une
vérification de comptage limitée aux tables effectivement chargées avant l'arrêt (section 3).

Aucune valeur de donnée réelle n'est citée dans ce document — uniquement noms de tables, de colonnes, de
fichiers, comptages, et une description structurelle du jeton fautif (format observé, sans reproduire sa
valeur).

## 1. Remise à zéro du Postgres de dev

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Conteneur `welloresto-postgres-dev` recréé avec un volume vide, prêt (`pg_isready`) en moins de 2 secondes.
Schéma cible [04-schema-postgres-target.sql](04-schema-postgres-target.sql) chargé via
`psql -v ON_ERROR_STOP=1` : **0 erreur**, 181 tables de base créées
(`information_schema.tables`, `table_type = 'BASE TABLE'`) — identique aux rapports 36/38. Le schéma
chargé est l'état courant du fichier sur cette branche, y compris les modifications non committées en
cours sur ce chantier (retrait de `customer.is_migrated` et `orders.isDelivery` documenté au rapport
[35](35-dead-columns-removal.md), élargissement de 3 colonnes `varchar` — `customer_loyalty_progress.id`,
`customer_loyalty_progress_order.progress_id`, `users.token` — cohérent avec la migration
`066_widen_varchar_columns`).

## 2. Régénération et chargement séquentiel des 147 fichiers

### Régénération

`generate-all-sql` relancé sur le dump réel, dans un dossier temporaire hors dépôt et hors de tout dossier
synchronisé :

- **147/147 tables générées, 0 échec** (`failed_tables: {}`).
- **0 occurrence** du motif `0000-00-00` restante sur les 147 fichiers de sortie ; le littéral epoch
  `'1970-01-01T00:00:00Z'` apparaît exactement 1 fois dans `083_merchant_parameters.sql` et exactement 6
  fois dans `067_integration_uber_eats.sql` — comptages identiques aux rapports 37/38, confirmant que la
  règle de conversion du sentinel de date est toujours active et inchangée.
- `dropped_null_key_rows_by_table: {"orderitems": 2}` — confirme que la règle d'exclusion arbitrée au
  rapport 38 (section 7) est toujours active et se déclenche toujours exactement sur les 2 mêmes lignes.
- `dropped_source_columns_by_table` inchangé : `{"customer": ["is_migrated"], "orders": ["isDelivery"]}`.
- Total toutes tables : **472 774 lignes** — identique au total post-correction du rapport 38 (section 8 :
  472 776 − 2 lignes exclues d'`orderitems` = 472 774), confirmant que le dump source n'a pas changé.

### Chargement

Chargement un par un, dans l'ordre numérique (`001_...` à `147_...`), via `psql -v ON_ERROR_STOP=1` dans
une boucle shell, chaque fichier dans sa propre session `psql` (chaque fichier contient son propre
`BEGIN;`/`COMMIT;`, suivi d'un appel `SELECT setval(pg_get_serial_sequence(...), ...)` pour resynchroniser
la séquence identity de la table venant d'être chargée).

**89 fichiers chargés avec succès** (`001_allergens.sql` à `089_orderitems.sql` — ce qui inclut
`089_orderitems.sql` elle-même, le fichier bloquant du rapport 38 : la correction est donc confirmée en
conditions réelles, la table charge intégralement), puis **arrêt** au 90ᵉ fichier :

```
FAILED: 090_orders.sql
ERROR:  syntax error at or near ":"
```

### Diagnostic (structurel, sans valeur de donnée)

- **Table concernée** : `orders`.
- **Colonne concernée** : `parent_order_id` (`integer` dans le schéma cible — voir
  [04-schema-postgres-target.sql](04-schema-postgres-target.sql), `CREATE TABLE orders`). Cette colonne a
  été convertie de `varchar(50)` (MySQL source) vers `integer` lors du chantier d'unification des
  identifiants de commande documenté aux rapports
  [15](15-fk-type-mismatch-audit.md)/[16](16-order-id-format-check.md)/[17](17-order-id-unification.md)/[18](18-order-id-schema-update.md)
  — self-référence vers `orders.order_id`, destinée à contenir soit `NULL`, soit l'identifiant numérique
  d'une commande précédente (voir commentaire MySQL source dans
  [wello-resto-mysql-ddl.md](wello-resto-mysql-ddl.md) : *« Deliveroo : Previous brand_order_id before
  remake »*).
- **Cause** : sur 12 lignes de la table (voir portée ci-dessous), la valeur source de `parent_order_id`
  n'est pas un identifiant numérique de commande mais un jeton texte au format `<préfixe deux lettres>:
  <UUID>` (36 caractères hexadécimal-tirets, précédés de deux lettres et de `:`) — vraisemblablement un
  identifiant externe Deliveroo, pas une self-référence vers `orders.order_id`. Le générateur classe
  `parent_order_id` comme colonne numérique (type cible `integer`) et, pour ce genre de colonne,
  retranscrit la valeur source telle quelle sans la citer entre guillemets (comportement correct pour un
  entier). Un jeton non numérique contenant un `:` traverse donc ce chemin tel quel, comme fragment SQL nu
  — et Postgres le rejette avec une erreur de syntaxe au moment du parsing de l'`INSERT` (contrairement au
  cas `orderitems.order_id` du rapport 38, où le jeton nu `null` était interprétable comme le mot-clé SQL
  `NULL` et provoquait une violation de contrainte plutôt qu'une erreur de syntaxe — même classe de bug de
  formatage déjà signalée comme risque ouvert au rapport 38, section 10 : *« une chaîne source non
  numérique passe telle quelle, non guillemetée, sur une colonne cible numérique »*, jusqu'ici non couverte
  par une correction générique).
- **Volume affecté dans ce fichier** : un balayage structurel étendu à l'ensemble des 147 fichiers générés
  (reparsing complet des tuples `INSERT`, y compris à travers les multiples lots `INSERT` par fichier —
  méthode affinée par rapport aux rapports 37/38 pour couvrir les fichiers multi-lots, chaque colonne
  cible numérique de chaque table vérifiée pour des jetons non numériques non guillemetés sur l'ensemble
  des tuples générés) montre **12 occurrences sur 32 849 lignes** de `orders`, **une seule colonne
  concernée** (`parent_order_id`), **aucune autre table ni colonne du schéma cible affectée par cette
  classe d'erreur** — la nouvelle méthode de balayage (multi-lots) a aussi été revalidée sur `orders`
  elle-même : le total de lignes rescannées (32 849) correspond exactement au `row_counts["orders"]` du
  rapport de génération, confirmant qu'aucun lot n'a été manqué.

### Portée du problème au-delà du fichier bloquant

**1 table concernée, 12 occurrences au total**, toutes sur `orders.parent_order_id` — un ordre de
grandeur comparable au blocage du rapport 38 (18 occurrences sur 2 tables) et bien plus faible que le
sentinel de date (244 occurrences, 7 tables). `orders` est chargée juste après `orderitems` (89) — la
première table de premier plan explicitement dans le périmètre applicatif de cette tâche à être atteinte
(`GetOrder`, `GetCashRegisterReport` en dépendent directement).

**Aucune décision de conversion n'a été prise** — ni sur le sens à donner à ces 12 valeurs (candidats
naturels différents selon l'interprétation métier : `NULL` si la self-référence Deliveroo n'a pas
d'équivalent dans `orders.order_id` côté Postgres, ou une table de correspondance séparée si ces
identifiants externes doivent être préservés sous une autre forme — hors périmètre d'une simple règle de
formatage), ni sur une modification du générateur ou du schéma cible. Aucun fichier n'a été modifié.

## 3. Vérification limitée aux tables chargées avant l'arrêt

Comptage `SELECT count(*)` sur les 89 tables effectivement chargées, comparé au comptage attendu
(`row_counts` du rapport JSON de génération) :

**89/89 tables : comptage identique, 0 écart.** `orderitems` (75 282 lignes, cohérent avec le rapport 38
section 8) compte parmi les 89 tables vérifiées avec 0 écart —
confirmation supplémentaire de la correction du rapport 38 en conditions réelles. `orders` elle-même est
vide (`SELECT count(*) FROM orders` = 0) après l'échec — le fichier `090_...` contient son propre
`BEGIN;`/`COMMIT;` et l'erreur a interrompu la transaction avant le `COMMIT;`, donc les lignes déjà
envoyées dans cette transaction avant le point d'échec ont bien été annulées (rollback automatique à la
fermeture de session `psql`), aucune insertion partielle n'est restée en base.

## 4. Vérifications non exécutées (bloquées par l'arrêt du chargement)

Conformément à la consigne (« si un fichier échoue, arrête-toi... et n'enchaîne pas sur les suivants »),
les étapes suivantes **n'ont pas été exécutées** :

- Chargement des 57 fichiers restants (`090_...` à `147_...`).
- Comptage de lignes complet sur les 147 tables.
- Requêtes applicatives réelles à travers le code Go (`GetOrder`, `GetCashRegisterReport`,
  `GetUserByToken`, `ComputePOSStatus`, et autres appels représentatifs des Tiers 1-4) : non tentées.
  `orders` n'a pas fini de charger (le fichier `090` a été atteint mais sa transaction a été annulée) donc
  `orders` elle-même est vide — `GetOrder` et `GetCashRegisterReport` en dépendent directement,
  `GetUserByToken` dépend de `users` (fichier `143`, jamais atteint), `ComputePOSStatus` dépend de tables
  situées après `orders` dans l'ordre de chargement. Une base partiellement chargée (89/147 tables, dont
  aucune table de premier plan pour ces requêtes) ne reflète pas l'état de production visé par ces
  vérifications.
- Vérification de la resynchronisation des séquences identity via une insertion applicative de test sur
  `orders` : non tentée, pour la même raison (`orders` vide, la vérification n'aurait rien confirmé de
  représentatif — chaque fichier `.sql` généré contient déjà son propre appel
  `SELECT setval(pg_get_serial_sequence(...), ...)` après son `COMMIT;`, mécanisme déjà observé fonctionner
  sur les 89 tables chargées avec succès, mais non exercé ici via le code applicatif Go faute d'une base
  suffisamment chargée pour ce test précis).

Ces vérifications restent à faire dans une prochaine répétition, une fois le traitement des 12 valeurs non
numériques sur `orders.parent_order_id` arbitré, de la même manière que le sentinel de date (rapports
36→37) et le cas `orderitems.order_id` (rapport 38) l'ont été.

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés (contenant de vraies données) ont été supprimés du dossier temporaire à
la fin de la session, ainsi que leur copie dans le conteneur Postgres de dev utilisée pour le chargement
(`/tmp/load`), le fichier de comptage de vérification (`/tmp/rowcheck.sql`), la copie du schéma cible
(`/tmp/04-schema-postgres-target.sql`), et les artefacts d'analyse structurelle générés localement (script
de balayage, journal de chargement, rapport JSON de génération, résultats de comptage). Aucun fichier de
sortie contenant de vraies données n'a été conservé. Le Postgres de dev est laissé dans l'état atteint par
cette répétition (89 tables chargées, `orders` et les 58 tables suivantes vides) — aucune remise à zéro
supplémentaire n'a été demandée après l'arrêt. Rien n'a été commité ; aucun fichier du dépôt n'a été
modifié par cette tâche (diagnostic uniquement, aucune règle de conversion arbitrée ni appliquée au
générateur).

## 6. Conclusion

| Étape | Résultat |
|---|---|
| Régénération des 147 fichiers | OK — 147/147, 0 échec, 0 sentinel de date résiduel, exclusion `orderitems.order_id` toujours active |
| Reset Postgres dev + schéma cible | OK — 0 erreur, 181 tables créées (schéma courant, y compris modifications non committées de ce chantier) |
| Chargement séquentiel | **Arrêté au fichier 90/147** (`orders`) — contre 89/147 au rapport 38, 20/147 au rapport 36 |
| Confirmation des correctifs des rapports 37/38 | OK — sentinel de date et exclusion `orderitems.order_id` tiennent tous deux en conditions réelles ; `orderitems` (89) charge intégralement |
| Cause du nouveau blocage | Erreur de syntaxe SQL — jeton non numérique non guillemeté sur une colonne cible `integer`, même classe de bug que le rapport 38 (signalée comme risque ouvert non couvert) mais manifestation différente (erreur de syntaxe plutôt que violation de contrainte `NOT NULL`) |
| Portée réelle du nouveau problème | 1 table, 1 colonne, 12 occurrences (`orders.parent_order_id`, sur 32 849 lignes) |
| Tables chargées avant l'arrêt | 89/89 comptages exacts, 0 écart ; `orders` vide (transaction annulée proprement) |
| Vérifications applicatives Go / séquences identity | Non exécutées (bloquées par l'arrêt — `orders` et `users` toutes deux hors de portée) |

Point bloquant pour la suite : décider, pour `orders.parent_order_id` (12 lignes portant un jeton externe
Deliveroo au lieu d'une self-référence numérique), comment traiter ces valeurs — candidats à arbitrer
plutôt qu'à décider ici (conversion vers `NULL`, ou conservation sous une autre forme si ces 12
identifiants externes ont une valeur métier à préserver) — avant de pouvoir régénérer et rejouer cette
répétition jusqu'à son terme. Note transverse : la classe de bug (chaîne source non numérique traversant
telle quelle le formatage d'une colonne cible numérique) a maintenant été rencontrée deux fois
(`orderitems.order_id` au rapport 38, `orders.parent_order_id` ici) avec deux symptômes Postgres
différents ; un audit dédié au formatage des colonnes numériques (sur le modèle du rapport 37 pour le
sentinel de date), couvrant les 147 tables plutôt qu'au cas par cas, reste une option pour la suite si ce
schéma de risque devait se reproduire une troisième fois.

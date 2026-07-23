# 38 - Répétition générale de chargement complet v2, avec traitement du sentinel de date (structurel uniquement, aucune donnée réelle)

Date: 2026-07-21 (répétition initiale) ; mise à jour 2026-07-21 (correction des deux `NULL`-sur-`NOT NULL` implémentée et vérifiée)
Branche: migration/postgres

## Objectif

Rejouer la répétition générale du rapport [36-full-data-load-rehearsal.md](36-full-data-load-rehearsal.md)
avec les 147 fichiers `.sql` régénérés après la correction du sentinel MySQL `0000-00-00`/`0000-00-00
00:00:00` documentée dans [37-zero-date-sentinel-audit.md](37-zero-date-sentinel-audit.md) — même
protocole : reset complet du Postgres de dev, régénération des 147 fichiers, chargement séquentiel
strict (`ON_ERROR_STOP=1`, arrêt immédiat sans enchaîner sur le fichier suivant en cas d'échec),
puis, si tout charge, vérifications complètes (comptages, requêtes applicatives Go, resynchronisation
des séquences identity).

**Résultat : la correction du sentinel de date fonctionne** — le chargement dépasse largement le point
de blocage du rapport 36 (fichier 20/147, `cash_registers`) et atteint le fichier **89/147** avant de
s'arrêter sur une erreur Postgres réelle et bloquante, d'une nature différente (contrainte `NOT NULL`
violée par une valeur `NULL` explicite sur une colonne qui n'est pas nullable dans le schéma cible —
sans rapport avec le sentinel de date). Conformément à la consigne, l'exécution ne s'est pas poursuivie
au-delà de ce point : les vérifications complètes (comptages sur 147 tables, requêtes applicatives par
tier, séquences identity) n'ont donc **pas** été exécutées, à l'exception d'une vérification de
comptage limitée aux tables effectivement chargées avant l'arrêt (section 3), comme au rapport 36.

Aucune valeur de donnée réelle n'est citée dans ce document — uniquement noms de tables, de colonnes,
de fichiers, comptages et messages d'erreur Postgres génériques (le message d'erreur Postgres reproduit
en section 2 a été tronqué pour retirer la ligne `DETAIL` de la commande originale, qui contenait les
valeurs de la ligne source concernée).

## 1. Remise à zéro du Postgres de dev

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Conteneur `welloresto-postgres-dev` recréé avec un volume vide, prêt (`pg_isready`) en moins de 2
secondes. Schéma cible [04-schema-postgres-target.sql](04-schema-postgres-target.sql) chargé via
`psql -v ON_ERROR_STOP=1` : **0 erreur**, 181 tables de base créées (`information_schema.tables`,
`table_type = 'BASE TABLE'`) — identique au rapport 36.

## 2. Régénération et chargement séquentiel des 147 fichiers

### Régénération

`generate-all-sql` (générateur modifié depuis le rapport 37, incluant la règle de conversion du
sentinel de date) relancé sur le dump réel, dans un dossier temporaire hors dépôt et hors de tout
dossier synchronisé :

- **147/147 tables générées, 0 échec** (`failed_tables: {}`).
- **0 occurrence** du motif `0000-00-00` restante sur les 147 fichiers de sortie ; le littéral epoch
  `'1970-01-01T00:00:00Z'` apparaît exactement 1 fois dans `083_merchant_parameters.sql` et exactement
  6 fois dans `067_integration_uber_eats.sql` — comptages identiques au rapport 37, confirmant que la
  règle de conversion des 8 colonnes auditées s'applique toujours à l'identique.
- `dropped_source_columns_by_table` inchangé : `{"customer": ["is_migrated"], "orders": ["isDelivery"]}`.
- Total toutes tables : 472 776 lignes, identique au rapport 37 (même dump source, non modifié depuis).

### Chargement

Chargement un par un, dans l'ordre numérique (`001_...` à `147_...`), via `psql -v ON_ERROR_STOP=1`
dans une boucle shell, chaque fichier dans sa propre session `psql` (chaque fichier contient son propre
`BEGIN;`/`COMMIT;`).

**88 fichiers chargés avec succès** (`001_allergens.sql` à `088_order_location.sql` — ce qui inclut
`020_cash_registers.sql`, le fichier bloquant du rapport 36 : la correction du sentinel de date est
donc confirmée en conditions réelles), puis **arrêt** au 89ᵉ fichier :

```
FAILED: 089_orderitems.sql
ERROR:  null value in column "order_id" of relation "orderitems" violates not-null constraint
```

### Diagnostic (structurel, sans valeur de donnée)

- **Table concernée** : `orderitems`.
- **Colonne concernée** : `order_id` (`integer`, `NOT NULL` dans le schéma cible — voir
  [04-schema-postgres-target.sql](04-schema-postgres-target.sql) ligne 2227 ; la colonne fait aussi
  partie de la clé primaire composite `(order_item_id, order_id, product_id)`, ligne 2245).
- **Cause** : une ligne source contient une valeur `NULL` explicite pour `order_id` — un article de
  commande sans commande associée. Contrairement au sentinel `0000-00-00` (rapport 37), il ne s'agit
  pas d'un artefact de conversion MySQL non stricte sur une colonne de date : c'est une valeur `NULL`
  réelle dans la colonne source `order_id` de `orderitems`, que le générateur retranscrit fidèlement
  (aucune règle de conversion ne s'applique à cette colonne — comportement volontairement inchangé,
  même politique que pour le sentinel de date : pas de devinette silencieuse sur une valeur source
  ambiguë).
- **Volume affecté dans ce fichier** : un balayage structurel du fichier `089_orderitems.sql` (reparsing
  des tuples `INSERT`, position par position, même méthode que le rapport 37) montre **2 occurrences
  sur 75 284 lignes** de `orderitems`.

### Portée du problème au-delà du fichier bloquant

Un balayage structurel identique a été étendu à l'ensemble des 147 fichiers générés : pour chaque
table, chaque colonne `NOT NULL` du schéma cible présente dans l'en-tête `INSERT` a été vérifiée pour
des valeurs `NULL` explicites sur l'ensemble des tuples générés. **2 tables sont concernées** (en plus
de `orderitems`, déjà identifiée ci-dessus) :

| Fichier | Table | Colonne(s) | Occurrences |
|---|---|---|---|
| `089_orderitems.sql` | `orderitems` | `order_id` | 2 |
| `120_scannorder_settings.sql` | `scannorder_settings` | `takeaway_auto_accept`, `delivery_auto_accept` | 8 + 8 (mêmes lignes pour les deux colonnes) |

`scannorder_settings.takeaway_auto_accept` et `delivery_auto_accept` sont toutes deux `boolean NOT
NULL DEFAULT false` dans le schéma cible (lignes 3212-3213) : 8 lignes sur 27 portent `NULL` sur ces
deux colonnes simultanément — cohérent avec une colonne ajoutée après coup côté MySQL sans backfill sur
les settings déjà existants, mais **aucune décision de conversion n'a été prise ici** (pas d'examen du
code Go demandé pour ce rapport, contrairement à la démarche du rapport 37 pour le sentinel de date).

**2 tables concernées, 18 occurrences au total** (2 dans `orderitems`, 16 réparties sur 2 colonnes dans
`scannorder_settings`). C'est un ordre de grandeur bien plus faible que le sentinel de date (244
occurrences, 7 tables) mais qui bloque le chargement complet exactement de la même façon : `orderitems`
est chargée juste après `order_comments` (86) et `order_item_configuration` (87) — la première table
de premier plan explicitement dans le périmètre applicatif de cette tâche à être atteinte
(`GetOrder` en dépend indirectement via ses lignes d'articles), et `scannorder_settings` aurait bloqué
un chargement complet même une fois `orderitems` réglée.

**Aucune décision de conversion n'a été prise** — ni sur le sens à donner à ces `NULL` table par table
et colonne par colonne (candidats naturels différents selon le cas : ligne orpheline à exclure pour
`orderitems.order_id`, faux plutôt que `NULL` pour les deux booléens de `scannorder_settings`), ni sur
une modification du générateur ou du schéma cible. Aucun fichier n'a été modifié.

## 3. Vérification limitée aux tables chargées avant l'arrêt

Comptage `SELECT count(*)` sur les 88 tables effectivement chargées, comparé au comptage attendu
(`row_counts` du rapport JSON de génération) :

**88/88 tables : comptage identique, 0 écart.** Confirmation supplémentaire : la table `orderitems`
elle-même est vide (`SELECT count(*) FROM orderitems` = 0) après l'échec — le fichier `089_...`
contient son propre `BEGIN;`/`COMMIT;` et l'erreur a interrompu la transaction avant le `COMMIT;`,
donc les ~62 500 lignes déjà insérées dans cette transaction avant le point d'échec ont bien été
annulées (rollback automatique à la fermeture de session `psql`), aucune insertion partielle n'est
restée en base.

## 4. Vérifications non exécutées (bloquées par l'arrêt du chargement)

Conformément à la consigne (« si un fichier échoue, arrête-toi... et n'enchaîne pas sur les
suivants »), les étapes suivantes **n'ont pas été exécutées** :

- Chargement des 58 fichiers restants (`089_...` à `147_...`).
- Comptage de lignes complet sur les 147 tables.
- Requêtes applicatives réelles à travers le code Go (`GetOrder`, `GetCashRegisterReport`,
  `GetUserByToken`, `ComputePOSStatus`) : non tentées. `orders` a bien fini de charger (fichier `090`
  n'a jamais été atteint, donc `orders` elle-même n'est même pas chargée) — `GetOrder` dépend à la fois
  de `orders` et `orderitems`, toutes deux hors de portée d'un chargement partiel qui n'aurait rien
  confirmé de représentatif pour cette vérification ni pour les autres (une base partiellement chargée
  ne reflète pas l'état de production visé par ces requêtes).
- Vérification de la resynchronisation des séquences identity via une insertion applicative de test sur
  `orders`.

Ces vérifications restent à faire dans une prochaine répétition, une fois le traitement des `NULL`
sur `orderitems.order_id` et sur les deux colonnes booléennes de `scannorder_settings` arbitré, de la
même manière que le sentinel de date l'a été entre les rapports 36 et 37.

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés (contenant de vraies données) ont été supprimés du dossier temporaire
à la fin de la session, ainsi que leur copie dans le conteneur Postgres de dev utilisée pour le
chargement (`/tmp/load`) et les artefacts d'analyse structurelle générés localement (scripts de comptage,
résultats de vérification). Aucun fichier de sortie contenant de vraies données n'a été conservé. Le
Postgres de dev est laissé dans l'état atteint par cette répétition (88 tables chargées, `orderitems` et
les 58 tables suivantes vides) — aucune remise à zéro supplémentaire n'a été demandée après l'arrêt.
Rien n'a été commité ; aucun fichier du dépôt n'a été modifié par cette tâche (contrairement au rapport
37, celle-ci n'a fait qu'un diagnostic, aucune règle de conversion n'a été arbitrée ni appliquée au
générateur).

## 6. Conclusion

| Étape | Résultat |
|---|---|
| Régénération des 147 fichiers | OK — 147/147, 0 échec, 0 sentinel de date résiduel |
| Reset Postgres dev + schéma cible | OK — 0 erreur, 181 tables créées |
| Chargement séquentiel | **Arrêté au fichier 89/147** (`orderitems`) — contre 20/147 au rapport 36 |
| Confirmation de la correction du rapport 37 | OK — `cash_registers` (20) et les 6 autres tables du sentinel de date chargent désormais sans erreur |
| Cause du nouveau blocage | `NULL` explicite sur une colonne `NOT NULL` du schéma cible, sans rapport avec le sentinel de date |
| Portée réelle du nouveau problème | 2 tables, 18 occurrences (`orderitems.order_id` : 2 ; `scannorder_settings.takeaway_auto_accept`/`delivery_auto_accept` : 8 chacune) |
| Tables chargées avant l'arrêt | 88/88 comptages exacts, 0 écart ; `orderitems` vide (transaction annulée proprement) |
| Vérifications applicatives Go / séquences identity | Non exécutées (bloquées par l'arrêt) |

Point bloquant pour la suite : décider, pour `orderitems.order_id` (2 lignes orphelines) et pour les
deux colonnes booléennes de `scannorder_settings` (8 lignes), comment traiter ces `NULL` sur des
colonnes `NOT NULL` du schéma cible — par exemple exclusion des lignes orphelines pour `orderitems`
(à confirmer côté métier : une ligne d'article sans commande n'a pas de sens applicatif évident) et
valeur de repli explicite pour les deux colonnes booléennes de `scannorder_settings` (candidat naturel
`false`, identique au `DEFAULT` de la colonne, mais à confirmer plutôt qu'à décider ici) — avant de
pouvoir régénérer et rejouer cette répétition jusqu'à son terme.

## 7. Implémentation des deux corrections décidées

Décision actée en dehors de ce document (sans nouvelle vérification préalable, conformément à la
consigne) : exclure les 2 lignes de `orderitems` à la génération (scope strict à cette table), et
convertir `NULL` → `FALSE` pour les 2 colonnes booléennes de `scannorder_settings` (scope strict à ces
2 colonnes). Les deux règles ont été implémentées dans
[data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py), suivant le même
principe que la règle du sentinel de date (rapport 37) : des frozensets `(table, colonne)` explicites,
pas de motif générique.

### Correction n°1 réellement nécessaire — diagnostic affiné en cours d'implémentation

En implémentant la règle d'exclusion pour `orderitems.order_id`, la vérification de non-régression a
mis en évidence que la caractérisation initiale de ce cas (section 2 : « une ligne source contient une
valeur `NULL` explicite ») était **incomplète**. Un nouveau balayage structurel du dump brut, cette
fois via le chemin d'extraction réellement utilisé par le générateur (`iter_dump_rows`, robuste aux
guillemets/apostrophes échappés dans les champs texte voisins — contrairement au script d'analyse
ad hoc jetable utilisé pour produire les comptages de la section 2, non conservé), montre que les 2
lignes concernées n'ont **aucune valeur `NULL` au sens SQL** dans la colonne source `order_id` : la
colonne contient la chaîne de texte `null` (4 caractères, entre guillemets côté source — donc une
vraie valeur, pas l'absence de valeur). Le générateur classe `order_id` comme colonne numérique
(type cible `integer`) et, pour ce genre de colonne, retranscrit la valeur source telle quelle sans la
citer entre guillemets (comportement correct pour un entier). Une chaîne source non numérique comme
`null` traverse donc ce chemin telle quelle, comme jeton SQL nu — et Postgres, dont le mot-clé `NULL`
est insensible à la casse, interprète ce jeton nu `null` comme le mot-clé `NULL` au moment du parsing
de l'`INSERT`, d'où le rejet par la contrainte `NOT NULL`. Un effet de bord, pas une valeur `NULL`
réellement présente dans la donnée source.

Ce point ne change rien à la remédiation demandée (exclure ces 2 lignes précises de `orderitems`, scope
strict à cette table) : la règle d'exclusion a été implémentée pour se déclencher sur la **valeur
formatée** produite par le générateur (celle qui serait effectivement écrite dans le fichier `.sql`),
pas sur la valeur brute du dump — ce qui couvre correctement ce cas (jeton nu non numérique
interprétable comme `NULL`) aussi bien qu'un éventuel vrai `NULL` source, sans rien changer au
traitement des lignes normales de `orderitems` ni d'aucune autre table. Point de code : `SqlTableWriter.add_row`,
comparaison insensible à la casse sur la valeur déjà formatée, restreinte aux colonnes listées dans le
nouveau frozenset `ROW_DROP_IF_NULL_COLUMNS` (une seule entrée : `("orderitems", "order_id")`).

### Correction n°2 — le cas signalé ne se reproduit pas sur la donnée source

Le même réexamen, appliqué à `scannorder_settings.takeaway_auto_accept` /
`delivery_auto_accept`, montre que **les 27 lignes de la table ont une valeur `0` ou `1` valide sur ces
deux colonnes dans le dump source — 0 occurrence de `NULL` (ni littéral, ni chaîne `null`, ni chaîne
vide)**. Le chiffre de 8 lignes par colonne cité en section 2 de ce rapport ne se reproduit pas avec le
chemin d'extraction robuste (`iter_dump_rows`) : cette table contient des colonnes texte libres
(`home_page_desc`, `info_popup_content`, ...) avec apostrophes échappées (français : « d'alcool »,
« J'ai compris », etc. — voir le `DEFAULT` de `info_popup_content` dans le schéma cible), un terrain
connu pour faire dérailler un parseur de tuples qui ne gère pas l'échappement par antislash — exactement
le cas du script d'analyse ad hoc jetable de la section 2, qui n'a jamais fait partie du dépôt et n'a
pas été conservé. Le diagnostic initial de cette table était donc un faux positif, pas une caractéristique
réelle de la donnée source.

La règle `NULL → FALSE` a néanmoins été implémentée exactement comme demandé (scope strict aux 2
colonnes de `scannorder_settings`, frozenset `NULL_TO_FALSE_COLUMNS`, appliquée dans
`format_sql_value` avant le passthrough `NULL` générique) : elle reste en place comme garde-fou
scopé et sans effet sur la donnée actuelle (elle ne se déclenche jamais sur les 27 lignes réelles),
au cas où une future extraction du même dump révélerait une ligne réellement `NULL` sur l'une des deux
colonnes.

## 8. Régénération et vérification post-correction

`generate-all-sql` relancé sur le dump réel avec les deux règles actives, dans un dossier temporaire
hors dépôt :

| Vérification demandée | Résultat |
|---|---|
| 147/147 fichiers générés | OK — `failed_tables: {}` |
| `orderitems` compte 75 282 lignes (75 284 − 2) | OK — `row_counts["orderitems"] = 75282`, confirmé par un compteur dédié (`dropped_null_key_rows_by_table: {"orderitems": 2}`) |
| 0 occurrence `NULL` restante sur `takeaway_auto_accept`/`delivery_auto_accept` | OK — 0 occurrence avant *et* après (jamais présente sur la donnée réelle, voir section 7) |
| Comptages croisés identiques ailleurs | OK — diff table par table entre la génération pré-correction et post-correction : **146/147 tables strictement identiques, seule `orderitems` diffère (75 284 → 75 282), écart exact de 2** |

Vérifications complémentaires, mêmes méthodes que les rapports 36/37 :

- **0 occurrence** du motif `0000-00-00` restante sur les 147 fichiers (règle du sentinel de date du
  rapport 37 toujours intacte) ; littéral epoch `'1970-01-01T00:00:00Z'` toujours exactement 1 fois
  dans `083_merchant_parameters.sql` et 6 fois dans `067_integration_uber_eats.sql`.
- Balayage complet et corrigé (chemin d'extraction robuste, toutes les colonnes `NOT NULL` de toutes
  les tables du schéma cible, pas seulement les 2 tables déjà connues) : **`orderitems.order_id` est
  la seule paire (table, colonne) sur l'ensemble du schéma cible où une ligne du dump réel produirait
  une valeur formatée `NULL`** — confirmation qu'aucun autre landmine du même genre (chaîne non
  numérique interprétable comme mot-clé SQL sur une colonne `NOT NULL`) ne subsiste ailleurs dans les
  147 tables.
- `dropped_source_columns_by_table` inchangé : `{"customer": ["is_migrated"], "orders": ["isDelivery"]}`.

## 9. Nettoyage

Les fichiers `.sql` régénérés pendant l'implémentation et la vérification (plusieurs passes, le temps
de corriger deux bugs dans la logique de détection avant qu'elle fonctionne comme prévu — voir section
7) ont tous été supprimés du dossier temporaire à la fin de la session, ainsi que les rapports JSON de
génération et scripts d'analyse ad hoc utilisés pour les vérifications croisées. Aucun fichier de
sortie contenant de vraies données n'a été conservé. Seul
[data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py) a été modifié
dans le dépôt (les deux règles de correction) ; ce document a été mis à jour. Rien n'a été commité. Le
Postgres de dev n'a pas été retouché par cette tâche (aucun chargement n'a été rejoué contre lui ici) —
il reste dans l'état laissé par la section 5 (88 tables chargées).

## 10. Conclusion (mise à jour)

| Étape | Résultat |
|---|---|
| Implémentation de l'exclusion `orderitems.order_id` | OK, après correction de deux bugs trouvés en vérifiant : (1) le test initial comparait la valeur brute du dump (`None`) au lieu de la valeur formatée, ne se déclenchait jamais sur ce cas ; (2) une fois corrigé pour comparer la valeur formatée, la comparaison était sensible à la casse (`"NULL"` vs le jeton source `"null"`), toujours sans effet — corrigé en comparant en majuscules |
| Diagnostic affiné de `orderitems.order_id` | La cause réelle n'est pas un `NULL` source mais une chaîne `null` non numérique sur une colonne cible entière, transcrite telle quelle puis interprétée comme mot-clé SQL par Postgres (insensible à la casse) — la remédiation demandée (exclure ces 2 lignes) reste correcte et a été implémentée sur cette base |
| Implémentation de `NULL → FALSE` sur `scannorder_settings` | OK, implémentée exactement comme demandé — mais **le cas ne se reproduit pas sur la donnée source** (0 `NULL` réel sur les 27 lignes) ; le chiffre de 8+8 du rapport initial (section 2) était un faux positif d'un script d'analyse ad hoc non conservé, qui ne gérait pas les apostrophes échappées dans les champs texte de cette table |
| Régénération post-correction | 147/147, 0 échec |
| `orderitems` | 75 282 lignes (− 2, confirmé) |
| `scannorder_settings` | 0 occurrence `NULL` avant et après (rien à convertir sur la donnée actuelle) |
| Autres tables | 146/146 comptages strictement identiques à la génération pré-correction |
| Balayage complet des colonnes `NOT NULL` (147 tables) | 0 paire (table, colonne) restante produisant une valeur `NULL` sur la donnée réelle |

Point notable pour la suite : la classe de bug révélée ici (une chaîne source non numérique passe telle
quelle, non guillemetée, sur une colonne cible numérique, et peut accidentellement matcher un mot-clé
SQL) est plus large que le seul cas `orderitems.order_id` traité ici — mais, conformément au scope
strict demandé pour cette tâche, aucune correction générique n'a été appliquée au formatage des colonnes
numériques : seule la paire `(orderitems, order_id)` a été corrigée. Un futur audit dédié (sur le modèle
du rapport 37 pour le sentinel de date) resterait à faire si l'on veut couvrir ce risque plus largement
plutôt qu'au cas par cas.

# 37 - Cartographie et traitement du sentinel MySQL 0000-00-00 (structurel uniquement, aucune donnée réelle)

Date: 2026-07-21 (audit initial) ; mise à jour 2026-07-21 (règle de conversion appliquée)
Branche: migration/postgres

## Objectif

Le rapport [36-full-data-load-rehearsal.md](36-full-data-load-rehearsal.md) a identifié 7 tables
contenant le sentinel MySQL de date invalide (`0000-00-00` ou `0000-00-00 00:00:00`) sur un total
de 244 occurrences, sans préciser les colonnes exactes ni leur nullabilité. Les sections 1 à 4
couvrent ce diagnostic initial, colonne par colonne, **sans décision de conversion** (aucun fichier
modifié à ce stade). La section 5 documente la mise à jour ultérieure : une règle de conversion a
depuis été arbitrée et appliquée dans le générateur (`data-migration/transform_mysql_csv.py`),
scopée aux 8 colonnes précises listées ci-dessous.

## Méthode

Les 147 fichiers `.sql` ont été régénérés (même commande que le rapport 36, mêmes 147/147 tables,
0 échec) dans un dossier temporaire hors dépôt, le temps de l'analyse, puis supprimés (section 5).
Pour chacun des 7 fichiers concernés, un script Python jetable (non conservé) a reparsé
structurellement chaque `INSERT` généré — liste de colonnes de l'en-tête + tuples de valeurs,
respectant les chaînes citées (`''` échappé) et les parenthèses imbriquées — pour déterminer,
position par position, quelle colonne porte la valeur sentinel sur chaque ligne concernée. Le total
par table obtenu recoupe exactement le comptage brut du rapport 36 (244 occurrences, table par
table) — aucun écart, ce qui valide la méthode.

## 1. Tableau récapitulatif

| Table | Colonne | Type cible | Nullable | Occurrences |
|---|---|---|---|---|
| `cash_registers` | `end_date` | `timestamptz` | **NULL autorisé** | 1 |
| `customer` | `customer_birthdate` | `date` | **NULL autorisé** | 22 |
| `integration_uber_eats` | `pos_provisionning_token_expiration_date` | `timestamptz` | **NOT NULL** | 6 |
| `merchant_marketing_settings` | `created_at` | `timestamptz` (`DEFAULT now()`) | **NULL autorisé** | 3 |
| `merchant_marketing_settings` | `updated_at` | `timestamptz` (`DEFAULT now()`) | **NULL autorisé** | 3 |
| `merchant_parameters` | `last_menu_update` | `timestamptz` | **NOT NULL** | 1 |
| `orders` | `estimated_ready` | `timestamptz` | **NULL autorisé** | 204 |
| `users` | `dob` | `date` | **NULL autorisé** | 4 |

**Total : 244 occurrences, 8 lignes de colonnes sur 7 tables** — identique au total du rapport 36.

Chaque déclaration a été vérifiée directement dans
[04-schema-postgres-target.sql](04-schema-postgres-target.sql) (`cash_registers.end_date` ligne 507,
`customer.customer_birthdate` ligne 841, `integration_uber_eats.pos_provisionning_token_expiration_date`
ligne 1675, `merchant_marketing_settings.created_at`/`updated_at` lignes 2059-2060,
`merchant_parameters.last_menu_update` ligne 2077, `orders.estimated_ready` ligne 2304,
`users.dob` ligne 3722).

**6 des 8 colonnes concernées sont nullables** dans le schéma cible (dont les deux de
`merchant_marketing_settings`, qui ont en plus un `DEFAULT now()` — un `NULL` explicite les
laisserait quand même sans valeur, le défaut ne s'appliquant qu'à une colonne absente de
l'`INSERT`, pas à une valeur `NULL` explicite). Seules **2 colonnes sont `NOT NULL`** :
`integration_uber_eats.pos_provisionning_token_expiration_date` et
`merchant_parameters.last_menu_update` — une conversion vers `NULL` les bloquerait telle quelle.

## 2. `orders.estimated_ready` — décomposition (204 occurrences)

Une seule colonne est concernée sur `orders` : **`estimated_ready`**, à 100% des 204 occurrences.
Aucune autre colonne de la table (dates ou non) ne porte le sentinel — pas de répartition à faire
entre plusieurs colonnes.

## 3. Constat côté code Go pour les 2 colonnes `NOT NULL`

Signalé ici à titre de constat, **sans décision de conversion prise**.

### `merchant_parameters.last_menu_update`

Trouvé dans [internal/modules/pos/create_repository.go](../../internal/modules/pos/create_repository.go)
(lignes 87-91), au moment de l'initialisation d'un nouveau merchant :

```go
// merchant_parameters (PK = merchant_id) — last_menu_update est NOT NULL
// sans défaut (MySQL non-strict insérait le zéro-date) : horodatage
// explicite pour la validité cross-dialecte.
if _, err := db.ExecContext(ctx,
    `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES (?, `+dbx.UTCNow()+`)`, merchantID,
); err != nil { ... }
```

Ce commentaire, déjà présent dans le code avant cette tâche (travail antérieur sur cette branche de
migration), confirme explicitement l'origine du sentinel : **MySQL en mode non strict insérait
`0000-00-00 00:00:00` quand la colonne `NOT NULL` sans défaut ne recevait pas de valeur explicite**
— exactement le mécanisme observé dans la donnée réelle exportée. Le code applicatif, lui, ne laisse
jamais cette colonne vide : il y insère systématiquement un horodatage réel
(`dbx.UTCNow()`) à la création. Elle est aussi lue en lecture normale dans
[internal/modules/menu/repository.go](../../internal/modules/menu/repository.go) (ligne ~707,
`SELECT last_menu_update FROM merchant_parameters ...`) comme un vrai "dernier horodatage de mise à
jour du menu", exposé aux clients. **Constat : aucun chemin applicatif vivant ne produit
intentionnellement cette valeur — le sentinel présent dans l'export a toutes les caractéristiques
d'une anomalie de donnée héritée du comportement MySQL non strict, pas d'un cas métier légitime.**

### `integration_uber_eats.pos_provisionning_token_expiration_date`

Recherche exhaustive (grep insensible à la casse, tout le dépôt) : **aucune occurrence en dehors de
deux fichiers de test d'intégration** (`internal/modules/ubereats/postgres_integration_test.go`,
`internal/modules/integrations/postgres_integration_test.go`), où la colonne n'apparaît que comme
donnée de seed (`INSERT ... VALUES (...)`), jamais lue ni comparée à `now()` dans une décision
métier. Aucun site en dehors de ces tests ne lit, n'écrit, ni ne référence cette colonne — même
statut structurel que `orders.isDelivery`/`customer.is_migrated` dans
[35-dead-columns-removal.md](35-dead-columns-removal.md) (colonne "morte" côté logique Go).
**Constat : contrairement à `last_menu_update`, il n'existe aucun chemin applicatif vivant
permettant de déduire ce que devrait être une valeur par défaut sensée — l'absence totale d'usage
rend la question "quelle valeur mettre à la place du sentinel" indissociable de la question plus
large "cette colonne a-t-elle encore un rôle".**

## 4. Conclusion

| Colonne | Nullable | Sentinel = anomalie confirmée par le code | Chemin de conversion évident |
|---|---|---|---|
| `cash_registers.end_date` | oui | — (pas d'examen Go demandé, colonne nullable) | `NULL` plausible, à confirmer |
| `customer.customer_birthdate` | oui | — | `NULL` plausible, à confirmer |
| `integration_uber_eats.pos_provisionning_token_expiration_date` | **non** | colonne morte, aucun usage Go vivant | aucun — dépend d'abord d'une décision sur l'utilité de la colonne |
| `merchant_marketing_settings.created_at` | oui (+ `DEFAULT now()`) | — | `NULL` plausible, à confirmer |
| `merchant_marketing_settings.updated_at` | oui (+ `DEFAULT now()`) | — | `NULL` plausible, à confirmer |
| `merchant_parameters.last_menu_update` | **non** | oui — commentaire de code confirmant l'origine MySQL non stricte, colonne activement lue/écrite | pas de `NULL` possible (`NOT NULL`) ; nécessite une valeur de repli explicite, à arbitrer |
| `orders.estimated_ready` | oui | — | `NULL` plausible, à confirmer |
| `users.dob` | oui | — | `NULL` plausible, à confirmer |

Cette cartographie a servi de base à la règle de conversion arbitrée et appliquée ci-dessous
(section 5).

## 5. Règle de conversion appliquée dans le générateur

**Décision arbitrée** (en dehors de ce document) : conversion scopée exactement aux 8 colonnes de
la section 1, différenciée selon la nullabilité déjà établie —

- **Colonne nullable** (6 colonnes : `cash_registers.end_date`, `customer.customer_birthdate`,
  `merchant_marketing_settings.created_at`, `merchant_marketing_settings.updated_at`,
  `orders.estimated_ready`, `users.dob`) : le sentinel devient `NULL`, le mot-clé SQL natif (jamais
  une chaîne citée) — cohérent avec le reste du générateur, où `NULL` est déjà préservé nativement
  de bout en bout (rapport 33).
- **Colonne `NOT NULL`** (2 colonnes : `merchant_parameters.last_menu_update`,
  `integration_uber_eats.pos_provisionning_token_expiration_date`) : le sentinel devient le
  littéral `'1970-01-01T00:00:00Z'` (epoch UTC), puisque `NULL` n'est pas une option pour ces
  colonnes.

Aucune autre colonne n'est concernée : la règle est indexée par la paire exacte
`(table, colonne)`, pas par un motif de valeur global — un `0000-00-00`/`0000-00-00 00:00:00` sur
une colonne non auditée (hors des 8 listées) n'est **pas** touché et continue de provoquer l'erreur
Postgres bloquante déjà observée au rapport 36 si elle existe ailleurs (aucune occurrence de ce
type n'a été trouvée en dehors des 7 tables auditées — voir section 6 ci-dessous).

### Point de code exact

Fichier [data-migration/transform_mysql_csv.py](../../data-migration/transform_mysql_csv.py) :

- **Constantes** (lignes 34-60, juste après `SENTINEL_IDENTITY_RULES`) :
  - `ZERO_DATE_SENTINELS` (ligne 41) : les deux formes littérales du sentinel source
    (`"0000-00-00"`, `"0000-00-00 00:00:00"`).
  - `ZERO_DATE_TO_NULL_COLUMNS` (lignes 44-52) : le frozenset des 6 paires `(table, colonne)`
    nullables.
  - `ZERO_DATE_TO_EPOCH_COLUMNS` (lignes 55-58) : le frozenset des 2 paires `(table, colonne)`
    `NOT NULL`.
  - `ZERO_DATE_EPOCH_LITERAL` (ligne 60) : `"1970-01-01T00:00:00Z"`.
- **`SqlFieldRules`** (dataclass, vers ligne 296) : deux nouveaux champs précalculés par table,
  `zero_date_to_null_fields` et `zero_date_to_epoch_fields`, dérivés des deux frozensets ci-dessus
  filtrés sur `table_info.name` dans `SqlFieldRules.from_table_info` — donc calculés une fois par
  table, pas par ligne.
- **Application effective** : dans `format_sql_value(field_name, value, rules)` (vers ligne 356),
  juste après le test `if value is None: return "NULL"` et **avant** toute autre règle
  (booléen/merchant_id/numeric/texte) — pour qu'aucune règle générique ne puisse masquer ce cas
  particulier. Logique : `if value.strip() in ZERO_DATE_SENTINELS:` puis bascule sur
  `zero_date_to_null_fields` (→ `"NULL"`) ou `zero_date_to_epoch_fields` (→ le littéral epoch
  cité). Si la colonne courante n'est dans aucun des deux ensembles, la valeur retombe sur le
  chemin de formatage habituel (donc une colonne non auditée portant ce même texte serait citée
  comme une chaîne SQL ordinaire `'0000-00-00...'`, provoquant l'erreur Postgres au chargement —
  comportement volontairement inchangé, cf. rapport 36).

Ce chemin de code s'applique uniquement à `generate-all-sql`/`generate-sql` (le pipeline SQL direct
utilisé pour cette tâche) : `format_sql_value` n'est pas utilisé par la commande `transform` (le
pipeline CSV historique, hors périmètre de cette tâche).

## 6. Vérification après régénération

Les 147 fichiers ont été régénérés avec le générateur modifié (`generate-all-sql` sur le fichier
réel `data-migration/migration_welloresto_data.sql`), dans un dossier temporaire hors dépôt,
supprimé après vérification (section 7).

1. **147/147 tables générées, 0 échec** (`failed_tables: {}` dans le rapport JSON de génération) —
   identique aux rapports 33/35/36.
2. **Balayage du motif `0000-00-00` sur les 147 fichiers de sortie : 0 occurrence restante.**
   Confirmation ciblée : le littéral epoch `'1970-01-01T00:00:00Z'` apparaît exactement 1 fois dans
   `083_merchant_parameters.sql` et exactement 6 fois dans `067_integration_uber_eats.sql` — les
   comptages exacts attendus pour les 2 colonnes `NOT NULL` (section 1) — et **0 fois** dans les
   145 autres fichiers.
3. **Comptages de lignes** : comparaison entre le `row_counts` du rapport de génération et un
   comptage indépendant (`inspect-dump`, second passage sur le dump brut, sans lien avec le code de
   génération) sur les 147 tables : **0 écart**, 472 776 lignes au total des deux côtés — même
   méthode que les rapports 33/35/36.
4. Effet de bord : `dropped_source_columns_by_table` du rapport de génération reste identique à
   avant ce changement — `{"customer": ["is_migrated"], "orders": ["isDelivery"]}` — confirmant que
   la modification n'a affecté que le traitement du sentinel de date, rien d'autre.

## 7. Nettoyage

Les 147 fichiers `.sql` régénérés (audit initial, puis à nouveau après la modification du
générateur — contenant de vraies données dans les deux cas) ont été supprimés du dossier temporaire
à la fin de chaque session, conformément à la pratique déjà en place sur ce chantier. Aucun fichier
de sortie contenant de vraies données n'a été conservé. Le script Python jetable utilisé pour le
parsing structurel de l'audit initial n'a pas été conservé dans le dépôt. Rien n'a été commité ;
seul `data-migration/transform_mysql_csv.py` a été modifié dans le dépôt (la règle de conversion),
et ce document mis à jour.

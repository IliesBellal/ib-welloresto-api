# 36 - Répétition générale de chargement complet (structurel uniquement, aucune donnée réelle)

Date: 2026-07-21
Branche: migration/postgres

## Objectif

Charger en conditions réelles les fichiers `.sql` générés par `generate-all-sql`
(voir [33-sql-output-generation.md](33-sql-output-generation.md) et
[35-dead-columns-removal.md](35-dead-columns-removal.md)) contre le Postgres Docker de dev
(`localhost:5433`), avec vérification post-chargement (comptages de lignes, requêtes
applicatives réelles via le code Go, resynchronisation des séquences identity).

**Résultat : le chargement s'est arrêté au fichier 20/147 sur une erreur Postgres réelle et
bloquante.** Conformément à la consigne, l'exécution n'a pas continué au-delà de ce point — les
vérifications post-chargement (comptages complets, requêtes applicatives par tier, séquences
identity) n'ont donc **pas** été exécutées, à l'exception d'une vérification de comptage limitée
aux tables effectivement chargées avant l'arrêt (section 3).

Aucune valeur de donnée réelle n'est citée dans ce document — uniquement noms de tables, de
colonnes, de fichiers, comptages et messages d'erreur Postgres génériques.

## 0. Écart constaté avant exécution (résolu, voir historique de session)

L'énoncé initial de cette tâche référençait 147 fichiers déjà générés. En pratique, aucun fichier
`.sql` n'était présent sur disque : la génération précédente ([35-dead-columns-removal.md](35-dead-columns-removal.md)
§3.3) avait écrit sa sortie dans un dossier temporaire supprimé immédiatement après vérification,
conformément à la pratique de ce chantier (ne jamais garder de données réelles sur disque plus
longtemps que nécessaire). Les 147 fichiers ont donc été régénérés en tout début de cette session
(même commande, même dump source), dans un dossier temporaire hors du dépôt et hors de tout
dossier synchronisé. Résultat de cette régénération : **147/147 tables générées, 0 échec**,
identique au résultat du rapport 35.

## 1. Remise à zéro du Postgres de dev

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Conteneur `welloresto-postgres-dev` recréé avec un volume vide, prêt (`pg_isready`) en moins de 2
secondes. Schéma cible [04-schema-postgres-target.sql](04-schema-postgres-target.sql) chargé via
`psql -v ON_ERROR_STOP=1` : **0 erreur**, 181 tables de base créées (`information_schema.tables`,
`table_type = 'BASE TABLE'`).

## 2. Chargement séquentiel des 147 fichiers

Chargement un par un, dans l'ordre numérique (`001_...` à `147_...`, ordre de tri topologique déjà
calculé à la génération), via `psql -v ON_ERROR_STOP=1` dans une boucle shell, chaque fichier dans
sa propre session `psql` (chaque fichier contient déjà son propre `BEGIN;` / `COMMIT;`).

**19 fichiers chargés avec succès** (`001_allergens.sql` à `019_cash_desks.sql`), puis **arrêt** au
20ᵉ fichier :

```
FAILED: 020_cash_registers.sql
ERROR:  date/time field value out of range: "0000-00-00 00:00:00"
```

### Diagnostic (structurel, sans valeur de donnée)

- **Table concernée** : `cash_registers`.
- **Colonne concernée** : `end_date` (`timestamptz`, nullable dans le schéma cible — voir
  [04-schema-postgres-target.sql](04-schema-postgres-target.sql) ligne 507).
- **Cause** : la colonne source MySQL contient, sur une ligne, le sentinel MySQL classique de date
  invalide `0000-00-00 00:00:00` (comportement historique de MySQL en mode non strict : une colonne
  `datetime` "vide" est stockée comme cette date zéro plutôt que comme `NULL`). Postgres n'a pas
  d'équivalent : `timestamptz` refuse toute valeur qui ne correspond pas à un instant calendaire
  réel, et lève une erreur bloquante au lieu de convertir silencieusement.
- **Volume affecté dans ce fichier** : 1 occurrence sur ~500+ lignes de `cash_registers` (une seule
  ligne concernée).
- Le générateur ([33-sql-output-generation.md](33-sql-output-generation.md)) ne fait aucune
  conversion implicite sur ce genre de valeur hors-domaine — même politique que pour les colonnes
  booléennes hors 0/1 traitées dans [35-dead-columns-removal.md](35-dead-columns-removal.md) : pas
  de devinette silencieuse sur une valeur source ambiguë, la question est remontée pour arbitrage
  humain plutôt que résolue par une conversion automatique (par exemple vers `NULL`, qui serait
  l'option la plus naturelle vu que la colonne est nullable, mais reste une interprétation et non
  une simple retranscription).

### Portée du problème au-delà du fichier bloquant

Un balayage structurel (comptage d'occurrences du motif `0000-00-00`, sans lecture des lignes
concernées) sur l'ensemble des 147 fichiers générés montre que ce n'est **pas un cas isolé** :

| Fichier | Table | Occurrences |
|---|---|---|
| `020_cash_registers.sql` | `cash_registers` | 1 |
| `031_customer.sql` | `customer` | 22 |
| `067_integration_uber_eats.sql` | `integration_uber_eats` | 6 |
| `082_merchant_marketing_settings.sql` | `merchant_marketing_settings` | 6 |
| `083_merchant_parameters.sql` | `merchant_parameters` | 1 |
| `090_orders.sql` | `orders` | 204 |
| `143_users.sql` | `users` | 4 |

**7 tables concernées, 244 occurrences au total** — dont 204 dans `orders`, une table de premier
plan explicitement citée dans le périmètre de vérification applicative de cette tâche
(`GetOrder`). Ce n'est donc pas un simple accroc marginal : sans décision sur le traitement de ce
sentinel (candidat naturel : conversion en `NULL` là où la colonne cible est nullable, à confirmer
colonne par colonne — certaines des colonnes concernées pourraient être `NOT NULL` dans le schéma
cible, ce qui bloquerait même après conversion en `NULL`), le chargement complet ne peut pas aller
à son terme sur au moins ces 7 tables.

**Aucune décision de conversion n'a été prise ici** — ni sur le sens à donner à ce sentinel table
par table, ni sur une modification du générateur ou du schéma cible. Aucun fichier n'a été modifié.

## 3. Vérification limitée aux tables chargées avant l'arrêt

Comptage `SELECT count(*)` sur les 19 tables effectivement chargées, comparé au comptage attendu
(`row_counts` du rapport JSON de génération) :

**19/19 tables : comptage identique, 0 écart.** Le chargement séquentiel lui-même — hors du
sentinel de date — fonctionne comme attendu sur toutes les tables testées avant le point d'arrêt.

## 4. Vérifications non exécutées (bloquées par l'arrêt du chargement)

Conformément à la consigne (« si un fichier échoue, arrête-toi... et n'enchaîne pas sur les
suivants »), les étapes suivantes **n'ont pas été exécutées** :

- Chargement des 127 fichiers restants (`021_...` à `147_...`).
- Comptage de lignes complet sur les 147 tables.
- Requêtes applicatives réelles à travers le code Go (`GetOrder`, `GetCashRegisterReport`,
  `GetUserByToken`, `GetPOSStatus`/`ComputePOSStatus`) : non tentées. `cash_registers` (bloquée) et
  `orders` (qui aurait elle-même échoué, 204 occurrences du même sentinel) sont toutes deux dans le
  périmètre direct de ces vérifications — les lancer contre un chargement partiel n'aurait rien
  confirmé de représentatif.
- Vérification de la resynchronisation des séquences identity via une insertion applicative de
  test sur `orders`.

Ces vérifications restent à faire dans une prochaine répétition, une fois le traitement du sentinel
`0000-00-00 00:00:00` arbitré sur les 7 tables concernées.

## 5. Conclusion

| Étape | Résultat |
|---|---|
| Régénération des 147 fichiers | OK — 147/147, 0 échec |
| Reset Postgres dev + schéma cible | OK — 0 erreur, 181 tables créées |
| Chargement séquentiel | **Arrêté au fichier 20/147** (`cash_registers`) |
| Cause | Sentinel MySQL `0000-00-00 00:00:00` dans une colonne `timestamptz`, sans conversion prévue par le générateur |
| Portée réelle du problème | 7 tables, 244 occurrences, dont 204 dans `orders` |
| Tables chargées avant l'arrêt | 19/19 comptages exacts, 0 écart |
| Vérifications applicatives Go / séquences identity | Non exécutées (bloquées par l'arrêt) |

Point bloquant pour la suite : décider, table par table et colonne par colonne parmi les 7
identifiées, comment traiter le sentinel `0000-00-00 00:00:00` (conversion en `NULL` si la colonne
cible est nullable ; sinon, une autre décision est nécessaire) avant de pouvoir régénérer et
rejouer cette répétition jusqu'au bout.

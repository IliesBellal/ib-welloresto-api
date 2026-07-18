# 23 — Traduction de la procédure stockée GET_AVERAGE_DISTRIBUTION_TIME

Remplacement de l'appel `CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)` par la requête SQL
équivalente exécutée directement depuis Go, portable MySQL/Postgres via `dbx`
([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)). Fait partie du
chantier « procédures stockées » préalable listé dans
[07-module-inventory.md](07-module-inventory.md) §« règle transverse » (voir aussi
[24-pos-status-translation.md](24-pos-status-translation.md) pour `GET_POS_STATUS`).

## Sites d'appel remplacés (3)

| Fichier | Fonction | Usage du résultat |
|---|---|---|
| [internal/modules/orders/repository.go](../../internal/modules/orders/repository.go) | `GetEstimatedDistributionTime` | secondes brutes retournées au service pricing |
| [internal/modules/order_life_cycle/repository.go](../../internal/modules/order_life_cycle/repository.go) | `ComputeEstimatedReady` | `now UTC + secondes` formaté `2006-01-02 15:04:05` (erreurs avalées, comme avant) |
| [internal/modules/ubereats/repository.go](../../internal/modules/ubereats/repository.go) | `CalculateAutoPrepTime` | `int((secondes/60)*0.7)` (héritage PHP) |

Les trois passent désormais par une fonction partagée unique :
[internal/modules/distributiontime/estimate.go](../../internal/modules/distributiontime/estimate.go)

```go
func EstimatedSeconds(ctx, database *sql.DB, merchantID string, nbProductsCurrentOrder int) (seconds int, found bool, err error)
```

`found=false` reproduit le cas « la procédure ne renvoie aucune ligne » (merchant sans
`average_distribution_time` ou `merchant_parameters`) — chaque appelant garde son
comportement historique (0 s, estimated_ready vide, prep time 0).

La fonction utilise `dbx.GetDB` : rebind `?`→`$N` en Postgres, résolution transaction/connexion
depuis le contexte, aucun changement de comportement en MySQL.

## Traduction du corps de la procédure

Formule (inchangée) :

```
ROUND(LEAST(GREATEST((pending + nb_produits) * LEAST(adt, 180) / capacity, min_prep), max_prep))
```

où `pending` = `SUM(quantity - distributed_quantity)` des orderitems non distribués des
commandes `OPEN` du merchant (non planifiées, ou planifiées dont `estimated_ready` tombe dans
les 90 prochaines minutes).

### Écarts voulus vis-à-vis du SQL original

| Original (MySQL) | Traduit | Pourquoi |
|---|---|---|
| `IFNULL(x, y)` | `COALESCE(x, y)` | Portable, même sémantique. |
| `DATE_ADD(UTC_TIMESTAMP, INTERVAL 90 MINUTE)` | `%s + INTERVAL '90' MINUTE` avec `dbx.UTCNow()` | Voir ci-dessous. |
| `o.scheduled = 0` / `= 1` | `= FALSE` / `= TRUE` | `scheduled` est `boolean` en cible Postgres ; les littéraux TRUE/FALSE sont aussi valides sur TINYINT(1) MySQL (pattern déjà établi en [14-tier1-conversion-log.md](14-tier1-conversion-log.md) §3). |
| numérateur nu | `CAST(numérateur AS DECIMAL(20,4))` | En Postgres `int/int` est une **division entière** (tronquée) alors que MySQL produit un décimal — sans le CAST, Postgres perdrait la fraction avant le ROUND. `DECIMAL` est accepté par les deux dialectes (alias de `numeric` en PG). |
| `/ mp.concurrent_preparation_capacity` | `/ NULLIF(mp.concurrent_preparation_capacity, 0)` | MySQL retourne `NULL` sur division par zéro (rattrapé par le `IFNULL` de la procédure) ; Postgres lèverait `division_by_zero`. `NULLIF` reproduit le comportement MySQL partout. |
| `GROUP BY adt.merchant_id, mp.minimum_preparation_time` | `GROUP BY adt.merchant_id, mp.merchant_id` | L'original laissait `mp.maximum_preparation_time`, `mp.concurrent_preparation_capacity` et `adt.distribution_time` hors GROUP BY sans dépendance fonctionnelle — rejeté par Postgres **et** par MySQL 8 en `ONLY_FULL_GROUP_BY` (mode par défaut ; la prod Hostinger tourne sans, d'où l'absence d'erreur historique). Grouper par les deux **clés primaires** rend toutes les colonnes sélectionnées fonctionnellement dépendantes dans les deux dialectes, sans changer le résultat (au plus une ligne par merchant). |

### Choix pour le « maintenant + 90 minutes »

Deux options étaient possibles : fragment SQL branché par dialecte, ou `time.Time` calculé
côté Go et passé en paramètre. **Choix : fragment SQL, via le `dbx.UTCNow()` existant** :

```sql
... AND (o.scheduled = FALSE OR (o.scheduled = TRUE AND %s + INTERVAL '90' MINUTE >= o.estimated_ready))
```

- La syntaxe `INTERVAL '90' MINUTE` (quantité en chaîne, unité hors chaîne — forme SQL
  standard) est acceptée **telle quelle par MySQL et Postgres** : seul le « maintenant »
  diffère (`UTC_TIMESTAMP()` vs `now()`), et ce point de branchement existe déjà
  (`dbx.UTCNow()`, utilisé partout depuis le Tier 1). Un seul `fmt.Sprintf`, pas de nouvelle
  branche de dialecte à maintenir.
- L'horloge reste **celle de la base**, comme dans la procédure et le reste du repo — pas de
  dérive si l'horloge du serveur applicatif diverge.
- Un `time.Time` en paramètre aurait dépendu de l'encodage du driver : `go-sql-driver/mysql`
  sérialise les `time.Time` selon le paramètre `loc` du DSN (hors de notre contrôle,
  `MYSQL_URL` vient de l'env), alors que le fragment SQL est insensible à la config du driver.

## Vérification réelle

### Postgres (Docker dev, `localhost:5433`)

Test d'intégration [postgres_integration_test.go](../../internal/modules/distributiontime/postgres_integration_test.go)
(tag `postgres_integration`, helper `pgtest`), fixtures insérées puis nettoyées :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/modules/distributiontime/...
```

| Cas | Fixtures | Attendu à la main | Résultat |
|---|---|---|---|
| Nominal | adt=200 (→180), capacity=2, bornes [300, 3600] ; OPEN non planifiée 5-1=4 pending + item déjà distribué exclu ; planifiée à +30 min incluse (+2) ; planifiée à +5 h exclue ; commande DONE exclue ; nb=3 | (4+2+3)×180/2 = **810** | ✅ 810 |
| Borne basse | adt=120, capacity=1, aucune commande, nb=2 | 240 → clampé à min **300** | ✅ 300 |
| Borne haute | adt=3000 (→180), min=60, max=600, nb=10 | 1800 → clampé à max **600** | ✅ 600 |
| capacity=0 | adt=180, min=300, nb=5 | division NULL → min **300** (pas de `division_by_zero` grâce au NULLIF) | ✅ 300 |
| Merchant inconnu | — | aucune ligne → `found=false` | ✅ (0, false) |

**PASS** (`TestEstimatedSeconds_Postgres`, 0.11 s).

### MySQL

- `go build ./...` : OK.
- `go test ./internal/...` : liste d'échecs **strictement identique** à la baseline
  pré-existante du rapport 14 (auth, bookingcomm, planning/{employees,leave,swaps},
  pos/accounting, ubereats — ces deux derniers étant des erreurs `go vet` pré-existantes dans
  des fichiers non touchés par ce chantier). Aucune régression.
- **Validation syntaxique et sémantique réelle** : la requête rendue en dialecte MySQL
  (`UTC_TIMESTAMP()`, placeholders `?`) a été rejouée contre un conteneur MySQL 8 jetable
  (`sql_mode` par défaut, incluant `ONLY_FULL_GROUP_BY` et `ERROR_FOR_DIVISION_BY_ZERO`) avec
  les **mêmes fixtures** que le test Postgres : résultats identiques — 810 / 300 / 600 / 300 /
  aucune ligne. La division par zéro en SELECT retourne bien NULL en MySQL (le
  `ERROR_FOR_DIVISION_BY_ZERO` ne s'applique qu'aux écritures), rattrapé par le COALESCE.

## Conséquences pour la suite

- La procédure MySQL `GET_AVERAGE_DISTRIBUTION_TIME` n'est plus appelée par l'API — elle peut
  être supprimée de la prod une fois cette branche déployée (`DROP PROCEDURE`), et n'a **pas**
  besoin d'être recréée en fonction Postgres.
- Le doute levé en [03-table-usage-audit.md](03-table-usage-audit.md) : le corps de la
  procédure ne référence **ni** `average_distribution_time_by_category` **ni**
  `average_distribution_time_history` — ces tables restent orphelines côté API.
- `GET_POS_STATUS` est traduite en parallèle
  ([24-pos-status-translation.md](24-pos-status-translation.md)) ; restent
  `GET_CASH_REGISTER_REPORT` et `GET_CASH_REGISTER_REPORT_MOP` (cash_registers).
- Attention : les modules appelants (`orders`, `order_life_cycle`, `ubereats`) ne sont pas
  encore convertis à `dbx` dans leur ensemble — seule la partie ex-procédure est portable.
  Le reste de leurs requêtes sera traité lors de leur conversion Tier 3/4.

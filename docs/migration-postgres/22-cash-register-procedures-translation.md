# 22 — Traduction SQL inline de `GET_CASH_REGISTER_REPORT` / `GET_CASH_REGISTER_REPORT_MOP`

Date : 2026-07-18

Les deux procédures stockées MySQL (jamais versionnées dans ce repo — corps fourni depuis la base,
confirmant l'hypothèse du [rapport 19 §6](19-cash-register-id-audit.md#6-get_cash_register_report--get_cash_register_report_mop--angle-mort-ou-hypothèse-implicite-))
sont désormais traduites en **SQL inline dual-dialecte** exécuté directement en Go via `dbx.GetDB`
dans [cash_registers/repository.go](../../internal/modules/cash_registers/repository.go) :

- constantes `cashRegisterReportSQL` et `cashRegisterReportMOPSQL` ;
- helpers `queryCashRegisterReportLines(ctx, cashRegisterID)` et
  `queryCashRegisterReportMOP(ctx, cashRegisterID)` ;
- les deux sites d'appel (`GetCashRegisterReport` et `GetCashRegisterTVADetails`) utilisent ces
  helpers ; plus aucun `CALL` dans le repo (grep `GET_CASH_REGISTER_REPORT` : uniquement des
  commentaires/docs). **Une fois déployé, les deux procédures peuvent être décommissionnées en
  base MySQL** — elles ne sont plus appelées, même sous `DB_DIALECT=mysql`.

## 1. Transformations appliquées (procédure REPORT)

| MySQL (corps de la procédure) | Traduction | Pourquoi |
|---|---|---|
| `IFNULL(x, 0)` | `COALESCE(x, 0)` | Standard, valide dans les deux dialectes |
| `ROUND(x, 0)` sur un flottant | `ROUND(CAST(x AS DECIMAL(20,6)), 0)` | PG n'a pas de `round(double precision, integer)` — uniquement `round(numeric, integer)`. `DECIMAL` est accepté par les deux dialectes |
| Alias `as 'HT'` (quote simple) | `AS ht` (non quoté) | L'alias entre quotes simples est du MySQL non standard ; le scan Go est positionnel, l'alias est cosmétique |
| Sous-requête agrégée : `GROUP BY tva.tva_title, tva.delivery_type` en sélectionnant `tva_id` | `GROUP BY tva.tva_id` (PK), `tva_title` retiré du SELECT interne (l'externe lit `all_tva.tva_title`) | PG rejette une colonne non agrégée hors GROUP BY. Grouper par le PK est déterministe ; l'original fusionnait les catégories partageant (titre, delivery_type) avec un `tva_id` arbitraire (non-déterminisme MySQL). Équivalent tant que les libellés ne sont pas dupliqués ; sinon la nouvelle forme ventile par catégorie au lieu de fusionner (somme globale identique) |
| Branche fees : agrégats `SUM(...)` sans `GROUP BY` avec colonnes nues | `GROUP BY all_tva.tva_id, all_tva.delivery_type, all_tva.tva_title, l.label` ajouté | Même contrainte PG ; une seule ligne `tva_id = -1` en pratique, résultat identique |
| `WHERE all_tva.tva_id = '-1'` | `= -1` (littéral numérique) | `tva_id` est un `integer` en PG — pas de coercition implicite varchar↔int fiable |
| `INNER JOIN merchant m ON m.id = o.merchant_id` | **Jointure supprimée** (les 2 branches + MOP) | `merchant.id` est resté `integer` face à `orders.merchant_id varchar(64)` ([rapport 13](13-merchant-id-schema-update.md)) → jointure intraduisible sans cast. Elle était purement restrictive (aucune colonne projetée) et `orders.merchant_id` référence toujours un merchant existant — aucun effet sur le résultat |
| `show_in_report is true` | conservé (`IS TRUE`) | Valide dans les deux dialectes |
| `cashRegisterID` (paramètre de la procédure) | placeholder `?` **string**, passé 2× (une fois par branche de l'`UNION`) | Comparé à `orders.cash_register_id` qui est `varchar` dans les deux dialectes (colonne hybride id/sentinelles, rapports 16/19) — le passer en int casserait `varchar = int` en PG. Toujours numérique côté appelant (audit 19 §6) |
| `UNION` | conservé tel quel | Syntaxe et sémantique (dédoublonnage) identiques |

## 2. Transformations appliquées (procédure REPORT MOP)

| MySQL | Traduction | Pourquoi |
|---|---|---|
| `SUM(ROUND(p.amount, 2))` | `CAST(SUM(ROUND(p.amount, 2)) AS DECIMAL(20,0))` | En PG, `round(numeric, 2)` produit un numeric à échelle 2 (`"1500.00"`) que `database/sql` ne sait pas scanner dans un `int` Go. `p.amount` étant un entier (centimes), le recast à l'échelle 0 est exact. Le `ROUND(_, 2)` d'origine (no-op sur un entier) est conservé pour fidélité |
| Filtres commentés dans le corps (`o.cash_register_id = ?`, `o.state IN ('CLOSED','DONE')`) | **non repris** (documentés en commentaire Go) | Seul `p.cash_register_id` fait foi — cohérent avec le modèle de requalification des paiements (rapports 19/20) |
| `p.enabled is true` | conservé | Valide dans les deux dialectes |
| `INNER JOIN merchant` | supprimée | Même raison qu'au §1 |

## 3. Changements côté Go

- `GetCashRegisterReport` et `GetCashRegisterTVADetails` passent de `dbutils.GetDB` à
  **`dbx.GetDB`** (rebind `?` → `$N` sous `DB_DIALECT=postgres`, requête inchangée sous MySQL).
  Le reste du module (dont `CloseCashRegister` et ses `UPDATE ... INNER JOIN` MySQL) reste sur
  `dbutils` — sa conversion complète est un chantier séparé (voir note du rapport 20).
- Le paramètre des requêtes *header* sur `cash_registers` (`WHERE cash_register_id = ?`) est
  désormais **parsé en int** (`strconv.Atoi`) : le PK est `integer` en PG et pgx type les strings
  Go en `text` (pas de coercition implicite `integer = text`). Sous MySQL, un int sur un `int(11)`
  est également correct. Un id non numérique (impossible selon l'audit 19 §6) retourne maintenant
  une erreur explicite au lieu d'un `ErrNoRows`.
- Les boucles `rows.NextResultSet()` (« drain » obligatoire des résultats multiples d'un `CALL`)
  sont supprimées — plus de multi-result-set avec des requêtes plates.
- Le type local `ReportRow` de `GetCashRegisterReport` est remplacé par `models.CashReportLine`
  (champs identiques), partagé avec `GetCashRegisterTVADetails` via le helper commun.
- Comportement inchangé : mêmes colonnes, mêmes filtres (`state IN ('CLOSED')`,
  `brand_status NOT IN ('CANCELED','DELETED')`), même construction des groupes par
  `delivery_type`, mêmes totaux.

Point de forme connu : `PeriodFrom`/`PeriodTo` (scan d'un `timestamptz` dans un `string` Go)
sortiront au format RFC 3339 sous PG au lieu du `YYYY-MM-DD hh:mm:ss` MySQL — artefact générique
de la migration (drivers), pas spécifique à ce module ; à traiter globalement si les clients y
sont sensibles.

## 4. Vérification réelle contre le Postgres Docker de dev

Test d'intégration [postgres_integration_test.go](../../internal/modules/cash_registers/postgres_integration_test.go)
(tag `postgres_integration`), exécuté contre `welloresto-postgres-dev` (localhost:5433) :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/modules/cash_registers/...
```

**Résultat : PASS** (données seedées puis nettoyées par le test). Scénario :

- 3 catégories TVA : 10 % (`IN`), 5,5 % (`TAKE_AWAY`), et la ligne spéciale `tva_id = -1`
  (20 %, `show_in_report = false`) insérée avec `OVERRIDING SYSTEM VALUE` (la colonne est
  `GENERATED ALWAYS AS IDENTITY` en cible — **la copie des données de prod devra faire pareil
  pour préserver la ligne `-1`**) ;
- commande sur place `CLOSED` (2 × 1000) → attendu HT 1818 / TTC 2000 / TVA 182 ✓ ;
- commande à emporter `CLOSED` (3 × 500, `delivery_fees` 300) → attendu HT 1422 / TTC 1500 /
  TVA 78 ✓, et branche fees → HT 240 / TTC 300 / TVA 60 ✓ (formule d'origine
  `fees × (100 − taux)/100` conservée telle quelle) ;
- commandes `OPEN` et `CANCELED` : exclues ✓ ;
- MOP : ES 2000 + CB (1000 + 500) = 1500 ✓ ; paiements `enabled = false`, sans registre (NULL)
  et de commande `CANCELED` : exclus ✓ ;
- totaux `GetCashRegisterReport` : HT 3480 / TTC 3800 / TVA 320 ✓ ; `GetCashRegisterTVADetails`
  mêmes totaux, et `nil` pour un mauvais `merchant_id` ✓.

`go build ./...` OK ; le module n'a pas d'autres tests unitaires.

## 5. Cohérence avec le correctif Kiosk (rapport 20)

Le 3ᵉ `UPDATE` de rattrapage ([rapport 20 §2](20-kiosk-cash-register-fix.md)) requalifie les
paiements `CB` orphelins (`cash_register_id` NULL ou `'KIOSK'`) vers la caisse qui se ferme,
**avant** l'appel à `GetCashRegisterReport` dans `CloseCashRegister`. La traduction préserve
exactement la sémantique dont ce correctif dépend :

- **MOP** : la requête traduite filtre sur `p.cash_register_id = ?` uniquement (les filtres
  commentés du corps d'origine ne sont pas réintroduits) → les paiements Kiosk rattrapés en 3bis
  portent l'id numérique de la caisse au moment du rapport et **apparaissent dans la ventilation
  MOP** (ligne `CB`), comme voulu par le correctif.
- **TVA** : la requête traduite filtre sur `o.cash_register_id` (côté *orders*), jamais
  requalifié ([rapport 19 §4.1](19-cash-register-id-audit.md)) → les articles des commandes
  Kiosk/ScanNOrder/UberEats/Deliveroo restent **hors de la ventilation TVA** du rapport Z,
  exactement comme avec la procédure d'origine. L'angle mort structurel documenté au rapport 19
  §6 est donc **conservé à l'identique** (aucune régression, aucune correction silencieuse) —
  sa résorption éventuelle reste le chantier « option B/C » du rapport 19 §7.

Conséquence assumée (préexistante) : un rapport Z peut présenter un TTC MOP ≠ TTC TVA dès que des
paiements de canaux sans caisse sont rattrapés — c'était déjà le cas avec les procédures.

## 6. Fichiers modifiés

| Fichier | Nature |
|---|---|
| [internal/modules/cash_registers/repository.go](../../internal/modules/cash_registers/repository.go) | 2 constantes SQL + 2 helpers ; `GetCashRegisterReport` et `GetCashRegisterTVADetails` réécrits (dbx, plus de `CALL`, param header en int) |
| [internal/modules/cash_registers/postgres_integration_test.go](../../internal/modules/cash_registers/postgres_integration_test.go) | Nouveau test d'intégration réel (tag `postgres_integration`) |

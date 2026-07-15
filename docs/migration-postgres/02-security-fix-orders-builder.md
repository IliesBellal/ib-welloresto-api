# Correctif sécurité : injection SQL dans le builder `orders` — 02

> Fait suite à [01-audit.md](01-audit.md), §2.3.d, qui signalait `orders_fetcher_builder.go` comme le point le plus risqué de la future migration Postgres et une injection SQL déjà présente sur MySQL. Ce correctif est **indépendant de la migration Postgres** : il s'applique tel quel sur le schéma MySQL actuel.

## Ce qui a été trouvé

`OrdersFetcher.FetchAndBuildOrders(ctx, merchantID, whereFilters, orderByFilter, limitsFilters string)` ([internal/modules/orders/orders_fetcher_builder.go](../../internal/modules/orders/orders_fetcher_builder.go)) concatène directement `whereFilters` (une `string`) dans 9 requêtes SQL. Cinq appelants construisaient ce fragment par interpolation de valeurs avec `fmt.Sprintf`, sans passage par un placeholder `?` :

| Fichier:ligne (avant correctif) | Pattern vulnérable |
|---|---|
| `internal/modules/orders/repository.go` (`GetOrdersByIDs`) | `idsStr += fmt.Sprintf("'%s'", oid)` puis `fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr)` |
| `internal/modules/orders/repository.go` (`GetOrder`) | `fmt.Sprintf(" AND o.order_id = '%s' ", orderID)` |
| `internal/modules/orders/repository.go` (`GetOrders`) | boucle manuelle équivalente à `IN (...)` avec valeurs quotées à la main |
| `internal/modules/orders/repository.go` (`GetHistory`) | `inParts = append(inParts, fmt.Sprintf("'%s'", id))` puis `fmt.Sprintf(" AND o.order_id IN (%s) ", strings.Join(inParts, ","))` |
| `internal/modules/delivery_sessions/repository.go` (deux call sites) | mêmes patterns (`IN (...)` construit à la main, `= '%s'` construit à la main) |

Dans `GetOrder`, `orderID` provient directement d'un paramètre de route HTTP (id de commande) — **injection SQL exploitable côté utilisateur non filtré**. Dans les autres cas, les valeurs proviennent d'IDs déjà lus en base (`order_id` récupérés par une requête précédente), donc moins immédiatement exploitables de l'extérieur, mais le pattern reste incorrect et fragile (tout ID métier contenant un guillemet simple casse la requête ou, pire, la détourne).

## Ce qui a été changé

### 1. Nouveau type `QueryFilter` ([internal/modules/orders/query_filter.go](../../internal/modules/orders/query_filter.go))

```go
type QueryFilter struct {
    SQL  string
    Args []interface{}
}

func NewFilter(sql string, args ...interface{}) QueryFilter
func (f QueryFilter) Append(other QueryFilter) QueryFilter
func InFilter(column string, values []string) QueryFilter // " AND col IN (?,?,...) "
```

`QueryFilter` porte un fragment SQL à placeholders `?` **et** ses arguments dans le même ordre que ces placeholders. `Append` concatène deux filtres en préservant l'ordre SQL/Args. `InFilter` génère le bon nombre de `?` pour une liste de valeurs (le nombre de valeurs, donc de placeholders, reste dynamique — c'est le cas signalé comme le plus risqué dans l'audit — mais désormais les valeurs ne passent plus jamais par le texte de la requête).

### 2. `FetchAndBuildOrders` prend un `QueryFilter` au lieu d'une `string`

Signature : `FetchAndBuildOrders(ctx, merchantID string, whereFilters QueryFilter, orderByFilter, limitsFilters string) (...)`.

Dans les 9 requêtes qui utilisaient `... WHERE o.merchant_id = ? ` + whereFilters`, le fragment devient `whereFilters.SQL` et les arguments passés à `db.QueryContext` sont désormais `merchantID` suivi de `whereFilters.Args...` (un helper interne `filteredArgs()` construit ce slice une fois). `orderByFilter` et `limitsFilters` restent des `string` : ce sont toujours des fragments statiques sans valeur utilisateur (ex. `" ORDER BY o.creation_date DESC "`), donc hors du périmètre de ce correctif.

### 3. Tous les appelants réécrits en `?` + args

- `GetOrder` : `filter := NewFilter(" AND o.order_id = ? ", orderID)`
- `GetOrdersByIDs`, `GetOrders`, `GetHistory` : `filter := InFilter("o.order_id", ids)`
- `internal/modules/delivery_sessions/repository.go` (les deux call sites de `FetchAndBuildOrders`, seul autre consommateur du builder partagé) : mêmes remplacements (`orders.InFilter(...)`, `orders.NewFilter(...)`)

Plus aucune valeur n'est interpolée dans une chaîne SQL dans ce chemin de code ; le nombre de `?` varie toujours selon le nombre d'IDs (cas signalé dans l'audit), mais il est désormais généré et consommé de façon cohérente par `InFilter`.

## Fichiers touchés

- `internal/modules/orders/query_filter.go` (nouveau)
- `internal/modules/orders/query_filter_test.go` (nouveau)
- `internal/modules/orders/orders_fetcher_builder.go`
- `internal/modules/orders/orders_fetcher_builder_test.go` (nouveau)
- `internal/modules/orders/repository.go`
- `internal/modules/delivery_sessions/repository.go` (mise à jour forcée par le changement de signature du builder partagé)

Rien d'autre n'a été modifié : pas de conversion Postgres, pas de changement des autres 100+ requêtes du module `orders` qui utilisaient déjà `?` correctement.

## Tests

Le module `orders` n'avait aucun fichier de test avant ce correctif. Deux fichiers ont été ajoutés :

- `query_filter_test.go` : tests unitaires purs (pas de DB) sur `NewFilter`, `InFilter` (cas simple, cas vide, cas d'une valeur contenant un guillemet simple pour confirmer qu'elle ne casse plus la requête) et `Append` (ordre SQL/Args préservé après concaténation).
- `orders_fetcher_builder_test.go` : test de bout en bout via `go-sqlmock` (déjà une dépendance du repo, utilisée ailleurs — ex. `internal/modules/customers/repository_test.go`) qui exécute réellement `FetchAndBuildOrders` avec un `QueryFilter` :
  - `TestFetchAndBuildOrders_SingleOrderIDFilter` : reproduit le cas `GetOrder` (un seul `order_id`), vérifie que la requête finale reçoit `(merchantID, orderID)` comme arguments et un seul `?`.
  - `TestFetchAndBuildOrders_InFilterMultipleOrderIDs` : reproduit le cas `GetOrdersByIDs`/`GetOrders`/`GetHistory` (plusieurs `order_id`), vérifie que le SQL contient `IN (?,?,?)` et que les arguments sont dans l'ordre `(merchantID, id1, id2, id3)`.

Résultats :

```
go build ./...                     # OK
go test ./internal/modules/orders/...              # PASS (8 tests)
go test ./internal/modules/order_life_cycle/...     # PASS (inchangé, consommateur indirect potentiel)
go test ./internal/modules/delivery_sessions/...    # pas de fichier de test existant
go test ./internal/modules/kiosk/...                # PASS (inchangé)
```

Les échecs pré-existants sur `planning/leave`, `planning/swaps`, `pos/accounting` (build) et `ubereats` (build) ont été vérifiés comme **indépendants de ce correctif** (`git stash` + re-run : mêmes échecs sans les changements de cette tâche).

## Réutilisabilité pour la conversion Postgres

Ce correctif a été conçu pour être directement exploitable lors de la conversion `?` → `$1..$n` documentée dans [01-audit.md](01-audit.md) (§2.2, §2.3) :

- `QueryFilter{SQL, Args}` est exactement la paire (fragment, arguments ordonnés) dont a besoin un renumérotage `sqlx.Rebind(sqlx.DOLLAR, sql)` (ou l'équivalent manuel) : le futur portage n'aura qu'à appliquer `Rebind` sur `whereFilters.SQL` concaténé aux fragments statiques, sans toucher à la logique de construction des filtres.
- Le nombre variable de placeholders dans `InFilter` (le point signalé comme le plus délicat dans l'audit pour la conversion `IN (%s)`) est déjà généré dynamiquement de façon centralisée — un futur `InFilterPg` n'aurait qu'à générer `$1,$2,...` à la place de `?,?,...` en réutilisant la même signature et le même slice `Args`.
- Ce pattern peut être étendu aux autres 128 requêtes à placeholders dynamiques identifiées dans l'audit (§2.3.b) au fur et à mesure, sans attendre la bascule Postgres — chaque module peut adopter `QueryFilter` indépendamment.

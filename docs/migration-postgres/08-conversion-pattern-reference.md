# 08 — Pattern de conversion des repositories (MySQL → PostgreSQL)

Référence pour convertir un module au point d'entrée centralisé `internal/database/dbx`.
Aucun module n'est encore converti — ce document décrit le pattern à appliquer module par module.

## Infrastructure en place

| Élément | Fichier | Rôle |
|---|---|---|
| Connexion Postgres | `internal/database/postgres.go` | `NewPostgres()` via driver `pgx` (stdlib `database/sql`), mêmes options de pool que MySQL, DSN dans `POSTGRES_URL` |
| Sélecteur de dialecte | `internal/database/dbx/dialect.go` | `DB_DIALECT=mysql\|postgres` (défaut : `mysql`) |
| Wrapper d'exécution | `internal/database/dbx/db.go` | `dbx.GetDB(ctx, db)` — même signature que `dbutils.GetDB`, applique `sqlx.Rebind` si Postgres actif |
| Helpers d'erreurs | `internal/database/dbx/errors.go` | `IsDuplicateEntry`, `IsForeignKeyViolation` — cross-dialecte |

Le rebind convertit les placeholders `?` en `$1, $2, ...` via `sqlx.Rebind(sqlx.DOLLAR, query)`
uniquement quand `DB_DIALECT=postgres`. En MySQL (défaut), la requête part au driver **inchangée** :
zéro impact sur le comportement actuel.

## Comment convertir un repository

Tous les repositories passent déjà par `dbutils.GetDB(ctx, r.database)` pour résoudre
connexion vs transaction. La conversion consiste à remplacer cet appel par `dbx.GetDB` —
le wrapper retourné implémente `dbutils.DBTX`, donc **aucune autre ligne ne change**.

### Avant

```go
import (
	"welloresto-api/internal/utils/dbutils"
)

func (r *Repository) ListAllergens(ctx context.Context) ([]models.AllergenEntry, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT allergen_id, name, code, icon, color FROM allergens WHERE merchant_id = ? AND enabled = ?`,
		merchantID, 1,
	)
	// ...
}
```

### Après

```go
import (
	"welloresto-api/internal/database/dbx"
)

func (r *Repository) ListAllergens(ctx context.Context) ([]models.AllergenEntry, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT allergen_id, name, code, icon, color FROM allergens WHERE merchant_id = ? AND enabled = ?`,
		merchantID, 1,
	)
	// ...
}
```

Le SQL reste écrit avec `?`. En Postgres, le wrapper l'envoie au driver sous la forme
`... WHERE merchant_id = $1 AND enabled = $2`.

Les transactions continuent de fonctionner : `dbutils.RunInTx` injecte le `*sql.Tx` dans le
contexte, et `dbx.GetDB` le récupère exactement comme `dbutils.GetDB` (il l'appelle en interne).

## Gestion des erreurs

### `sql.ErrNoRows` — identique aux deux drivers

`QueryRowContext(...).Scan(...)` retourne `sql.ErrNoRows` avec les deux drivers
(`go-sql-driver/mysql` et `pgx/stdlib`). Le code existant ne change pas :

```go
err := db.QueryRowContext(ctx, `SELECT name FROM products WHERE product_id = ?`, id).Scan(&name)
if errors.Is(err, sql.ErrNoRows) {
	return nil, ErrProductNotFound
}
```

### Violations de contraintes — codes différents

| Contrainte | MySQL | Postgres |
|---|---|---|
| Unicité (duplicate entry) | erreur `1062` | SQLSTATE `23505` |
| Clé étrangère | erreur `1451`/`1452` | SQLSTATE `23503` |

Ne jamais tester le code d'erreur d'un seul driver en dur. Utiliser les helpers `dbx`,
qui détectent le type d'erreur du driver réellement actif via `errors.As` :

**Avant (spécifique MySQL) :**

```go
var mysqlErr *mysql.MySQLError
if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
	return ErrEmailAlreadyExists
}
```

**Après (cross-dialecte) :**

```go
if dbx.IsDuplicateEntry(err) {
	return ErrEmailAlreadyExists
}
```

En interne, `IsDuplicateEntry` teste `*mysql.MySQLError{Number: 1062}` **et**
`*pgconn.PgError{Code: "23505"}` — le bon match se fait naturellement selon le driver
qui a produit l'erreur, sans consulter `DB_DIALECT`.

## Pièges du rebind

- **`?` littéral interdit dans le SQL** : le rebind est textuel. Un `?` dans une chaîne en dur
  ou un opérateur JSON Postgres (`?`, `?|`, `?&`) serait réécrit en `$N`. Passer les valeurs en
  paramètres ; pour les opérateurs JSON, utiliser les fonctions équivalentes (`jsonb_exists`).
- **Placeholders répétés** : `?` ne peut pas être réutilisé pour la même valeur comme `$1` en
  Postgres natif — chaque `?` devient un `$N` distinct, il faut donc passer la valeur autant de
  fois qu'elle apparaît (comportement déjà requis par MySQL, rien ne change).
- **SQL construit dynamiquement** (query builders, `strings.Builder`) : continuer à générer du
  `?` ; le rebind s'applique sur la chaîne finale au moment de l'exécution.

## Checklist de conversion d'un module

1. Remplacer `dbutils.GetDB` par `dbx.GetDB` dans `repository.go` (import compris).
2. Remplacer toute détection d'erreur `1062`/`1452` en dur par `dbx.IsDuplicateEntry` / `dbx.IsForeignKeyViolation`.
3. Chasser les incompatibilités SQL du module (voir `04-schema-mapping-notes.md`) : backticks,
   `NOW()` vs `now()`, `LIMIT ?, ?`, fonctions de date, `ON DUPLICATE KEY UPDATE` → `ON CONFLICT`.
4. `go build ./...` + tests du module sous les deux valeurs de `DB_DIALECT`.

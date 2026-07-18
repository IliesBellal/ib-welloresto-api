// Package dbx est le point d'entrée unique pour l'exécution de requêtes SQL
// pendant la migration MySQL → PostgreSQL. Il réécrit les placeholders `?`
// en `$1, $2, ...` quand le dialecte actif est Postgres, et laisse les
// requêtes inchangées en MySQL (comportement par défaut).
package dbx

import (
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
)

// EnvDialect est la variable d'environnement qui sélectionne le dialecte actif.
const EnvDialect = "DB_DIALECT"

type Dialect string

const (
	MySQL    Dialect = "mysql"
	Postgres Dialect = "postgres"
)

// ActiveDialect lit DB_DIALECT à chaque appel (coût négligeable) pour que les
// tests puissent simuler les deux dialectes via t.Setenv. Défaut : MySQL.
func ActiveDialect() Dialect {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvDialect))) {
	case "postgres", "postgresql", "pgx":
		return Postgres
	default:
		return MySQL
	}
}

// Rebind réécrit les placeholders `?` en `$N` si le dialecte actif est
// Postgres, sinon retourne la requête telle quelle.
//
// Attention : le rebind est textuel — un `?` littéral dans la requête
// (opérateur JSON Postgres, chaîne en dur) serait réécrit aussi. Passer les
// valeurs en paramètres, jamais de `?` littéral dans le SQL.
func Rebind(query string) string {
	if ActiveDialect() == Postgres {
		return sqlx.Rebind(sqlx.DOLLAR, query)
	}
	return query
}

// UTCNow retourne le fragment SQL « instant courant » selon le dialecte actif.
// MySQL : UTC_TIMESTAMP() (colonnes timestamp stockées en UTC).
// Postgres : now() — les colonnes cibles sont timestamptz, now() est l'instant
// absolu correct quel que soit le fuseau de session (ne pas utiliser
// `now() AT TIME ZONE 'UTC'`, qui produit un timestamp naïf réinterprété
// dans le fuseau de session à l'insertion en timestamptz).
func UTCNow() string {
	if ActiveDialect() == Postgres {
		return "now()"
	}
	return "UTC_TIMESTAMP()"
}

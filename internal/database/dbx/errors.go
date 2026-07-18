package dbx

import (
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsDuplicateEntry détecte une violation de contrainte d'unicité,
// quel que soit le driver actif (MySQL 1062 / Postgres 23505).
func IsDuplicateEntry(err error) bool {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1062
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsForeignKeyViolation détecte une violation de clé étrangère
// (MySQL 1451/1452 / Postgres 23503).
func IsForeignKeyViolation(err error) bool {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1451 || myErr.Number == 1452
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

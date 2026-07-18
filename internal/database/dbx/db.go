package dbx

import (
	"context"
	"database/sql"

	"welloresto-api/internal/utils/dbutils"
)

// DB enveloppe un dbutils.DBTX (sql.DB ou sql.Tx) et applique Rebind sur
// chaque requête avant exécution. Il implémente lui-même dbutils.DBTX.
type DB struct {
	base dbutils.DBTX
}

// Wrap enveloppe n'importe quel DBTX (connexion ou transaction).
func Wrap(base dbutils.DBTX) *DB {
	return &DB{base: base}
}

// GetDB remplace dbutils.GetDB dans les repositories convertis : même
// signature, même résolution de transaction depuis le contexte, plus le
// rebind des placeholders selon le dialecte actif.
func GetDB(ctx context.Context, defaultDB *sql.DB) *DB {
	return &DB{base: dbutils.GetDB(ctx, defaultDB)}
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return d.base.ExecContext(ctx, Rebind(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.base.QueryContext(ctx, Rebind(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.base.QueryRowContext(ctx, Rebind(query), args...)
}

func (d *DB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return d.base.PrepareContext(ctx, Rebind(query))
}

// InsertReturningID exécute un INSERT et retourne l'ID auto-généré.
// MySQL : Exec + LastInsertId. Postgres : le driver pgx ne supporte pas
// LastInsertId — la requête est suffixée de `RETURNING <idColumn>` et lue
// via QueryRow. La requête passée ne doit PAS contenir de RETURNING.
func (d *DB) InsertReturningID(ctx context.Context, query, idColumn string, args ...interface{}) (int64, error) {
	if ActiveDialect() == Postgres {
		var id int64
		err := d.base.QueryRowContext(ctx, Rebind(query+" RETURNING "+idColumn), args...).Scan(&id)
		return id, err
	}
	res, err := d.base.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return d.ExecContext(context.Background(), query, args...)
}

func (d *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return d.QueryContext(context.Background(), query, args...)
}

func (d *DB) QueryRow(query string, args ...interface{}) *sql.Row {
	return d.QueryRowContext(context.Background(), query, args...)
}

package dbutils

import (
	"context"
	"database/sql"
)

// DBTX unifie sql.DB et sql.Tx pour que tes repos ne fassent pas la différence
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type txKey struct{}

// InjectTx place la transaction dans le contexte
func InjectTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// ExtractTx récupère la transaction si elle existe
func ExtractTx(ctx context.Context) *sql.Tx {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return nil
}

func GetDB(ctx context.Context, defaultDB *sql.DB) DBTX {
	if tx := ExtractTx(ctx); tx != nil {
		return tx
	}
	return defaultDB
}

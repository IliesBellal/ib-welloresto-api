package dbutils

import (
	"context"
	"database/sql"
)

// RunInTx exécute une fonction à l'intérieur d'une transaction SQL
func RunInTx(ctx context.Context, db *sql.DB, fn func(txCtx context.Context) error) error {
	// Si on est déjà dans une transaction (imbrication), on exécute juste la fonction
	if ExtractTx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// On crée un nouveau contexte contenant la transaction
	txCtx := InjectTx(ctx, tx)

	// On exécute ta logique métier (le Service)
	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

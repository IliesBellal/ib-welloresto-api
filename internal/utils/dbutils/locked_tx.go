package dbutils

import (
	"context"
	"database/sql"
	"fmt"
)

// RunInTxLocked exécute fn dans une transaction qui détient d'abord le verrou
// consultatif nommé key (pg_advisory_xact_lock, levé à la fin de la transaction).
// Deux traitements qui portent la même clé s'exécutent l'un après l'autre.
//
// Sert aux « trouver ou créer » des mappings marketplace (produit, groupe
// d'options, option) : deux commandes simultanées qui contiennent le même article
// inconnu le créaient en double ou échouaient sur l'index unique du mapping. Le
// verrou est tenu le temps de relire puis créer, pas pendant le chemin courant
// (mapping existant), qui ne le prend jamais.
//
// Déjà dans une transaction (imbrication) : le verrou est pris dans celle-ci et
// tenu jusqu'à sa fin.
func RunInTxLocked(ctx context.Context, db *sql.DB, key string, fn func(txCtx context.Context) error) error {
	return RunInTx(ctx, db, func(txCtx context.Context) error {
		tx := ExtractTx(txCtx)
		if tx == nil {
			return fmt.Errorf("dbutils: no transaction for lock %q", key)
		}
		if _, err := tx.ExecContext(txCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return fmt.Errorf("dbutils: acquire lock %q: %w", key, err)
		}
		return fn(txCtx)
	})
}

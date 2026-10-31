package dbutils

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Sous Postgres, une requête en échec invalide toute la transaction : les
// requêtes suivantes échouent jusqu'au ROLLBACK. Plusieurs étapes de la création
// d'une commande sont volontairement tolérantes (commentaire, coût de revient,
// attribution de remise : l'erreur est journalisée et la commande continue).
// Hors transaction, elles gardent ce comportement sans rien faire de plus ; dans
// une transaction, Isolated et Shield le préservent avec un point de sauvegarde.
//
// Les points de sauvegarde ne sont posés que si l'appelant l'a demandé avec
// WithSavepoints : les flux qui tournent déjà dans une transaction (modification
// d'une commande, paiements…) ne changent donc ni de comportement ni de nombre de
// requêtes.

type savepointsKey struct{}

// WithSavepoints active les points de sauvegarde d'Isolated et de Shield pour les
// requêtes exécutées avec ce contexte.
func WithSavepoints(ctx context.Context) context.Context {
	return context.WithValue(ctx, savepointsKey{}, true)
}

var savepointSeq atomic.Uint64

// Isolated exécute fn de façon qu'une erreur SQL ne rende pas la transaction
// englobante inutilisable. L'erreur de fn est renvoyée telle quelle.
//
// Sans transaction, ou sans WithSavepoints, fn est simplement appelée.
func Isolated(ctx context.Context, fn func() error) error {
	tx := ExtractTx(ctx)
	if tx == nil || ctx.Value(savepointsKey{}) == nil {
		return fn()
	}

	name := fmt.Sprintf("wr_sp_%d", savepointSeq.Add(1))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		// Transaction déjà inutilisable : fn échouera aussi, avec son propre message.
		return fn()
	}

	fnErr := fn()
	if fnErr != nil {
		// Retour à l'état d'avant fn : la transaction redevient utilisable.
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name)
	}
	_, _ = tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	return fnErr
}

// Shield est Isolated pour une fonction qui avale elle-même ses erreurs SQL (elle
// les journalise et continue) : fn ne renvoie rien, c'est donc l'état de la
// transaction qui dit si une requête a échoué. Une requête d'essai le vérifie ; en
// cas d'échec, retour au point de sauvegarde.
func Shield(ctx context.Context, fn func()) {
	tx := ExtractTx(ctx)
	if tx == nil || ctx.Value(savepointsKey{}) == nil {
		fn()
		return
	}

	name := fmt.Sprintf("wr_sp_%d", savepointSeq.Add(1))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		fn()
		return
	}

	fn()
	if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name)
	}
	_, _ = tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
}

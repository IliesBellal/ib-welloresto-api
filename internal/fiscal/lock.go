// Package fiscal porte ce que les cinq chaînes fiscales (orders, payments,
// receipts, cash_registers, audit_logs) ont en commun : le verrou qui
// sérialise leurs écritures par établissement et l'empreinte v2 qui scelle
// chaque ligne (docs/attestation-conformite-01-lot-A-brief.md, constats C3 et
// C5 de docs/attestation-conformite-00-audit.md).
package fiscal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"welloresto-api/internal/utils/dbutils"
)

// ErrNoTransaction : un verrou de transaction pris hors transaction serait
// relâché aussitôt et ne protégerait rien. Signale un chemin d'écriture
// fiscale qui n'ouvre pas sa transaction (voir la solution C du brief : les
// opérations métier et les écritures appelées de partout ouvrent la leur).
var ErrNoTransaction = errors.New("fiscal: chain write outside a transaction")

// LockChain sérialise les écritures d'une chaîne d'un établissement jusqu'à
// la fin de la transaction courante. Réentrant (un second appel dans la même
// transaction ne bloque pas). À prendre juste avant la lecture du dernier
// maillon, jamais en début de transaction : le verrou est tenu jusqu'au
// commit.
//
// Un verrou par chaîne, comme les SELECT ... ORDER BY ... LIMIT 1 FOR UPDATE
// qu'il remplace : même topologie de verrous qu'avant, sans leur course (en
// READ COMMITTED, deux transactions concurrentes y relisaient la même
// dernière ligne et chaînaient sur le même parent). Un verrou unique par
// établissement créait au contraire un cycle avec les verrous de ligne : une
// mutation de commande tient la ligne de la commande avant d'écrire son audit,
// un encaissement tient la chaîne avant de mettre la commande à jour. Les
// chaînes sont toujours prises dans l'ordre payments, orders, receipts,
// cash_registers, audit_logs, fiscal_closures, fiscal_archives : aucun cycle
// entre elles (la clôture fiscale et l'archive, chacune seule dans sa
// transaction, ne prennent aucune autre chaîne).
func LockChain(ctx context.Context, chain Chain, merchantID string) error {
	tx := dbutils.ExtractTx(ctx)
	if tx == nil {
		return ErrNoTransaction
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, chainLockKey(chain, merchantID)); err != nil {
		return fmt.Errorf("fiscal: acquire %s lock: %w", chain, err)
	}
	return nil
}

// chainLockKey est la clé du verrou consultatif d'une chaîne d'un
// établissement (pg_advisory_xact_lock(hashtextextended(clé, 0))).
func chainLockKey(chain Chain, merchantID string) string {
	return "fiscal:" + string(chain) + ":" + merchantID
}

// TryLock prend, sans attendre, un verrou consultatif nommé, tenu par une
// transaction dédiée jusqu'à l'appel de release (ou jusqu'à la perte de la
// connexion). ok vaut false si quelqu'un d'autre le tient. Sert aux travaux
// longs qu'une seule instance ou une seule demande doit mener à la fois
// (archives fiscales, lot D) ; jamais pour une écriture de chaîne, qui prend
// LockChain dans sa propre transaction.
func TryLock(ctx context.Context, db *sql.DB, key string) (release func(), ok bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("fiscal: try lock %s: %w", key, err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, key).Scan(&ok); err != nil {
		_ = tx.Rollback()
		return nil, false, fmt.Errorf("fiscal: try lock %s: %w", key, err)
	}
	if !ok {
		_ = tx.Rollback()
		return nil, false, nil
	}
	return func() { _ = tx.Rollback() }, true, nil
}

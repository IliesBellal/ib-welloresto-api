package fiscal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Réouverture d'une commande close (conformité caisse, lot C, C1 :
// docs/attestation-conformite-03-lot-C-brief.md, R1 et S6). Refusée si la
// commande est scellée (la clôture journalière de son jour de clôture est
// écrite, lot B) ou si l'un de ses paiements actifs a été encaissé sur un
// registre déjà fermé — une commande peut l'être sur plusieurs registres.

// orderSealedSQL : la commande (alias o) figure dans une clôture journalière.
// Un établissement sans fuseau n'a pas de clôture : jamais scellée.
const orderSealedSQL = `EXISTS (
		SELECT 1 FROM fiscal_closures fc
		WHERE fc.merchant_id = o.merchant_id AND fc.period_type = 'DAY'
		  AND fc.period_start = (o.delivered_on AT TIME ZONE
		      (SELECT NULLIF(m.timezone, '') FROM merchant m WHERE m.id::text = o.merchant_id))::date)`

// paymentRegisterIDSQL : identifiant numérique du registre d'un paiement
// (alias p) ; NULL pour un marqueur (SCANNORDER, UBER_EATS, KIOSK...) ou
// aucun registre.
const paymentRegisterIDSQL = `CASE WHEN p.cash_register_id ~ '^[0-9]+$' THEN p.cash_register_id::bigint END`

// ReopenOrder rouvre une commande close, dans une transaction (la sienne ou
// celle de l'appelant). Refus : models.ErrNotFound (commande inconnue pour
// cet établissement), models.ErrReopenOrderSealed,
// models.ErrReopenPaymentRegisterClosed. Une commande déjà ouverte est un
// no-op (reopened = false).
//
// Deux requêtes : la première verrouille la commande puis la chaîne des
// clôtures fiscales de l'établissement (une clôture journalière en cours
// d'écriture finit avant le contrôle, et inversement) ; la seconde, lancée
// ensuite et donc à jour, vérifie, verrouille en partage les registres des
// paiements (leur fermeture attend) et rouvre.
func ReopenOrder(ctx context.Context, database *sql.DB, merchantID, orderID string) (reopened bool, err error) {
	err = dbutils.RunInTx(ctx, database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, database)
		var lock sql.NullString
		err := db.QueryRowContext(txCtx, `
			SELECT pg_advisory_xact_lock(hashtextextended(?, 0))::text
			FROM (SELECT order_id FROM orders WHERE order_id = ? AND merchant_id = ? FOR UPDATE) o`,
			chainLockKey(ChainFiscalClosures, merchantID), orderID, merchantID).Scan(&lock)
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fiscal: lock order %s for reopen: %w", orderID, err)
		}

		var state string
		var sealed, registerClosed, updated bool
		err = db.QueryRowContext(txCtx, `
			WITH regs AS (
				SELECT closed FROM cash_registers
				WHERE cash_register_id IN (
					SELECT `+paymentRegisterIDSQL+` FROM payments p WHERE p.order_id = ? AND p.enabled = TRUE)
				FOR SHARE
			), chk AS (
				SELECT o.state, `+orderSealedSQL+` AS sealed,
				       EXISTS (SELECT 1 FROM regs WHERE closed) AS register_closed
				FROM orders o WHERE o.order_id = ?
			), upd AS (
				UPDATE orders SET state = 'OPEN'
				FROM chk
				WHERE orders.order_id = ? AND chk.state = 'CLOSED' AND NOT chk.sealed AND NOT chk.register_closed
				RETURNING orders.order_id
			)
			SELECT chk.state, chk.sealed, chk.register_closed, EXISTS (SELECT 1 FROM upd) FROM chk`,
			orderID, orderID, orderID).Scan(&state, &sealed, &registerClosed, &updated)
		if err != nil {
			return fmt.Errorf("fiscal: reopen order %s: %w", orderID, err)
		}
		switch {
		case updated:
			reopened = true
			return nil
		case state != "CLOSED":
			return nil
		case sealed:
			return models.ErrReopenOrderSealed
		case registerClosed:
			return models.ErrReopenPaymentRegisterClosed
		}
		return fmt.Errorf("fiscal: reopen order %s: not updated", orderID)
	})
	return reopened, err
}

// ReopenableOrders indique, pour chaque commande close de la liste, si elle
// peut être rouverte (mêmes règles que ReopenOrder, sans verrou) : la caisse
// masque le bouton de réouverture sinon. Les commandes ouvertes ou inconnues
// sont absentes du résultat. Une requête.
func ReopenableOrders(ctx context.Context, db *dbx.DB, orderIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(orderIDs))
	for i, id := range orderIDs {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, `
		SELECT o.order_id::text,
		       NOT `+orderSealedSQL+`
		       AND NOT EXISTS (
		           SELECT 1 FROM payments p
		           JOIN cash_registers cr ON cr.cash_register_id = `+paymentRegisterIDSQL+`
		           WHERE p.order_id = o.order_id AND p.enabled = TRUE AND cr.closed)
		FROM orders o
		WHERE o.state = 'CLOSED' AND o.order_id IN (`+strings.TrimSuffix(strings.Repeat("?, ", len(orderIDs)), ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("fiscal: reopenable orders: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ok bool
		if err := rows.Scan(&id, &ok); err != nil {
			return nil, err
		}
		out[id] = ok
	}
	return out, rows.Err()
}

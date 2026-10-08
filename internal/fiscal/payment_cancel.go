package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Sources d'une annulation de paiement, inscrites au journal d'audit.
const (
	CancelSourceStaff     = "STAFF"
	CancelSourceSystem    = "SYSTEM"
	CancelSourceStripe    = "STRIPE"
	CancelSourceUberEats  = "UBER_EATS"
	CancelSourceDeliveroo = "DELIVEROO"
	CancelSourceCustomer  = "CUSTOMER"
)

// PaymentCancellation décrit une annulation de paiement (conformité caisse,
// lot C, C2 : docs/attestation-conformite-03-lot-C-brief.md, R4 et R5).
type PaymentCancellation struct {
	MerchantID string
	OrderID    string
	// PaymentID : le paiement à annuler (CancelPayment seulement).
	PaymentID string
	Source    string
	UserID    string
	Reason    string
	// MarkOrderUnpaid : la commande repasse non payée (isPaid) si un
	// paiement est annulé — annulation depuis la caisse (CancelPayment).
	MarkOrderUnpaid bool
}

// CancelledPayment est un paiement qui vient d'être annulé.
type CancelledPayment struct {
	PaymentID int64
	Amount    int64
	MOP       string
}

// cancelledPaymentState est l'état du paiement avant l'annulation (old_values
// de l'entrée d'audit) : rien de ce qui y figure n'est ensuite modifié.
type cancelledPaymentState struct {
	PaymentID      int64   `json:"payment_id"`
	OrderID        string  `json:"order_id"`
	Amount         int64   `json:"amount"`
	MOP            string  `json:"mop"`
	OperationType  *string `json:"operation_type"`
	PaymentDate    *string `json:"payment_date"`
	CashRegisterID *string `json:"cash_register_id"`
	Enabled        bool    `json:"enabled"`
}

// paymentCancelChange est le changement inscrit au journal (new_values).
type paymentCancelChange struct {
	Enabled bool    `json:"enabled"`
	Source  string  `json:"source"`
	Reason  *string `json:"reason"`
}

// Une annulation, quel que soit son chemin :
//   - verrouille la commande, vérifie qu'elle appartient à l'établissement et
//     qu'elle est ouverte ;
//   - passe enabled à false sur le ou les paiements actifs, et rien d'autre :
//     montant, moyen, date et registre d'origine ne changent jamais ;
//   - refuse si l'un d'eux a été encaissé sur un registre déjà fermé (le
//     registre est verrouillé en partage : sa fermeture attend la fin de
//     l'annulation, et inversement) ;
//   - inscrit une entrée PAYMENT_CANCELLED par paiement au journal d'audit
//     chaîné et signé (état d'origine, source, utilisateur, motif).
//
// Refus : models.ErrNotFound (commande ou paiement inconnu pour cet
// établissement) ; commande close : models.ErrPaymentOrderClosed pour un
// paiement, models.ErrOrderClosed pour toute la commande ; registre fermé :
// models.ErrPaymentRegisterClosed ou models.ErrOrderPaymentRegisterClosed.

// CancelPayment annule un paiement (caisse, webhook). nil sans erreur : il
// était déjà annulé (double appui, webhook rejoué), rien n'est écrit.
//
// Quatre allers-retours dans la transaction : verrous de la commande et de la
// chaîne d'audit (dans cet ordre, en une requête), annulation avec contrôle
// des registres et lecture du dernier maillon (une requête), insertion de
// l'entrée. Le verrou de la chaîne est pris tôt sans risque d'interblocage :
// ensuite, seuls les registres (dont la fermeture n'écrit pas au journal) et
// les paiements de cette commande, déjà verrouillée, sont verrouillés.
func CancelPayment(ctx context.Context, database *sql.DB, c PaymentCancellation) (*CancelledPayment, error) {
	if c.PaymentID == "" {
		return nil, models.ErrNotFound
	}
	var out *CancelledPayment
	err := dbutils.RunInTx(ctx, database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, database)
		if _, err := lockOpenOrder(txCtx, db, c, true); err != nil {
			return err
		}
		states, prev, err := disablePayments(txCtx, db, c, true)
		if err != nil {
			return err
		}
		if len(states) == 0 {
			var exists bool
			if err := db.QueryRowContext(txCtx, `
				SELECT EXISTS (SELECT 1 FROM payments WHERE payment_id = ? AND order_id = ?)`,
				c.PaymentID, c.OrderID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return models.ErrNotFound
			}
			return nil
		}
		entries, err := cancelAuditEntries(c, states)
		if err != nil {
			return err
		}
		if err := insertAuditEntries(txCtx, db, prev.String, entries); err != nil {
			return fmt.Errorf("fiscal: audit payment %d cancel: %w", states[0].PaymentID, err)
		}
		out = &CancelledPayment{PaymentID: states[0].PaymentID, Amount: states[0].Amount, MOP: states[0].MOP}
		return nil
	})
	return out, err
}

// CancelOrderPayments annule tous les paiements actifs d'une commande qu'on
// annule ou qu'on refuse, puis appelle closeOrder (la clôture de la commande
// et ses autres écritures), puis écrit le journal : dans une transaction (la
// sienne ou celle de l'appelant). Sans paiement, vérifie seulement que la
// commande est ouverte. closeOrder reçoit hasSaleReceipt : la commande a déjà
// un ticket de vente (rouverte après une vente, son annulation appelle un
// avoir — lot C, R2), lu avec le verrou de la commande.
//
// Le verrou de la chaîne d'audit est pris en dernier, juste avant
// l'écriture du journal : closeOrder verrouille des lignes (récompenses,
// fiche client) que d'autres transactions tiennent avant d'écrire leur
// propre entrée d'audit.
func CancelOrderPayments(ctx context.Context, database *sql.DB, c PaymentCancellation, closeOrder func(ctx context.Context, hasSaleReceipt bool) error) ([]CancelledPayment, error) {
	var out []CancelledPayment
	err := dbutils.RunInTx(ctx, database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, database)
		c.PaymentID = ""
		hasSale, err := lockOpenOrder(txCtx, db, c, false)
		if err != nil {
			return err
		}
		states, _, err := disablePayments(txCtx, db, c, false)
		if err != nil {
			return err
		}
		if closeOrder != nil {
			if err := closeOrder(txCtx, hasSale); err != nil {
				return err
			}
		}
		entries, err := cancelAuditEntries(c, states)
		if err != nil {
			return err
		}
		if err := AppendAuditLogs(txCtx, db, c.MerchantID, entries); err != nil {
			return fmt.Errorf("fiscal: audit payments cancel of order %s: %w", c.OrderID, err)
		}
		for _, s := range states {
			out = append(out, CancelledPayment{PaymentID: s.PaymentID, Amount: s.Amount, MOP: s.MOP})
		}
		return nil
	})
	return out, err
}

// lockOpenOrder verrouille la commande jusqu'au commit (une clôture, une
// réouverture ou une autre annulation concurrente attend) et vérifie qu'elle
// est ouverte et de cet établissement. withAuditLock prend aussi le verrou de
// la chaîne d'audit, après celui de la ligne (projection au-dessus de
// LockRows). hasSale : la commande a déjà un ticket de vente.
func lockOpenOrder(ctx context.Context, db *dbx.DB, c PaymentCancellation, withAuditLock bool) (hasSale bool, err error) {
	if dbutils.ExtractTx(ctx) == nil {
		return false, ErrNoTransaction
	}
	lockSQL := `NULL::text`
	args := []any{}
	if withAuditLock {
		lockSQL = `pg_advisory_xact_lock(hashtextextended(?, 0))::text`
		args = append(args, chainLockKey(ChainAuditLogs, c.MerchantID))
	}
	// Ticket de vente : utile à l'annulation de toute la commande seulement,
	// jamais lu sous le verrou d'audit (il le tiendrait plus longtemps).
	saleSQL := `FALSE`
	if !withAuditLock {
		saleSQL = `EXISTS (SELECT 1 FROM receipts r WHERE r.order_id = o.order_id AND r.total_ttc >= 0)`
	}
	args = append(args, c.OrderID, c.MerchantID)
	var state string
	var lock sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT o.state, `+lockSQL+`, `+saleSQL+`
		FROM (SELECT order_id, state FROM orders WHERE order_id = ? AND merchant_id = ? FOR UPDATE) o`, args...).
		Scan(&state, &lock, &hasSale)
	if errors.Is(err, sql.ErrNoRows) {
		return false, models.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("fiscal: lock order %s for payment cancel: %w", c.OrderID, err)
	}
	if state != "OPEN" {
		if c.PaymentID != "" {
			return false, models.ErrPaymentOrderClosed
		}
		return false, models.ErrOrderClosed
	}
	return hasSale, nil
}

// disablePayments annule, en une requête, le paiement visé ou tous les
// paiements actifs de la commande (déjà verrouillée), sauf si l'un d'eux a été
// encaissé sur un registre fermé. Les registres sont verrouillés en partage ;
// cash_register_id porte aussi des marqueurs (SCANNORDER, UBER_EATS, KIOSK...)
// ou rien : paiement pas encore rattaché à un registre, donc annulable.
// readPrev : lit aussi le dernier maillon du journal d'audit (verrou de la
// chaîne déjà tenu, pris par une requête précédente).
func disablePayments(ctx context.Context, db *dbx.DB, c PaymentCancellation, readPrev bool) ([]cancelledPaymentState, sql.NullString, error) {
	var prev sql.NullString
	targetFilter := ""
	args := []any{c.OrderID}
	if c.PaymentID != "" {
		targetFilter = ` AND payment_id = ?`
		args = append(args, c.PaymentID)
	}
	prevSQL := `NULL::text`
	if readPrev {
		prevSQL = `(` + lastAuditHashSQL + `)`
	}
	args = append(args, c.OrderID, c.MarkOrderUnpaid)
	if readPrev {
		args = append(args, c.MerchantID)
	}
	rows, err := db.QueryContext(ctx, `
		WITH target AS (
			SELECT payment_id, cash_register_id FROM payments
			WHERE order_id = ? AND enabled = TRUE`+targetFilter+`
		), regs AS (
			SELECT closed FROM cash_registers
			WHERE cash_register_id IN (
				SELECT CASE WHEN cash_register_id ~ '^[0-9]+$' THEN cash_register_id::bigint END FROM target)
			FOR SHARE
		), upd AS (
			UPDATE payments p SET enabled = FALSE
			FROM target t
			WHERE p.payment_id = t.payment_id AND p.enabled = TRUE
			  AND NOT EXISTS (SELECT 1 FROM regs WHERE closed)
			RETURNING p.payment_id, p.amount, p.mop, p.operation_type, p.payment_date, p.cash_register_id
		), unpaid AS (
			UPDATE orders SET isPaid = FALSE, last_update = `+dbx.UTCNow()+`
			WHERE order_id = ? AND ?::boolean AND EXISTS (SELECT 1 FROM upd)
		)
		SELECT EXISTS (SELECT 1 FROM regs WHERE closed), `+prevSQL+`,
		       u.payment_id, u.amount, u.mop, u.operation_type, u.payment_date, u.cash_register_id
		FROM (SELECT 1) one LEFT JOIN upd u ON TRUE`, args...)
	if err != nil {
		return nil, prev, fmt.Errorf("fiscal: cancel payments of order %s: %w", c.OrderID, err)
	}
	defer rows.Close()
	var states []cancelledPaymentState
	registerClosed := false
	for rows.Next() {
		var id, amount sql.NullInt64
		var mop, opType, register sql.NullString
		var date sql.NullTime
		if err := rows.Scan(&registerClosed, &prev, &id, &amount, &mop, &opType, &date, &register); err != nil {
			return nil, prev, fmt.Errorf("fiscal: scan cancelled payment of order %s: %w", c.OrderID, err)
		}
		if !id.Valid {
			continue
		}
		s := cancelledPaymentState{PaymentID: id.Int64, OrderID: c.OrderID, Amount: amount.Int64, MOP: mop.String, Enabled: true}
		if opType.Valid {
			s.OperationType = &opType.String
		}
		if date.Valid {
			d := FormatTime(date.Time)
			s.PaymentDate = &d
		}
		if register.Valid {
			s.CashRegisterID = &register.String
		}
		states = append(states, s)
	}
	if err := rows.Err(); err != nil {
		return nil, prev, err
	}
	if registerClosed {
		if c.PaymentID != "" {
			return nil, prev, models.ErrPaymentRegisterClosed
		}
		return nil, prev, models.ErrOrderPaymentRegisterClosed
	}
	sort.Slice(states, func(i, j int) bool { return states[i].PaymentID < states[j].PaymentID })
	return states, prev, nil
}

// cancelAuditEntries prépare une entrée PAYMENT_CANCELLED par paiement.
func cancelAuditEntries(c PaymentCancellation, states []cancelledPaymentState) ([]AuditEntry, error) {
	var reason *string
	if r := strings.TrimSpace(c.Reason); r != "" {
		reason = &r
	}
	newValues, err := json.Marshal(paymentCancelChange{Enabled: false, Source: c.Source, Reason: reason})
	if err != nil {
		return nil, err
	}
	entries := make([]AuditEntry, 0, len(states))
	for _, s := range states {
		oldValues, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		entries = append(entries, AuditEntry{
			ID:           helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix),
			MerchantID:   c.MerchantID,
			UserID:       c.UserID,
			Action:       models.ActionPaymentCancelled,
			ResourceType: models.ResourcePayment,
			ResourceID:   strconv.FormatInt(s.PaymentID, 10),
			OldValues:    oldValues,
			NewValues:    newValues,
		})
	}
	return entries, nil
}

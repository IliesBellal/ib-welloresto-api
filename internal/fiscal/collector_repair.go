package fiscal

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// CollectorRepair décrit un paiement signé dont payments.user_id a été
// réécrit après son scellement.
//
// Jusqu'à la migration 188, delivery_sessions.FinalizeDeliveredStop
// réattribuait au livreur TOUS les paiements d'une commande livrée en
// réécrivant user_id. user_id fait partie de la charge utile signée
// (PaymentPayload) : l'empreinte de ces paiements ne se recalcule plus
// (cmd/verify_fiscal : « empreinte recalculée différente »).
//
// L'ancien user_id se retrouve sans le deviner : l'empreinte est déterministe,
// il suffit d'essayer les identifiants plausibles (ceux de l'établissement)
// jusqu'à retomber sur l'empreinte stockée.
type CollectorRepair struct {
	PaymentID  string
	MerchantID string
	OrderID    string
	MOP        string
	// CurrentUserID : valeur actuelle de user_id (le livreur).
	CurrentUserID string
	// OriginalUserID : id qui redonne l'empreinte stockée. Vide = introuvable,
	// le paiement n'est alors pas modifié.
	OriginalUserID string
	// CollectedBy : à écrire dans payments.collected_by (le livreur pour un
	// paiement en main propre, vide sinon).
	CollectedBy string

	hash string
}

// sealedPayment est un paiement relu avec tout ce qui entre dans son
// empreinte.
type sealedPayment struct {
	CollectorRepair
	amount  int
	op      string
	at      time.Time
	comment *string
	prev    string
}

// sealedWith indique si l'empreinte stockée du paiement se recalcule avec
// userID comme saisisseur.
func (r sealedPayment) sealedWith(userID string) (bool, error) {
	p, err := NewPaymentPayload(r.MerchantID, r.OrderID, r.amount, r.MOP, r.op, r.at, userID, r.comment)
	if err != nil {
		return false, err
	}
	h, _, err := Seal(ChainPayments, r.prev, p)
	return h == r.hash, err
}

// FindReassignedPayments liste les paiements signés dont user_id a été
// réécrit par la livraison d'un arrêt : ils portent l'id du livreur de leur
// commande et leur empreinte ne se recalcule pas avec lui. Lecture seule.
//
// Prérequis : migration 188 (payments.collected_by).
func FindReassignedPayments(ctx context.Context, db *sql.DB) ([]CollectorRepair, error) {
	q := dbx.GetDB(ctx, db)

	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT p.payment_id::text, p.merchant_id, p.order_id::text, p.amount, COALESCE(p.mop, ''),
		       COALESCE(p.operation_type, ''), p.payment_date, COALESCE(p.user_id, ''), p.comment,
		       COALESCE(p.previous_hash, ''), p.hash
		FROM payments p
		JOIN delivery_session_order dso ON dso.order_id = p.order_id AND dso.status = 'delivered'
		JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.user_id = p.user_id
		WHERE p.hash_version >= ? AND p.hash IS NOT NULL AND p.hash <> '' AND p.signature IS NOT NULL
		  AND p.collected_by IS NULL
		ORDER BY 1`, HashVersion)
	if err != nil {
		return nil, fmt.Errorf("fiscal: find reassigned payments: %w", err)
	}
	var found []sealedPayment

	for rows.Next() {
		var r sealedPayment
		var comment sql.NullString
		if err := rows.Scan(&r.PaymentID, &r.MerchantID, &r.OrderID, &r.amount, &r.MOP, &r.op, &r.at, &r.CurrentUserID, &comment, &r.prev, &r.hash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("fiscal: find reassigned payments: %w", err)
		}
		if comment.Valid {
			r.comment = &comment.String
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fiscal: find reassigned payments: %w", err)
	}

	candidates := map[string][]string{} // par établissement
	var out []CollectorRepair
	for _, r := range found {

		// L'empreinte vaut avec l'id du livreur : c'est lui qui avait saisi le
		// paiement, rien à réparer.
		if ok, err := r.sealedWith(r.CurrentUserID); err != nil {
			return nil, fmt.Errorf("fiscal: payment %s: %w", r.PaymentID, err)
		} else if ok {
			continue
		}

		if _, done := candidates[r.MerchantID]; !done {
			c, err := collectorCandidates(ctx, q, r.MerchantID)
			if err != nil {
				return nil, err
			}
			candidates[r.MerchantID] = c
		}
		orderCreator, err := orderCreatedBy(ctx, q, r.OrderID)
		if err != nil {
			return nil, err
		}
		for _, id := range append([]string{orderCreator}, candidates[r.MerchantID]...) {
			if id == "" || id == r.CurrentUserID {
				continue
			}
			ok, err := r.sealedWith(id)
			if err != nil {
				return nil, fmt.Errorf("fiscal: payment %s: %w", r.PaymentID, err)
			}
			if ok {
				r.OriginalUserID = id
				break
			}
		}
		if models.IsHandCollectedMOP(r.MOP) {
			r.CollectedBy = r.CurrentUserID
		}
		out = append(out, r.CollectorRepair)
	}
	return out, nil
}

// ApplyCollectorRepairs remet l'ancien user_id des paiements retrouvés et
// enregistre le livreur dans collected_by pour ceux encaissés en main propre,
// dans une seule transaction. Les paiements dont l'id d'origine n'a pas été
// retrouvé ne sont pas modifiés. Chaque UPDATE est gardé par l'empreinte et
// par la valeur actuelle de user_id : rejouable, et sans effet sur une ligne
// modifiée entre-temps. Renvoie le nombre de paiements réparés.
func ApplyCollectorRepairs(ctx context.Context, db *sql.DB, repairs []CollectorRepair) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // sans effet après Commit

	fixed := 0
	for _, r := range repairs {
		if r.OriginalUserID == "" {
			continue
		}
		var collectedBy interface{}
		if r.CollectedBy != "" {
			collectedBy = r.CollectedBy
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE payments SET user_id = $1, collected_by = $2
			WHERE payment_id = $3 AND hash = $4 AND user_id = $5 AND collected_by IS NULL`,
			r.OriginalUserID, collectedBy, r.PaymentID, r.hash, r.CurrentUserID)
		if err != nil {
			return 0, fmt.Errorf("fiscal: repair payment %s: %w", r.PaymentID, err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			fixed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return fixed, nil
}

// collectorCandidates : les identifiants qui ont pu saisir un paiement de cet
// établissement — ceux déjà vus sur ses paiements, ses employés (y compris
// ceux renumérotés depuis, dont l'ancien id est dans user_id_remap) et les
// pseudo-utilisateurs de canaux (SCANNORDER, KIOSK, webhooks).
func collectorCandidates(ctx context.Context, q *dbx.DB, merchantID string) ([]string, error) {
	var hasRemap bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('user_id_remap') IS NOT NULL`).Scan(&hasRemap); err != nil {
		return nil, fmt.Errorf("fiscal: collector candidates: %w", err)
	}
	query := `
		SELECT DISTINCT c FROM (
			SELECT user_id AS c FROM payments WHERE merchant_id = ?
			UNION SELECT user_id FROM users_rights WHERE merchant_id = ?`
	args := []interface{}{merchantID, merchantID}
	if hasRemap {
		query += `
			UNION SELECT r.legacy_user_id FROM user_id_remap r
			JOIN users_rights ur ON ur.user_id = r.user_id AND ur.merchant_id = ?`
		args = append(args, merchantID)
	}
	query += `
		) t WHERE c IS NOT NULL AND c <> ''`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fiscal: collector candidates: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fiscal: collector candidates: %w", err)
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func orderCreatedBy(ctx context.Context, q *dbx.DB, orderID string) (string, error) {
	var id sql.NullString
	err := q.QueryRowContext(ctx, `SELECT created_by FROM orders WHERE order_id = ?`, orderID).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("fiscal: order creator: %w", err)
	}
	return id.String, nil
}

package customers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/database/dbx"
)

// identityWhere construit le filtre "fiches de l'établissement visées par
// l'identité" : la fiche par son ID, et/ou toutes celles qui partagent le
// téléphone ou l'email (insensible à la casse). Volontairement SANS condition
// sur enabled : une fiche supprimée reste bloquée (et débloquable).
func identityWhere(merchantID string, id blockIdentity) (string, []interface{}) {
	var ors []string
	args := []interface{}{merchantID}
	if id.CustomerID != "" {
		ors = append(ors, "customer_id = ?")
		args = append(args, id.CustomerID)
	}
	if id.Tel != "" {
		ors = append(ors, "customer_tel = ?")
		args = append(args, id.Tel)
	}
	if id.Email != "" {
		ors = append(ors, "LOWER(customer_email) = ?")
		args = append(args, strings.ToLower(id.Email))
	}
	return "merchant_id = ? AND (" + strings.Join(ors, " OR ") + ")", args
}

// FindBlockReason cherche un motif de blocage actif (deletion_reasons, object
// 'customer') et dit s'il exige un commentaire. found=false : motif inconnu,
// désactivé, ou d'un autre object (un motif d'annulation de commande n'est pas
// un motif de blocage). Même garde que le motif d'annulation de réservation.
func (r *CustomersRepository) FindBlockReason(ctx context.Context, reasonID string) (requiresComment, found bool, err error) {
	// deletion_reason_id est un integer : un ID non numérique n'existe pas,
	// sans laisser Postgres lever une erreur de type.
	if _, convErr := strconv.Atoi(strings.TrimSpace(reasonID)); convErr != nil {
		return false, false, nil
	}

	err = dbx.GetDB(ctx, r.database).QueryRowContext(ctx, `
		SELECT requires_comment
		FROM deletion_reasons
		WHERE deletion_reason_id = ?
		  AND enabled = TRUE
		  AND deletion_reason_object = ?
		LIMIT 1
	`, strings.TrimSpace(reasonID), objectBlockReason).Scan(&requiresComment)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return requiresComment, true, nil
}

// getCustomerIdentity lit le téléphone et l'email de la fiche (NULL -> ""),
// scopé à l'établissement. Retourne sql.ErrNoRows si elle n'existe pas.
func (r *CustomersRepository) getCustomerIdentity(ctx context.Context, merchantID, customerID string) (tel, email string, err error) {
	var t, e sql.NullString
	err = dbx.GetDB(ctx, r.database).QueryRowContext(ctx, `
		SELECT customer_tel, customer_email
		FROM customer
		WHERE customer_id = ? AND merchant_id = ?
	`, customerID, merchantID).Scan(&t, &e)
	if err != nil {
		return "", "", err
	}
	return t.String, e.String, nil
}

// findCustomerIDsByIdentity liste les fiches de l'établissement visées par
// l'identité (aucune condition sur enabled, voir identityWhere).
func (r *CustomersRepository) findCustomerIDsByIdentity(ctx context.Context, merchantID string, id blockIdentity) ([]string, error) {
	where, args := identityWhere(merchantID, id)
	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx,
		`SELECT customer_id FROM customer WHERE `+where+` ORDER BY customer_id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var cid int64
		if err := rows.Scan(&cid); err != nil {
			return nil, err
		}
		ids = append(ids, strconv.FormatInt(cid, 10))
	}
	return ids, rows.Err()
}

// setBlocked bloque (info != nil) ou débloque (info == nil) toutes les fiches de
// l'établissement visées par l'identité, et retourne leurs IDs. Un blocage
// ré-écrit blocked_at : le dernier motif saisi fait foi.
func (r *CustomersRepository) setBlocked(ctx context.Context, merchantID string, id blockIdentity, info *blockInfo) ([]string, error) {
	if id.isEmpty() {
		return nil, ErrBlockIdentityRequired
	}
	where, whereArgs := identityWhere(merchantID, id)

	var set string
	var args []interface{}
	if info != nil {
		set = fmt.Sprintf("blocked_at = %s, blocked_by = ?, blocked_reason_id = ?, blocked_comment = ?", dbx.UTCNow())
		args = append(args, info.By, info.ReasonID, nullIfEmpty(info.Comment))
	} else {
		set = "blocked_at = NULL, blocked_by = NULL, blocked_reason_id = NULL, blocked_comment = NULL"
	}
	args = append(args, whereArgs...)

	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx,
		`UPDATE customer SET `+set+` WHERE `+where+` RETURNING customer_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var cid int64
		if err := rows.Scan(&cid); err != nil {
			return nil, err
		}
		ids = append(ids, strconv.FormatInt(cid, 10))
	}
	return ids, rows.Err()
}

// IsPhoneBlocked dit si un client bloqué de l'établissement porte ce téléphone.
// Appelé par ScanNOrder avant de créer une commande : le téléphone est
// normalisé comme FindCustomerByPhone, et le contrôle parcourt TOUTES les
// fiches en doublon (FindCustomerByPhone n'en retient qu'une). Un téléphone ou
// un établissement vide n'est jamais bloqué.
func (r *CustomersRepository) IsPhoneBlocked(ctx context.Context, merchantID, phone string) (bool, error) {
	normalized, err := normalizeBlockPhone(phone)
	if err != nil || normalized == "" || merchantID == "" {
		return false, nil
	}

	var blocked bool
	err = dbx.GetDB(ctx, r.database).QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM customer
			WHERE merchant_id = ?
			  AND customer_tel = ?
			  AND blocked_at IS NOT NULL
		)
	`, merchantID, normalized).Scan(&blocked)
	return blocked, err
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

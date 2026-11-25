package reviewrequests

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// ListCandidates lit les commandes Wello Resto closes entre from et to, quel
// que soit le canal (ScanNOrder, caisse, borne), d'un établissement qui demande
// les avis (lien renseigné, abonnement actif), avec un client joignable par
// e-mail, pas encore considérées. Sont écartés l'e-mail d'un membre de l'équipe
// et les fiches client génériques (plus de MaxOrdersPerCustomerPerDay commandes
// en 24 h).
func (r *Repository) ListCandidates(ctx context.Context, from, to time.Time, limit int) ([]candidate, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT o.order_id::text, o.merchant_id, m.fullName, COALESCE(NULLIF(m.timezone, ''), 'Europe/Paris'),
		       TRIM(COALESCE(c.customer_first_name, '')), TRIM(c.customer_email), snos.google_review_url
		FROM orders o
		JOIN customer c ON c.customer_id = o.customer_id AND c.merchant_id = o.merchant_id
		JOIN scannorder_settings snos ON snos.merchant_id = o.merchant_id
		JOIN merchant m ON CAST(m.id AS TEXT) = o.merchant_id
		WHERE o.state = 'CLOSED'
		  AND o.brand = ?
		  AND o.delivered_on >= ? AND o.delivered_on < ?
		  AND snos.review_request_enabled = TRUE
		  AND COALESCE(snos.google_review_url, '') <> ''
		  AND TRIM(COALESCE(c.customer_email, '')) <> ''
		  -- Fiche « client » de l'établissement lui-même (comptoir, passage) : l'e-mail
		  -- est celui d'un membre de l'équipe, pas celui d'un client.
		  AND NOT EXISTS (SELECT 1 FROM users u
		                  JOIN users_rights ur ON ur.user_id = u.user_id
		                  WHERE CAST(ur.merchant_id AS TEXT) = o.merchant_id
		                    AND LOWER(TRIM(COALESCE(u.email, ''))) = LOWER(TRIM(c.customer_email)))
		  -- Fiche générique : un même client rattaché à beaucoup de commandes le même jour.
		  AND (SELECT COUNT(*) FROM orders o2
		       WHERE o2.merchant_id = o.merchant_id AND o2.customer_id = o.customer_id
		         AND o2.delivered_on >= o.delivered_on - INTERVAL '12 hours'
		         AND o2.delivered_on <= o.delivered_on + INTERVAL '12 hours') <= ?
		  AND NOT EXISTS (SELECT 1 FROM subscriptions sub
		                  WHERE sub.merchant_id = o.merchant_id
		                    AND (sub.status IN ('suspended', 'canceled') OR sub.scannorder_enabled = FALSE))
		  AND NOT EXISTS (SELECT 1 FROM review_requests rr WHERE rr.order_id = o.order_id::text)
		ORDER BY o.delivered_on
		LIMIT ?`, models.BrandWelloResto, from, to, MaxOrdersPerCustomerPerDay, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.OrderID, &c.MerchantID, &c.MerchantName, &c.Timezone, &c.FirstName, &c.Email, &c.ReviewURL); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// IsOptedOut : le client a demandé à ne plus être sollicité par cet établissement.
func (r *Repository) IsOptedOut(ctx context.Context, merchantID, emailHash string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM review_request_optouts WHERE merchant_id = ? AND email_hash = ?)`,
		merchantID, emailHash).Scan(&exists)
	return exists, err
}

// RequestedSince : une demande a été envoyée (ou est en cours) à ce client par
// cet établissement depuis since.
func (r *Repository) RequestedSince(ctx context.Context, merchantID, emailHash string, since time.Time) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM review_requests
		               WHERE merchant_id = ? AND email_hash = ? AND status IN ('pending', 'sent') AND created_at >= ?)`,
		merchantID, emailHash, since).Scan(&exists)
	return exists, err
}

// Insert enregistre la demande ; faux si la commande a déjà sa ligne (autre
// instance, passage concurrent).
func (r *Repository) Insert(ctx context.Context, row requestRow, now time.Time) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		INSERT INTO review_requests (merchant_id, order_id, email_hash, token, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (order_id) DO NOTHING`,
		row.MerchantID, row.OrderID, row.EmailHash, row.Token, row.Status, now.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Finish clôt une demande en cours : sent (avec le messageId Brevo) ou failed.
func (r *Repository) Finish(ctx context.Context, orderID, status, messageID string, sentAt *time.Time) error {
	db := dbx.GetDB(ctx, r.database)
	var msg interface{}
	if messageID != "" {
		msg = messageID
	}
	var at interface{}
	if sentAt != nil {
		at = sentAt.UTC()
	}
	_, err := db.ExecContext(ctx, `
		UPDATE review_requests SET status = ?, provider_message_id = ?, sent_at = ?
		WHERE order_id = ? AND status = 'pending'`, status, msg, at, orderID)
	return err
}

// GetByToken retrouve la demande d'un lien de désinscription. ErrNotFound si
// le jeton est inconnu.
func (r *Repository) GetByToken(ctx context.Context, token string) (*tokenInfo, error) {
	db := dbx.GetDB(ctx, r.database)
	var info tokenInfo
	err := db.QueryRowContext(ctx, `
		SELECT rr.merchant_id, COALESCE(m.fullName, ''), rr.email_hash,
		       EXISTS (SELECT 1 FROM review_request_optouts o
		               WHERE o.merchant_id = rr.merchant_id AND o.email_hash = rr.email_hash)
		FROM review_requests rr
		LEFT JOIN merchant m ON CAST(m.id AS TEXT) = rr.merchant_id
		WHERE rr.token = ?`, token).Scan(&info.MerchantID, &info.MerchantName, &info.EmailHash, &info.OptedOut)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// SetOptOut inscrit ou retire le client de la liste « ne plus recevoir » de
// l'établissement. Idempotent.
func (r *Repository) SetOptOut(ctx context.Context, merchantID, emailHash string, optedOut bool, now time.Time) error {
	db := dbx.GetDB(ctx, r.database)
	if optedOut {
		_, err := db.ExecContext(ctx, `
			INSERT INTO review_request_optouts (merchant_id, email_hash, created_at) VALUES (?, ?, ?)
			ON CONFLICT (merchant_id, email_hash) DO NOTHING`, merchantID, emailHash, now.UTC())
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM review_request_optouts WHERE merchant_id = ? AND email_hash = ?`, merchantID, emailHash)
	return err
}

// GetReviewSettings lit le nom de l'établissement et son lien d'avis (envoi de test).
func (r *Repository) GetReviewSettings(ctx context.Context, merchantID string) (name, reviewURL string, err error) {
	db := dbx.GetDB(ctx, r.database)
	err = db.QueryRowContext(ctx, `
		SELECT m.fullName, COALESCE(snos.google_review_url, '')
		FROM merchant m
		JOIN scannorder_settings snos ON snos.merchant_id = CAST(m.id AS TEXT)
		WHERE m.id = ?`, merchantID).Scan(&name, &reviewURL)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", models.ErrNotFound
	}
	return name, reviewURL, err
}

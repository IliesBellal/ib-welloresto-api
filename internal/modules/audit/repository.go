package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

type AuditRepository interface {
	InsertLog(ctx context.Context, log *models.AuditLog) error
	InsertLogWithChain(ctx context.Context, log *models.AuditLog) error
}

type auditRepository struct {
	db *sql.DB // Ou *sqlx.DB selon ce que tu utilises
}

func NewAuditRepository(db *sql.DB) AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) InsertLog(ctx context.Context, log *models.AuditLog) error {
	db := dbx.GetDB(ctx, r.db)

	query := `
		INSERT INTO audit_logs 
		(id, user_id, merchant_id, action, resource_type, resource_id, old_values, new_values, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())
	`

	// log.OldValues et log.NewValues sont des []byte (json.RawMessage),
	// le driver MySQL les passera directement comme du texte JSON.
	_, err := db.ExecContext(ctx, query,
		log.ID,
		log.UserID,
		log.MerchantID,
		log.Action,
		log.ResourceType,
		log.ResourceID,
		log.OldValues,
		log.NewValues,
	)

	if err != nil {
		logger.FromContext(ctx).Error("failed to insert audit log : " + err.Error())
		// On ne retourne pas d'erreur temporairement pour éviter de bloquer la logique métier en cas de problème avec l'audit pendant la phase de développement. À revoir pour la production.
		//return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}

func (r *auditRepository) InsertLogWithChain(ctx context.Context, log *models.AuditLog) error {
	db := dbx.GetDB(ctx, r.db)

	// 1. Récupération du hash précédent avec verrouillage (FOR UPDATE)
	// On filtre par merchant_id car le chaînage est propre à chaque client
	var prevHash sql.NullString
	err := db.QueryRowContext(ctx, `
        SELECT hash FROM audit_logs 
        WHERE merchant_id = ? 
        ORDER BY created_at DESC, id DESC LIMIT 1 
        FOR UPDATE
    `, log.MerchantID).Scan(&prevHash)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to fetch previous hash: %w", err)
	}

	// 2. Préparation des données pour le nouveau Hash
	// On utilise les valeurs déjà présentes dans l'objet log
	pHash := prevHash.String
	now := time.Now().UTC()
	createdAtStr := now.Format(time.RFC3339Nano)

	// Formule : SHA256(prevHash | createdAt | action | resourceID | newValues)
	payload := fmt.Sprintf("%s|%s|%s|%s|%s",
		pHash,
		createdAtStr,
		log.Action,
		log.ResourceID,
		string(log.NewValues),
	)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))

	// 3. Insertion finale
	query := `
        INSERT INTO audit_logs 
        (id, user_id, merchant_id, action, resource_type, resource_id, old_values, new_values, previous_hash, hash, created_at) 
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `

	_, err = db.ExecContext(ctx, query,
		log.ID,
		log.UserID,
		log.MerchantID,
		log.Action,
		log.ResourceType,
		log.ResourceID,
		log.OldValues,
		log.NewValues,
		pHash,
		newHash,
		now,
	)

	if err != nil {
		logger.FromContext(ctx).Error("failed to insert chained audit log: " + err.Error())
		return err
	}

	return nil
}

package audit

import (
	"context"
	"database/sql"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type AuditRepository interface {
	InsertLog(ctx context.Context, log *models.AuditLog) error
}

type auditRepository struct {
	db *sql.DB // Ou *sqlx.DB selon ce que tu utilises
}

func NewAuditRepository(db *sql.DB) AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) InsertLog(ctx context.Context, log *models.AuditLog) error {
	db := dbutils.GetDB(ctx, r.db)

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

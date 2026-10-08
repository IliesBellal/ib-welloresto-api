package audit

import (
	"context"
	"database/sql"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
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

// InsertLogWithChain ouvre sa propre transaction (ou rejoint celle de
// l'appelant) : le journal est écrit depuis de nombreux modules, souvent hors
// transaction, et le verrou fiscal n'a d'effet que dans une transaction
// (solution C du lot A conformité caisse).
func (r *auditRepository) InsertLogWithChain(ctx context.Context, log *models.AuditLog) error {
	return dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		return r.insertLogWithChain(txCtx, log)
	})
}

// insertLogWithChain : empreinte v2 signée, état avant et utilisateur compris
// (l'ancienne formule ne couvrait que l'action, la ressource et l'état après,
// sans clé) — voir fiscal.AppendAuditLog.
func (r *auditRepository) insertLogWithChain(ctx context.Context, log *models.AuditLog) error {
	err := fiscal.AppendAuditLog(ctx, dbx.GetDB(ctx, r.db), fiscal.AuditEntry{
		ID:           log.ID,
		MerchantID:   log.MerchantID,
		UserID:       log.UserID,
		Action:       log.Action,
		ResourceType: log.ResourceType,
		ResourceID:   log.ResourceID,
		OldValues:    log.OldValues,
		NewValues:    log.NewValues,
	})
	if err != nil {
		logger.FromContext(ctx).Error("failed to insert chained audit log: " + err.Error())
		return err
	}
	return nil
}

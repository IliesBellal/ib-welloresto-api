package audit

import (
	"context"
	"database/sql"
	"fmt"
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

func (r *auditRepository) insertLogWithChain(ctx context.Context, log *models.AuditLog) error {
	db := dbx.GetDB(ctx, r.db)

	// 1. Verrou de la chaîne audit_logs de l'établissement, puis dernière entrée de son journal
	// (le chaînage est propre à chaque établissement).
	if err := fiscal.LockChain(ctx, fiscal.ChainAuditLogs, log.MerchantID); err != nil {
		return err
	}
	var prevHash sql.NullString
	err := db.QueryRowContext(ctx, `
        SELECT hash FROM audit_logs
        WHERE merchant_id = ?
        ORDER BY created_at DESC, id DESC LIMIT 1
    `, log.MerchantID).Scan(&prevHash)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to fetch previous hash: %w", err)
	}

	// 2. Empreinte v2 signée : état avant compris, et utilisateur (l'ancienne
	// formule ne couvrait que l'action, la ressource et l'état après, sans clé).
	pHash := fiscal.PrevOrGenesis(prevHash.String)
	now := fiscal.Now()
	payload, err := fiscal.NewAuditLogPayload(log.ID, log.MerchantID, log.UserID, log.Action, log.ResourceType, log.ResourceID,
		now, log.OldValues, log.NewValues)
	if err != nil {
		return err
	}
	newHash, signature, err := fiscal.Seal(fiscal.ChainAuditLogs, pHash, payload)
	if err != nil {
		return err
	}

	// 3. Insertion finale
	query := `
        INSERT INTO audit_logs
        (id, user_id, merchant_id, action, resource_type, resource_id, old_values, new_values, previous_hash, hash, signature, hash_version, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		signature,
		fiscal.HashVersion,
		now,
	)

	if err != nil {
		logger.FromContext(ctx).Error("failed to insert chained audit log: " + err.Error())
		return err
	}

	return nil
}

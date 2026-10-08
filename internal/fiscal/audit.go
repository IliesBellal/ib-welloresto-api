package fiscal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// AuditEntry est une entrée du journal d'audit avant scellement. OldValues et
// NewValues sont du JSON (nil vaut null).
type AuditEntry struct {
	ID           string
	MerchantID   string
	UserID       string
	Action       string
	ResourceType string
	ResourceID   string
	OldValues    []byte
	NewValues    []byte
}

// AppendAuditLog écrit une entrée chaînée et signée (v2) au journal d'audit de
// l'établissement. À appeler dans une transaction : le verrou de la chaîne
// audit_logs est tenu jusqu'au commit (ErrNoTransaction sinon). Partagée par
// le module audit et les écritures fiscales qui journalisent elles-mêmes
// (annulation de paiement, lot C conformité caisse), que le module audit ne
// peut pas servir sans cycle d'import.
func AppendAuditLog(ctx context.Context, db *dbx.DB, e AuditEntry) error {
	return AppendAuditLogs(ctx, db, e.MerchantID, []AuditEntry{e})
}

// AppendAuditLogs écrit plusieurs entrées d'un même établissement, chaînées
// dans l'ordre donné : un verrou, une lecture du dernier maillon et une
// insertion, quel que soit leur nombre.
func AppendAuditLogs(ctx context.Context, db *dbx.DB, merchantID string, entries []AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}
	// 1. Verrou de la chaîne audit_logs de l'établissement, puis dernière
	// entrée de son journal (le chaînage est propre à chaque établissement).
	if err := LockChain(ctx, ChainAuditLogs, merchantID); err != nil {
		return err
	}
	var prevHash sql.NullString
	err := db.QueryRowContext(ctx, lastAuditHashSQL, merchantID).Scan(&prevHash)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to fetch previous hash: %w", err)
	}
	return insertAuditEntries(ctx, db, prevHash.String, entries)
}

// lastAuditHashSQL lit le dernier maillon du journal d'un établissement. Ne
// le lire qu'avec le verrou de la chaîne déjà tenu, dans une requête lancée
// après l'avoir pris : sinon, un maillon écrit entre-temps serait ignoré.
const lastAuditHashSQL = `
        SELECT hash FROM audit_logs
        WHERE merchant_id = ?
        ORDER BY created_at DESC, id DESC LIMIT 1`

// insertAuditEntries scelle et insère des entrées (empreinte v2 signée, état
// avant et utilisateur compris) à la suite de prev, en une requête. Les
// entrées d'un même lot sont datées à une microseconde d'écart, pour que
// l'ordre (created_at, id) du dernier maillon suive celui de la chaîne.
func insertAuditEntries(ctx context.Context, db *dbx.DB, prev string, entries []AuditEntry) error {
	pHash := PrevOrGenesis(prev)
	now := Now()
	args := make([]any, 0, len(entries)*13)
	for i, e := range entries {
		at := now.Add(time.Duration(i) * time.Microsecond)
		payload, err := NewAuditLogPayload(e.ID, e.MerchantID, e.UserID, e.Action, e.ResourceType, e.ResourceID,
			at, e.OldValues, e.NewValues)
		if err != nil {
			return err
		}
		newHash, signature, err := Seal(ChainAuditLogs, pHash, payload)
		if err != nil {
			return err
		}
		args = append(args, e.ID, e.UserID, e.MerchantID, e.Action, e.ResourceType, e.ResourceID, e.OldValues, e.NewValues,
			pHash, newHash, signature, HashVersion, at)
		pHash = newHash
	}
	_, err := db.ExecContext(ctx, `
        INSERT INTO audit_logs
        (id, user_id, merchant_id, action, resource_type, resource_id, old_values, new_values, previous_hash, hash, signature, hash_version, created_at)
        VALUES `+strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?), ", len(entries)), ", "), args...)
	return err
}

//go:build postgres_integration

package audit

import (
	"context"
	"encoding/json"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestInsertLogWithChain_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-audit-m1"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM audit_logs WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewAuditRepository(db)

	mk := func(id, action string) *models.AuditLog {
		return &models.AuditLog{
			ID:           id,
			MerchantID:   merchantID,
			Action:       action,
			ResourceType: "itest",
			ResourceID:   "res-1",
			OldValues:    json.RawMessage(`{"before": 1}`),
			NewValues:    json.RawMessage(`{"after": 2}`),
		}
	}

	if err := repo.InsertLogWithChain(ctx, mk("itest-audit-1", "create")); err != nil {
		t.Fatalf("InsertLogWithChain (1st) failed against postgres: %v", err)
	}
	if err := repo.InsertLogWithChain(ctx, mk("itest-audit-2", "update")); err != nil {
		t.Fatalf("InsertLogWithChain (2nd) failed against postgres: %v", err)
	}

	// Le second log doit chaîner sur le hash du premier (SELECT ... FOR UPDATE)
	var firstHash, secondPrev string
	if err := db.QueryRowContext(ctx,
		`SELECT hash FROM audit_logs WHERE id = $1`, "itest-audit-1").Scan(&firstHash); err != nil {
		t.Fatalf("read first: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT previous_hash FROM audit_logs WHERE id = $1`, "itest-audit-2").Scan(&secondPrev); err != nil {
		t.Fatalf("read second: %v", err)
	}
	if firstHash == "" || firstHash != secondPrev {
		t.Fatalf("hash chain broken: first.hash=%q second.previous_hash=%q", firstHash, secondPrev)
	}
}

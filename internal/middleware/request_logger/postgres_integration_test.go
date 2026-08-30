//go:build postgres_integration

package requestlogger

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"

	"go.uber.org/zap"
)

// Couvre la régression corrigée lors de la réactivation du middleware pour
// Postgres : flush() construisait ses requêtes avec des placeholders `?`
// (syntaxe MySQL, invalide pour pgx) et MerchantID était typé *int64 alors
// que api_request_logs.merchant_id est varchar(64).
func TestLogger_Flush_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const ip = "203.0.113.10" // TEST-NET-3 (RFC 5737), ne collera jamais à une IP réelle

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM api_request_logs WHERE ip = $1`, ip)
	}
	cleanup()
	t.Cleanup(cleanup)

	l := &Logger{db: db, log: zap.NewNop()}

	merchantID := "itest-reqlog-m1"
	userID := int64(42)

	l.flush([]LogEntry{
		{
			UserID:          &userID,
			MerchantID:      &merchantID,
			Method:          "GET",
			URL:             "/test/postgres-rebind",
			Payload:         []byte(`{"ok":true}`),
			ResponsePayload: []byte(`{"non_json_body_bytes":12}`),
			StatusCode:      200,
			IP:              ip,
		},
	})

	var gotMerchantID, gotResponsePayload string
	err := db.QueryRowContext(ctx,
		`SELECT merchant_id, response_payload FROM api_request_logs WHERE ip = $1`, ip,
	).Scan(&gotMerchantID, &gotResponsePayload)
	if err != nil {
		t.Fatalf("flush() did not insert the expected row (placeholder rebind or type mismatch likely broken): %v", err)
	}
	if gotMerchantID != merchantID {
		t.Fatalf("merchant_id = %q, want %q", gotMerchantID, merchantID)
	}
	if gotResponsePayload != `{"non_json_body_bytes":12}` {
		t.Fatalf("response_payload = %q, want %q", gotResponsePayload, `{"non_json_body_bytes":12}`)
	}
}

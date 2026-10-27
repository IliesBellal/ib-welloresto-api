//go:build postgres_integration

package kiosk

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestRecordEvents_Postgres : POST /kiosk/events écrit dans kiosk_events
// (migration 195) — borne et établissement du jeton, session en uuid, champs
// facultatifs à NULL, événement invalide ignoré seul.
func TestRecordEvents_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID, kioskID = "merch-itest-audience", "kiosk-itest-audience"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_events WHERE kiosk_id = $1`, kioskID)
	})

	svc := &Service{repo: NewRepository(db)}
	elapsed, amount := int64(64000), int64(1890)
	err := svc.RecordEvents(ctx, &AuthenticatedKiosk{KioskID: kioskID, MerchantID: merchantID}, KioskEventsRequest{Events: []KioskEvent{
		{Type: EventSessionStart, SessionID: "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35"},
		{Type: EventProductView, SessionID: "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35", ProductID: "42"},
		{Type: "unknown", SessionID: "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35"},
		{Type: EventSessionAbandoned, SessionID: "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35", ElapsedMs: &elapsed, AmountCents: &amount, Detail: "inactivity", Step: "cart"},
	}})
	if err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT event_type, merchant_id, CAST(session_id AS text), elapsed_ms, product_id, amount_cents, detail, step
		FROM kiosk_events WHERE kiosk_id = $1 ORDER BY id`, kioskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		typ, merchant, session string
		elapsed, amount        *int64
		product, detail, step  *string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.typ, &r.merchant, &r.session, &r.elapsed, &r.product, &r.amount, &r.detail, &r.step); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3 (unknown type ignored)", len(got))
	}
	if got[0].typ != EventSessionStart || got[0].merchant != merchantID || got[0].session != "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35" ||
		got[0].elapsed != nil || got[0].product != nil || got[0].detail != nil {
		t.Errorf("session_start = %+v", got[0])
	}
	if got[1].product == nil || *got[1].product != "42" {
		t.Errorf("product_view = %+v", got[1])
	}
	a := got[2]
	if a.elapsed == nil || *a.elapsed != 64000 || a.amount == nil || *a.amount != 1890 ||
		a.detail == nil || *a.detail != "inactivity" || a.step == nil || *a.step != "cart" {
		t.Errorf("session_abandoned = %+v", a)
	}
}

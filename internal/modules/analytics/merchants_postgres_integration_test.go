//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"
)

// TestGetAccessibleMerchants_Postgres is PROMPT 24 Phase 1's coverage for the
// new GET /analytics/merchants endpoint: the caller must see exactly the
// establishments where they hold permission.POSAnalytics, named, and nothing
// else — not an establishment where they hold a different permission
// (reports.sales.read) but not pos.analytics, and not an establishment they
// have no users_rights link to at all.
func TestGetAccessibleMerchants_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(repo, nil)

	merchantA := seedNamedMerchant(t, ctx, db, "ITest Bistro Nord")
	merchantB := seedNamedMerchant(t, ctx, db, "ITest Bistro Sud")
	merchantC := seedNamedMerchant(t, ctx, db, "ITest Bistro Est")
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id IN ($1, $2, $3)`, merchantA, merchantB, merchantC)
		_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id IN ($1, $2, $3)`, merchantA, merchantB, merchantC)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id IN ($1, $2, $3)`, merchantA, merchantB, merchantC)
	}()

	const userID = "itest-accessible-merchants-user"

	// Merchant A: role carries pos.analytics -> accessible.
	roleA := seedScopeRole(t, ctx, db, merchantA, "itest-am-role-a", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-am-tok-a", roleID: &roleA, enabled: true, loginEnabled: true})

	// Merchant B: role carries reports.sales.read only, NOT pos.analytics ->
	// must be excluded even though the caller can read sales figures there.
	roleB := seedScopeRole(t, ctx, db, merchantB, "itest-am-role-b", []permission.Key{permission.ReportsSalesRead})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantB, token: "itest-am-tok-b", roleID: &roleB, enabled: true, loginEnabled: true})

	// Merchant C: no users_rights link at all for this user -> excluded.

	user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantA}
	userCtx := middleware.WithUser(ctx, user)

	resp, err := svc.GetAccessibleMerchants(userCtx)
	if err != nil {
		t.Fatalf("GetAccessibleMerchants: %v", err)
	}
	if len(resp.Merchants) != 1 {
		t.Fatalf("expected exactly 1 accessible merchant, got %d: %+v", len(resp.Merchants), resp.Merchants)
	}
	if resp.Merchants[0].MerchantID != merchantA || resp.Merchants[0].Name != "ITest Bistro Nord" {
		t.Fatalf("expected merchant A named 'ITest Bistro Nord', got %+v", resp.Merchants[0])
	}

	// A user with zero pos.analytics links anywhere gets an empty list, not
	// an error and not a nil-vs-empty-array ambiguity on the wire.
	otherUser := &auth.UserLoginRow{UserID: "itest-accessible-merchants-nobody", MerchantID: merchantA}
	otherCtx := middleware.WithUser(ctx, otherUser)
	emptyResp, err := svc.GetAccessibleMerchants(otherCtx)
	if err != nil {
		t.Fatalf("GetAccessibleMerchants (no access): %v", err)
	}
	if len(emptyResp.Merchants) != 0 {
		t.Fatalf("expected 0 accessible merchants, got %+v", emptyResp.Merchants)
	}
}

func seedNamedMerchant(t *testing.T, ctx context.Context, db *sql.DB, name string) string {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', 'sc-'||substr(gen_random_uuid()::text, 1, 8), 'https://example.com', '0600000000', 'mt-'||substr(gen_random_uuid()::text, 1, 8), 'Europe/Paris', 1.0, 2.0)
		RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("seed named merchant %q: %v", name, err)
	}
	return itoa(id)
}

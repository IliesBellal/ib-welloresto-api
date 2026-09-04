//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle (PROMPT 11, §2) que chaque chemin d'annulation écrit la
// bonne valeur de orders.cancelled_by_type — pas une recherche de texte, une
// commande réellement annulée par chemin, valeur relue en base. DenyOrderLocal
// et DeleteOrderLocal sont les deux points de choke où convergent tous les
// chemins internes (POS staff, ScanNOrder, kiosk, cron d'expiration, webhook
// Stripe) : classifyCancelledByType (cancellation.go) fait toute la
// dérivation depuis le userID déjà threadé par chacun de ces appelants.
func TestOrderLifeCycleRepository_CancelledByType_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-cbt' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC CBT', 'a', '1', 's', '75001', 'Paris', 'siret-olc-cbt', 'https://x', '06', 'mtok-olc-cbt', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	newOpenOrder := func(t *testing.T, orderNum int) string {
		t.Helper()
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'OPEN', 1000, 0, 1000, 'itest')
			RETURNING order_id`, merchantID, orderNum).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(orderID, 10)
	}

	readCancelledByType := func(t *testing.T, orderID string) sql.NullString {
		t.Helper()
		var v sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT cancelled_by_type FROM orders WHERE order_id = $1`, orderID).Scan(&v); err != nil {
			t.Fatalf("read back cancelled_by_type: %v", err)
		}
		return v
	}

	tests := []struct {
		name       string
		userID     string
		wantValue  string // "" means expect SQL NULL
		wantIsNull bool
	}{
		{name: "staff (real numeric id) via DenyOrderLocal", userID: "226", wantValue: "STAFF"},
		{name: "SYSTEM (cron expiry) via DeleteOrderLocal", userID: "SYSTEM", wantValue: "SYSTEM"},
		{name: "Stripe webhook via DeleteOrderLocal", userID: models.StripeWebhookUserID, wantValue: "SYSTEM"},
		{name: "kiosk self-service via DeleteOrderLocal", userID: "KIOSK", wantValue: "CUSTOMER"},
		{name: "ScanNOrder self-service via DeleteOrderLocal", userID: "SNO_CUSTOMER", wantValue: "CUSTOMER"},
		{name: "Uber Eats webhook via DenyOrderLocal", userID: models.UberEatsWebhookUserID, wantValue: "PLATFORM"},
		{name: "Deliveroo webhook via DenyOrderLocal", userID: models.DeliverooWebhookUserID, wantValue: "PLATFORM"},
		{name: "empty userID left NULL, not guessed", userID: "", wantIsNull: true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderID := newOpenOrder(t, 1000+i)
			if err := repo.DenyOrderLocal(ctx, orderID, "1", "itest deny", tt.userID); err != nil {
				t.Fatalf("DenyOrderLocal: %v", err)
			}
			got := readCancelledByType(t, orderID)
			if tt.wantIsNull {
				if got.Valid {
					t.Fatalf("DenyOrderLocal(userID=%q): expected NULL, got %q", tt.userID, got.String)
				}
				return
			}
			if !got.Valid || got.String != tt.wantValue {
				t.Fatalf("DenyOrderLocal(userID=%q): expected cancelled_by_type=%q, got %v", tt.userID, tt.wantValue, got)
			}
		})
	}

	// DeleteOrderLocal is the second choke point (ScanNOrder/kiosk self-cancel,
	// Deliveroo-triggered internal cancel path all converge here per
	// docs/decisions.md's C2 recensement) — verify it independently of
	// DenyOrderLocal since it's a distinct write site with its own SET clause.
	t.Run("DeleteOrderLocal writes the same classification", func(t *testing.T) {
		orderID := newOpenOrder(t, 2000)
		if err := repo.DeleteOrderLocal(ctx, orderID, "1", "itest delete", "KIOSK"); err != nil {
			t.Fatalf("DeleteOrderLocal: %v", err)
		}
		got := readCancelledByType(t, orderID)
		if !got.Valid || got.String != "CUSTOMER" {
			t.Fatalf("DeleteOrderLocal(userID=KIOSK): expected cancelled_by_type=CUSTOMER, got %v", got)
		}
	})
}

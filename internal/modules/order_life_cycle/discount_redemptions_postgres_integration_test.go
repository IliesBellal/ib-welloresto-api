//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérifie que discount_redemptions (PROMPT 21 Phase 3, table de liaison
// commande×remise) est tenue à jour par upsertOrderItemDiscountRedemption —
// appelée par InsertOrderItem (création) et par la boucle de UpsertOrder
// (édition d'une commande ouverte) — pas seulement par la reprise historique
// (migration 119, is_reconstructed=true).
//
// Teste la fonction directement plutôt qu'en passant par CreateOrder/
// UpdateOrder au complet : les deux dépendent d'un schéma orders bien plus
// large (brand_store_id, migration 111) dont l'état sur le Postgres pointé
// par ce test (staging, via POSTGRES_URL) est hors du périmètre de ce lot —
// seul le comportement de la fonction elle-même, sur une orders/orderitems
// minimale, est sous test ici.
func TestDiscountRedemptions_UpsertOrderItemDiscountRedemption_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM discount_redemptions WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM discounts_products WHERE discount_id_new IN (SELECT discount_id_new FROM discounts WHERE merchant_id = $1)`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}

	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-discred' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest DiscRed', 'a', '1', 's', '75001', 'Paris', 'siret-discred', 'https://x', '06', 'mtok-discred', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	// discount_id_new s'auto-attribue via la séquence posée par la migration
	// 118 — discount_id (varchar) reste requis (transition, PRIMARY KEY
	// encore en place), une valeur de test suffit.
	var discountIDNew int
	if err := db.QueryRowContext(ctx, `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_value, discount_unit, valid_from, discounted_quantity, is_cumulative, is_time_limited, available, enabled)
		VALUES ('itest-discred-1', $1, 'ITest Discount', 'd', 20, 'PERCENTAGE', now(), 1, false, false, true, true)
		RETURNING discount_id_new`, merchantID).Scan(&discountIDNew); err != nil {
		t.Fatalf("seed discount: %v", err)
	}
	discountIDStr := strconv.Itoa(discountIDNew)

	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, tva, ht, price, created_by)
		VALUES ($1, 1, 'ACCEPTED', 0, 800, 800, 'itest-discred-user')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	var orderItemIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price)
		VALUES ($1, 1, $2, 1, 1000, 800)
		RETURNING order_item_id`, orderID, merchantID).Scan(&orderItemIntID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}
	orderItemID := strconv.FormatInt(orderItemIntID, 10)

	repo := NewOrdersLifeCycleRepository(db, nil)

	// --- 1. Remise appliquée : la ligne de liaison est créée ---
	repo.upsertOrderItemDiscountRedemption(ctx, orderID, orderItemID, merchantID, &discountIDStr, 1000, 800)

	var scope string
	var gotDiscountID, gotAmount int
	var isReconstructed bool
	if err := db.QueryRowContext(ctx, `
		SELECT scope, discount_id, amount_applied_cents, is_reconstructed
		FROM discount_redemptions WHERE order_item_id = $1`, orderItemID).
		Scan(&scope, &gotDiscountID, &gotAmount, &isReconstructed); err != nil {
		t.Fatalf("expected a discount_redemptions row for order_item_id=%s: %v", orderItemID, err)
	}
	if scope != "PRODUCT_LINE" || gotDiscountID != discountIDNew || gotAmount != 200 || isReconstructed {
		t.Fatalf("unexpected discount_redemptions row: scope=%s discount_id=%d amount=%d is_reconstructed=%v (want PRODUCT_LINE/%d/200/false)",
			scope, gotDiscountID, gotAmount, isReconstructed, discountIDNew)
	}

	// --- 2. Même ligne, remise modifiée (25€ -> 10€ au lieu de 20€ de remise) :
	// upsert, pas de doublon, montant mis à jour ---
	repo.upsertOrderItemDiscountRedemption(ctx, orderID, orderItemID, merchantID, &discountIDStr, 1000, 900)

	var count, gotAmount2 int
	if err := db.QueryRowContext(ctx, `SELECT count(*), max(amount_applied_cents) FROM discount_redemptions WHERE order_item_id = $1`, orderItemID).Scan(&count, &gotAmount2); err != nil {
		t.Fatalf("re-check after amount change: %v", err)
	}
	if count != 1 || gotAmount2 != 100 {
		t.Fatalf("expected 1 row with amount_applied_cents=100 after re-application, got count=%d amount=%d", count, gotAmount2)
	}

	// --- 3. Remise retirée sur une commande encore ouverte : la ligne disparaît ---
	// (cas explicitement cité par le brief comme le plus facile à manquer)
	repo.upsertOrderItemDiscountRedemption(ctx, orderID, orderItemID, merchantID, nil, 1000, 1000)

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM discount_redemptions WHERE order_item_id = $1`, orderItemID).Scan(&remaining); err != nil {
		t.Fatalf("count discount_redemptions after removal: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected discount_redemptions row deleted after discount removed on open order, got %d remaining", remaining)
	}
}

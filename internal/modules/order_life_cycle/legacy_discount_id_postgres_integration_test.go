//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Hotfix 2026-10-19 : le pricing renvoie encore discounts.discount_id
// (varchar, "discount-<uuid>" pour les promotions récentes) et le POS le
// renvoie tel quel à la création/édition de commande, où il était écrit dans
// orderitems.discount_id (integer) -> 22P02. resolveLegacyDiscountIDs le
// remplace par discount_id_new avant toute écriture.
//
// Le test va jusqu'à l'INSERT réel dans orderitems (le crash de prod) puis
// jusqu'à discount_redemptions, sans passer par CreateOrder au complet (même
// raison que discount_redemptions_postgres_integration_test.go).
func TestResolveLegacyDiscountIDs_Postgres(t *testing.T) {
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
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}

	seedMerchant := func(siret string) string {
		var oldID int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
			cleanupFor(strconv.FormatInt(oldID, 10))
		}
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
			VALUES ('ITest LegacyDisc', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'UTC')
			RETURNING id`, siret).Scan(&id); err != nil {
			t.Fatalf("seed merchant %s: %v", siret, err)
		}
		mid := strconv.FormatInt(id, 10)
		t.Cleanup(func() { cleanupFor(mid) })
		return mid
	}

	seedDiscount := func(legacyID, merchantID string) int64 {
		var idNew int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_value, discount_unit, valid_from, discounted_quantity, is_cumulative, is_time_limited, available, enabled)
			VALUES ($1, $2, 'ITest Legacy', 'd', 20, 'PERCENTAGE', now(), 1, false, false, true, true)
			RETURNING discount_id_new`, legacyID, merchantID).Scan(&idNew); err != nil {
			t.Fatalf("seed discount %s: %v", legacyID, err)
		}
		return idNew
	}

	merchantID := seedMerchant("siret-legacydisc")
	otherMerchantID := seedMerchant("siret-legacydisc-o")

	legacyID := "discount-itest-legacydisc-4228b038"
	discountIDNew := seedDiscount(legacyID, merchantID)
	otherLegacyID := "discount-itest-legacydisc-other"
	seedDiscount(otherLegacyID, otherMerchantID)

	str := func(s string) *string { return &s }
	numericID := "424242"
	products := []models.OrderProductPayload{
		{ProductID: "1", DiscountID: str(legacyID)},        // 0. legacy -> discount_id_new
		{ProductID: "2", DiscountID: str(legacyID)},        // 1. même remise sur 2 lignes
		{ProductID: "3", DiscountID: &numericID},           // 2. déjà numérique : jamais touché
		{ProductID: "4", DiscountID: nil},                  // 3. sans remise
		{ProductID: "5", DiscountID: str("discount-nope")}, // 4. introuvable -> nil
		{ProductID: "6", DiscountID: str(otherLegacyID)},   // 5. autre merchant -> nil
		{ProductID: "7", DiscountID: str("")},              // 6. vide -> nil
	}

	repo := NewOrdersLifeCycleRepository(db, nil)
	if err := repo.resolveLegacyDiscountIDs(ctx, merchantID, products); err != nil {
		t.Fatalf("resolveLegacyDiscountIDs: %v", err)
	}

	want := strconv.FormatInt(discountIDNew, 10)
	for _, i := range []int{0, 1} {
		if products[i].DiscountID == nil || *products[i].DiscountID != want {
			t.Fatalf("product %d: expected discount_id %s, got %v", i, want, products[i].DiscountID)
		}
	}
	if products[2].DiscountID != &numericID {
		t.Fatalf("numeric discount_id must be left untouched, got %v", products[2].DiscountID)
	}
	for _, i := range []int{3, 4, 5, 6} {
		if products[i].DiscountID != nil {
			t.Fatalf("product %d: expected nil discount_id, got %q", i, *products[i].DiscountID)
		}
	}

	// Le crash de prod : l'INSERT dans orderitems passe désormais, et la
	// remise est attribuée dans discount_redemptions.
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, tva, ht, price, created_by)
		VALUES ($1, 1, 'ACCEPTED', 0, 800, 800, 'itest-legacydisc-user')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	price := 800
	oid, err := repo.InsertOrderItem(ctx, &models.OrderItemInsert{
		OrderID:         orderID,
		ProductID:       "1",
		MerchantID:      merchantID,
		Quantity:        1,
		DiscountID:      products[0].DiscountID,
		Price:           price,
		BasePrice:       1000,
		DiscountedPrice: &price,
		CreatedBy:       "itest-legacydisc-user",
	})
	if err != nil {
		t.Fatalf("InsertOrderItem with resolved discount_id: %v", err)
	}

	var gotOrderItemDiscount, gotRedemptionDiscount int64
	if err := db.QueryRowContext(ctx, `SELECT discount_id FROM orderitems WHERE order_item_id = $1`, oid).Scan(&gotOrderItemDiscount); err != nil {
		t.Fatalf("read orderitems.discount_id: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT discount_id FROM discount_redemptions WHERE order_item_id = $1`, oid).Scan(&gotRedemptionDiscount); err != nil {
		t.Fatalf("expected a discount_redemptions row: %v", err)
	}
	if gotOrderItemDiscount != discountIDNew || gotRedemptionDiscount != discountIDNew {
		t.Fatalf("expected discount_id %d on orderitems and discount_redemptions, got %d / %d", discountIDNew, gotOrderItemDiscount, gotRedemptionDiscount)
	}
}

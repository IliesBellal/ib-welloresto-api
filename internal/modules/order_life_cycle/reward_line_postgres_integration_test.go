//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Récompenses « produit offert » : orderitems.discount_id ne reçoit plus que
// des remises du marchand, orderitems.reward_id les récompenses
// (docs/ids-entiers-users-discounts-chantier.md, §9).
//
// Prérequis : migration 189 (orderitems.reward_id). Le test va jusqu'à
// l'INSERT réel dans orderitems puis discount_redemptions, comme
// legacy_discount_id_postgres_integration_test.go.
func TestResolveLineDiscounts_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var hasColumn bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'orderitems' AND column_name = 'reward_id')`).Scan(&hasColumn); err != nil {
		t.Fatalf("check migration 189: %v", err)
	}
	if !hasColumn {
		t.Skip("migration 189 (orderitems.reward_id) non appliquée sur cette base")
	}

	// Identifiants fixes et très hauts : une récompense et une remise peuvent
	// porter le même numéro, il faut donc les maîtriser. Hors de portée des
	// séquences de la base de test.
	const (
		rewardOK        = 987001 // free_product consommée par la commande
		rewardOld       = 987002 // free_product, POS pas à jour (discount_id)
		rewardFixed     = 987004 // fixed_discount : jamais une ligne
		rewardCollision = 987003 // aussi une remise du marchand
		rewardUpdate    = 987005 // free_product rattachée à la commande (modification)
		discountMine    = 987010
		discountOther   = 987011
	)
	rewardIDs := []int{rewardOK, rewardOld, rewardFixed, rewardCollision, rewardUpdate}

	cleanupFor := func(mid string) {
		for _, q := range []string{
			`DELETE FROM customer_rewards WHERE reward_id = ANY($1)`,
		} {
			_, _ = db.ExecContext(ctx, q, rewardIDs)
		}
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
			VALUES ('ITest RewardLine', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'UTC')
			RETURNING id`, siret).Scan(&id); err != nil {
			t.Fatalf("seed merchant %s: %v", siret, err)
		}
		mid := strconv.FormatInt(id, 10)
		t.Cleanup(func() { cleanupFor(mid) })
		return mid
	}
	seedDiscount := func(idNew int, merchantID string) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO discounts (discount_id, discount_id_new, merchant_id, discount_name, discount_desc, discount_value, discount_unit, valid_from, discounted_quantity, is_cumulative, is_time_limited, available, enabled)
			VALUES ($1, $2, $3, 'ITest Reward', 'd', 20, 'PERCENTAGE', now(), 1, false, false, true, true)`,
			strconv.Itoa(idNew), idNew, merchantID); err != nil {
			t.Fatalf("seed discount %d: %v", idNew, err)
		}
	}
	seedReward := func(id int, rewardType string, usedOnOrder *string) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO customer_rewards (reward_id, customer_id, loyalty_program_id, reward_type, reward_value, creation_date, used_on_order_id)
			OVERRIDING SYSTEM VALUE
			VALUES ($1, 'itest-rewardline', 'itest-lp', $2, 500, now(), $3)`, id, rewardType, usedOnOrder); err != nil {
			t.Fatalf("seed reward %d: %v", id, err)
		}
	}

	merchantID := seedMerchant("siret-rewardline")
	otherMerchantID := seedMerchant("siret-rewardline-o")
	seedDiscount(discountMine, merchantID)
	seedDiscount(discountOther, otherMerchantID)
	seedDiscount(rewardCollision, merchantID)

	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, tva, ht, price, created_by)
		VALUES ($1, 1, 'ACCEPTED', 0, 800, 800, 'itest-rewardline-user')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	seedReward(rewardOK, "free_product", nil)
	seedReward(rewardOld, "free_product", nil)
	seedReward(rewardFixed, "fixed_discount", nil)
	seedReward(rewardCollision, "free_product", nil)
	seedReward(rewardUpdate, "free_product", &orderID)

	str := func(s string) *string { return &s }
	id := func(n int) *string { return str(strconv.Itoa(n)) }
	used := func(ns ...int) []*models.UsedReward {
		out := make([]*models.UsedReward, 0, len(ns))
		for _, n := range ns {
			out = append(out, &models.UsedReward{RewardID: strconv.Itoa(n)})
		}
		return out
	}
	repo := NewOrdersLifeCycleRepository(db, nil)

	type want struct{ discount, reward *string }
	check := func(label string, p models.OrderProductPayload, w want) {
		t.Helper()
		eq := func(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
		if !eq(p.DiscountID, w.discount) || !eq(p.RewardID, w.reward) {
			dump := func(s *string) string {
				if s == nil {
					return "nil"
				}
				return *s
			}
			t.Fatalf("%s: discount_id/reward_id = %s/%s, want %s/%s", label, dump(p.DiscountID), dump(p.RewardID), dump(w.discount), dump(w.reward))
		}
	}

	// --- Création : orderID inconnu, les récompenses viennent de used_rewards ---
	products := []models.OrderProductPayload{
		{ProductID: "1", RewardID: id(rewardOK), DiscountID: id(rewardOK)}, // 0. POS à jour (renvoie les deux)
		{ProductID: "2", DiscountID: id(discountMine)},                     // 1. remise du marchand
		{ProductID: "3", DiscountID: id(rewardOld)},                        // 2. POS pas à jour : reward dans discount_id
		{ProductID: "4", DiscountID: id(987099)},                           // 3. inconnu
		{ProductID: "5", DiscountID: id(discountOther)},                    // 4. remise d'un autre marchand
		{ProductID: "6", DiscountID: str("not-a-number")},                  // 5. ni legacy ni numérique
		{ProductID: "7", RewardID: str("abc")},                             // 6. reward_id invalide
		{ProductID: "8", DiscountID: id(rewardCollision)},                  // 7. remise ET récompense : reste une remise
		{ProductID: "9", DiscountID: id(rewardFixed)},                      // 8. fixed_discount : jamais une ligne
		{ProductID: "10"}, // 9. rien
		{ProductID: "11", DiscountID: str("99999999999")},                         // 10. hors integer
		{ProductID: "12", RewardID: id(rewardFixed), DiscountID: id(rewardFixed)}, // 11. reward_id explicite de type fixed_discount
		{ProductID: "13", RewardID: id(rewardOK), DiscountID: id(discountMine)},   // 12. reward_id valide + remise : une seule attribution
		{ProductID: "14", RewardID: id(987098), DiscountID: id(987098)},           // 13. reward_id explicite inconnu
		{ProductID: "15", RewardID: id(rewardOld)},                                // 14. reward_id explicite valide, sans écho
	}
	if err := repo.resolveLineDiscounts(ctx, merchantID, nil, used(rewardOK, rewardOld, rewardCollision, rewardFixed), products); err != nil {
		t.Fatalf("resolveLineDiscounts (create): %v", err)
	}
	check("0 POS à jour", products[0], want{nil, id(rewardOK)})
	check("1 remise du marchand", products[1], want{id(discountMine), nil})
	check("2 POS pas à jour", products[2], want{nil, id(rewardOld)})
	check("3 inconnu", products[3], want{nil, nil})
	check("4 autre marchand", products[4], want{nil, nil})
	check("5 non numérique", products[5], want{nil, nil})
	check("6 reward_id invalide", products[6], want{nil, nil})
	check("7 collision", products[7], want{id(rewardCollision), nil})
	check("8 fixed_discount", products[8], want{nil, nil})
	check("9 rien", products[9], want{nil, nil})
	check("10 hors integer", products[10], want{nil, nil})
	check("11 reward_id explicite fixed_discount", products[11], want{nil, nil})
	check("12 reward_id valide + remise : la récompense l'emporte", products[12], want{nil, id(rewardOK)})
	check("13 reward_id explicite inconnu", products[13], want{nil, nil})
	check("14 reward_id explicite valide", products[14], want{nil, id(rewardOld)})

	// --- Sans récompense dans la commande : aucune requête sur customer_rewards,
	// donc une récompense « libre » reste un identifiant inconnu. ---
	lone := []models.OrderProductPayload{{ProductID: "1", DiscountID: id(rewardOld)}}
	if err := repo.resolveLineDiscounts(ctx, merchantID, nil, nil, lone); err != nil {
		t.Fatalf("resolveLineDiscounts (no rewards): %v", err)
	}
	check("sans used_rewards", lone[0], want{nil, nil})

	// --- Modification : la commande existe, la récompense est rattachée via
	// customer_rewards.used_on_order_id, used_rewards est absent du payload. ---
	edit := []models.OrderProductPayload{
		{ProductID: "1", DiscountID: id(rewardUpdate)},
		{ProductID: "2", DiscountID: id(discountMine)},
		{ProductID: "3", RewardID: id(rewardUpdate)}, // POS à jour : reward_id explicite
		{ProductID: "4", RewardID: id(rewardOK)},     // récompense non rattachée à cette commande
	}
	if err := repo.resolveLineDiscounts(ctx, merchantID, &orderID, nil, edit); err != nil {
		t.Fatalf("resolveLineDiscounts (update): %v", err)
	}
	check("modification : récompense rattachée", edit[0], want{nil, id(rewardUpdate)})
	check("modification : remise", edit[1], want{id(discountMine), nil})
	check("modification : reward_id explicite rattaché", edit[2], want{nil, id(rewardUpdate)})
	check("modification : reward_id non rattaché à la commande", edit[3], want{nil, nil})

	// --- Écriture réelle : la ligne offerte n'a plus de discount_id, donc ni
	// valeur fiscale ni ligne discount_redemptions ; reward_id est enregistré. ---
	price := 0
	oid, err := repo.InsertOrderItem(ctx, &models.OrderItemInsert{
		OrderID:         orderID,
		ProductID:       "1",
		MerchantID:      merchantID,
		Quantity:        1,
		DiscountID:      products[0].DiscountID,
		RewardID:        products[0].RewardID,
		Price:           price,
		BasePrice:       500,
		DiscountedPrice: &price,
		CreatedBy:       "itest-rewardline-user",
	})
	if err != nil {
		t.Fatalf("InsertOrderItem (reward line): %v", err)
	}
	var gotDiscount, gotReward *int64
	if err := db.QueryRowContext(ctx, `SELECT discount_id, reward_id FROM orderitems WHERE order_item_id = $1`, oid).Scan(&gotDiscount, &gotReward); err != nil {
		t.Fatalf("read orderitems: %v", err)
	}
	if gotDiscount != nil || gotReward == nil || *gotReward != rewardOK {
		t.Fatalf("orderitems discount_id/reward_id = %v/%v, want NULL/%d", gotDiscount, gotReward, rewardOK)
	}
	var redemptions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM discount_redemptions WHERE order_item_id = $1`, oid).Scan(&redemptions); err != nil {
		t.Fatalf("read discount_redemptions: %v", err)
	}
	if redemptions != 0 {
		t.Fatalf("a reward line must not create a discount_redemptions row, got %d", redemptions)
	}
}

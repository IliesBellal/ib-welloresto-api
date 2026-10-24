//go:build postgres_integration

package orders

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Modifier une commande dont la récompense « produit offert » est déjà
// consommée (créée par ScanNOrder, ouverte au POS) ne doit pas refaire payer
// la ligne offerte : le recalcul retrouve la récompense par les order_item_id
// des lignes déjà enregistrées (OrdersRepository.appliedRewardIDs).
//
// Prérequis : migration 189 (orderitems.reward_id).
func TestRewardKeptWhenEditingOrder_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var hasColumn bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'orderitems' AND column_name = 'reward_id')`).Scan(&hasColumn); err != nil {
		t.Fatalf("check migration 189: %v", err)
	}
	if !hasColumn {
		t.Skip("migration 189 (orderitems.reward_id) non appliquée sur cette base")
	}

	const programID = "itest-rwdedit-prog"
	var merchantIDs []string

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM customer_rewards WHERE loyalty_program_id = $1`,
			`DELETE FROM customer_loyalty_program_reward_products WHERE loyalty_program_id = $1`,
			`DELETE FROM customer_loyalty_programs WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, programID)
		}
		for _, sirets := range []string{"siret-rwdedit-a", "siret-rwdedit-b"} {
			var id int64
			if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, sirets).Scan(&id); err != nil {
				continue
			}
			mid := strconv.FormatInt(id, 10)
			for _, q := range []string{
				`DELETE FROM orderitems WHERE merchant_id = $1`,
				`DELETE FROM orders WHERE merchant_id = $1`,
				`DELETE FROM customer WHERE merchant_id = $1`,
				`DELETE FROM products WHERE merchant_Id = $1`,
				`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
				`DELETE FROM merchant WHERE id = $1`,
			} {
				_, _ = db.ExecContext(ctx, q, mid)
			}
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id IN (9321, 9322, 9323)`)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(desc, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	mustID := func(desc, q string, args ...interface{}) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
		return id
	}

	mustExec("tva_categories", `
		INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
		OVERRIDING SYSTEM VALUE VALUES
		(9321, 'IN', 'ITest TVA 10', 'itest', 10, TRUE),
		(9322, 'TAKE_AWAY', 'ITest TVA 5.5', 'itest', 5.5, TRUE),
		(9323, 'DELIVERY', 'ITest TVA 20', 'itest', 20, TRUE)`)

	newMerchant := func(siret string) string {
		t.Helper()
		id := mustID("merchant "+siret, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
			VALUES ('ITest RewardEdit', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'Europe/Paris', 1, 2)
			RETURNING id`, siret)
		mid := strconv.FormatInt(id, 10)
		mustExec("merchant_parameters", `
			INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, delivery_fees, delivery_fees_limit, minimum_cart_for_delivery_order)
			VALUES ($1, now(), 'EUR', true, 250, 2000, 0)`, mid)
		merchantIDs = append(merchantIDs, mid)
		return mid
	}
	merchantA, merchantB := newMerchant("siret-rwdedit-a"), newMerchant("siret-rwdedit-b")

	product := mustID("product", `
		INSERT INTO products (merchant_Id, name, price, price_take_away, price_delivery, category, status, tva_in_id, tva_delivery_id, tva_take_away_id)
		VALUES ($1, 'itest-rwdedit-pizza', 750, 750, 750, 'itest', '1', 9321, 9323, 9322)
		RETURNING product_id`, merchantA)
	productID := strconv.FormatInt(product, 10)

	customer := func(mid, tel string) string {
		return strconv.FormatInt(mustID("customer", `
			INSERT INTO customer (merchant_id, customer_first_name, customer_last_name, customer_name, customer_tel, enabled)
			VALUES ($1, 'Rwd', 'Edit', 'Rwd Edit', $2, true) RETURNING customer_id`, mid, tel), 10)
	}
	customerA, otherCustomerA := customer(merchantA, "+33612000091"), customer(merchantA, "+33612000092")

	mustExec("program", `
		INSERT INTO customer_loyalty_programs (id, merchant_id, name, description, type, target_value, reward_type, reward_value, min_order_value, max_rewards_per_order, available, enabled)
		VALUES ($1, $2, 'itest', 'itest', 'orders_count', 5, 'free_product', 1, 0, 1, false, false)`, programID, merchantA)
	mustExec("program product", `
		INSERT INTO customer_loyalty_program_reward_products (id, product_id, loyalty_program_id) VALUES ('itest-rwdedit-rp', $1, $2)`, productID, programID)

	newOrder := func(mid string) int64 {
		return mustID("order", `
			INSERT INTO orders (merchant_id, order_num, brand_status, tva, ht, price, created_by)
			VALUES ($1, 1, 'ACCEPTED', 0, 800, 800, 'itest-rwdedit') RETURNING order_id`, mid)
	}
	newItem := func(orderID int64, mid string, price int, rewardID *int64) string {
		return strconv.FormatInt(mustID("orderitem", `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, reward_id, ordered_on)
			VALUES ($1, $2, $3, 1, 750, $4, $5, now()) RETURNING order_item_id`, orderID, product, mid, price, rewardID), 10)
	}
	newReward := func(cid, rewardType string, usedOnOrder int64) int64 {
		return mustID("reward", `
			INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_value, is_used, usage_date, used_on_order_id, creation_date)
			VALUES ($1, $2, $3, 1, true, now(), $4, now()) RETURNING reward_id`, cid, programID, rewardType, usedOnOrder)
	}

	// La commande ScanNOrder d'origine : une ligne offerte et une ligne payée.
	order := newOrder(merchantA)
	reward := newReward(customerA, "free_product", order)
	offeredItem := newItem(order, merchantA, 1, &reward)
	paidItem := newItem(order, merchantA, 750, nil)

	repo := NewOrdersRepository(db, nil)
	svc := &OrdersService{ordersRepo: repo}
	str := func(s string) *string { return &s }
	price := func(mid, cid string, items ...string) *models.PricingResponse {
		t.Helper()
		lines := make([]models.OrderProductPayload, 0, len(items))
		for _, item := range items {
			line := models.OrderProductPayload{ProductID: productID, Quantity: 1}
			if item != "" {
				line.OrderItemID = str(item)
			}
			lines = append(lines, line)
		}
		resp, err := svc.ComputePricing(ctx, &models.PricingRequest{
			MerchantID: mid,
			Order: &models.OrderRequest{
				OrderType: "IN",
				Products:  lines,
				Customer:  &models.CustomerRequest{CustomerID: &cid}, // le POS n'envoie aucune available_rewards
			},
		})
		if err != nil {
			t.Fatalf("ComputePricing: %v", err)
		}
		if resp == nil || resp.Status != "success" {
			t.Fatalf("unexpected pricing response: %+v", resp)
		}
		return resp
	}
	offered := func(resp *models.PricingResponse) (n int, ttc int) {
		for _, p := range resp.OrderRequest.Order.Products {
			if p.RewardID != nil {
				n += p.Quantity
			}
		}
		return n, resp.OrderRequest.Order.TTC
	}

	t.Run("modification de la commande : la ligne offerte le reste", func(t *testing.T) {
		resp := price(merchantA, customerA, offeredItem, paidItem)
		n, ttc := offered(resp)
		if n != 1 || ttc != 1+750 {
			t.Fatalf("offered units = %d, TTC = %d, want 1 and 751", n, ttc)
		}
		if got := resp.OrderRequest.Order.UsedRewards; len(got) != 1 || got[0].RewardID != strconv.FormatInt(reward, 10) {
			t.Fatalf("used_rewards = %+v, want the consumed reward %d", got, reward)
		}
	})

	t.Run("ajout d'une ligne à la modification : toujours une seule unité offerte", func(t *testing.T) {
		resp := price(merchantA, customerA, offeredItem, paidItem, "")
		if n, ttc := offered(resp); n != 1 || ttc != 1+750+750 {
			t.Fatalf("offered units = %d, TTC = %d, want 1 and 1501", n, ttc)
		}
	})

	t.Run("produit retiré de la commande : la récompense ne s'applique plus", func(t *testing.T) {
		resp := price(merchantA, customerA, paidItem)
		if n, ttc := offered(resp); n != 0 || ttc != 750 {
			t.Fatalf("offered units = %d, TTC = %d, want 0 and 750", n, ttc)
		}
	})

	t.Run("nouvelle commande (aucune ligne enregistrée) : rien n'est offert", func(t *testing.T) {
		resp := price(merchantA, customerA, "", "")
		if n, ttc := offered(resp); n != 0 || ttc != 1500 {
			t.Fatalf("offered units = %d, TTC = %d, want 0 and 1500", n, ttc)
		}
	})

	t.Run("autre client : la récompense d'un autre n'est pas reprise", func(t *testing.T) {
		resp := price(merchantA, otherCustomerA, offeredItem, paidItem)
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("a reward of another customer must not apply, got %d offered unit(s)", n)
		}
	})

	t.Run("autre marchand : les lignes d'un autre établissement n'ouvrent rien", func(t *testing.T) {
		resp := price(merchantB, customerA, offeredItem, paidItem)
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("lines of another merchant must not apply a reward, got %d offered unit(s)", n)
		}
	})

	t.Run("récompense consommée par une autre commande : non reprise", func(t *testing.T) {
		// La ligne porte reward_id mais la récompense a été consommée ailleurs.
		otherOrder := newOrder(merchantA)
		stolen := newItem(otherOrder, merchantA, 1, &reward) // reward est lié à `order`, pas à otherOrder
		resp := price(merchantA, customerA, stolen, paidItem)
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("reward consumed by another order must not apply, got %d offered unit(s)", n)
		}
	})

	t.Run("réduction en montant : jamais reprise (paiement REWARD déjà enregistré)", func(t *testing.T) {
		fixedOrder := newOrder(merchantA)
		fixed := newReward(customerA, "fixed_discount", fixedOrder)
		fixedItem := newItem(fixedOrder, merchantA, 1, &fixed)
		resp := price(merchantA, customerA, fixedItem)
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("a fixed_discount reward must not be reapplied, got %d offered unit(s)", n)
		}
		if len(resp.OrderRequest.Order.Payments) != 0 {
			t.Fatalf("no REWARD payment must be added, got %+v", resp.OrderRequest.Order.Payments)
		}
	})

	t.Run("order_item_id invalides : ignorés sans erreur", func(t *testing.T) {
		resp := price(merchantA, customerA, "abc", "-5", "99999999999", "")
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("invalid item ids must not apply a reward, got %d", n)
		}
	})

	t.Run("sans client identifié : rien", func(t *testing.T) {
		resp, err := svc.ComputePricing(ctx, &models.PricingRequest{
			MerchantID: merchantA,
			Order: &models.OrderRequest{
				OrderType: "IN",
				Products: []models.OrderProductPayload{
					{ProductID: productID, Quantity: 1, OrderItemID: str(offeredItem)},
				},
				Customer: &models.CustomerRequest{},
			},
		})
		if err != nil {
			t.Fatalf("ComputePricing: %v", err)
		}
		if n, _ := offered(resp); n != 0 {
			t.Fatalf("no customer id: no reward expected, got %d", n)
		}
	})
}

//go:build postgres_integration

package deliveroo_orders

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/webhook/marketplacefallback"
)

// Des commandes simultanées qui contiennent le même article ou la même option inconnus
// n'en créent qu'un : produit, option et mapping (avant, le mapping d'option échouait
// sur l'index unique pour la seconde commande, et rien n'empêchait deux produits).
func TestSyncConcurrent_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "itest-do-lot3"

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM integration_deliveroo_options_mapping WHERE merchant_id = $1`,
			`DELETE FROM integration_deliveroo_products_mapping WHERE merchant_id = $1`,
			`DELETE FROM product_configurable_attribute WHERE product_id IN (SELECT product_id::text FROM products WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, merchantID)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	count := func(q string) int {
		var n int
		if err := db.QueryRowContext(ctx, q, merchantID).Scan(&n); err != nil {
			t.Fatalf("%s : %v", q, err)
		}
		return n
	}

	item := DeliverooItem{PosItemID: "itest-lot3-item", Name: "Pizza Nouvelle", OperationalName: "pizza-nouvelle", UnitPrice: DeliverooMoney{Fractional: 1200}}
	mod := DeliverooModifier{PosItemID: "itest-lot3-mod", Name: "Sans oignons"}
	// un titre d'option de 150 caractères : configurable_attribute_options.title est limité à 80
	longMod := DeliverooModifier{PosItemID: "itest-lot3-mod-long", Name: repeat("Sans ", 30)}

	const workers = 8
	var wg sync.WaitGroup
	type result struct{ product, attr, option string }
	results := make(chan result, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			productID, err := repo.SyncProduct(ctx, merchantID, item)
			if err != nil {
				errs <- err
				return
			}
			attr, opt, err := repo.SyncOption(ctx, merchantID, productID, mod)
			if err != nil {
				errs <- err
				return
			}
			if _, _, err := repo.SyncOption(ctx, merchantID, productID, longMod); err != nil {
				errs <- err
				return
			}
			results <- result{productID, attr, opt}
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("une commande a échoué : %v", err)
	}
	seen := map[result]bool{}
	for r := range results {
		seen[r] = true
	}
	if len(seen) != 1 {
		t.Fatalf("les commandes simultanées n'ont pas toutes le même produit, groupe et option : %v", seen)
	}

	for q, want := range map[string]int{
		`SELECT count(*) FROM products WHERE merchant_id = $1 AND category = 'DELIVEROO'`:                                                                        1,
		`SELECT count(*) FROM integration_deliveroo_products_mapping WHERE merchant_id = $1`:                                                                     1,
		`SELECT count(*) FROM integration_deliveroo_options_mapping WHERE merchant_id = $1`:                                                                      2,
		`SELECT count(*) FROM configurable_attributes WHERE merchant_id = $1`:                                                                                    1,
		`SELECT count(*) FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`: 2,
	} {
		if got := count(q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}

	var title string
	if err := db.QueryRowContext(ctx, `
		SELECT o.title FROM configurable_attribute_options o JOIN integration_deliveroo_options_mapping m ON m.configurable_attribute_option_id = o.id
		WHERE m.merchant_id = $1 AND m.item_id = 'itest-lot3-mod-long'`, merchantID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(title)); n == 0 || n > 80 {
		t.Errorf("titre de l'option longue = %d caractères, want entre 1 et 80", n)
	}
}

// L'article de repli est créé une seule fois par établissement et par plateforme, même
// quand plusieurs commandes en ont besoin en même temps.
func TestFallbackProduct_Concurrent_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "itest-fallback-lot3"

	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	const workers = 16
	var wg sync.WaitGroup
	start := make(chan struct{}) // départ simultané : toutes les commandes lisent avant qu'aucune n'ait créé
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, err := marketplacefallback.Product(ctx, db, merchantID, models.MarketplaceCategoryDeliveroo, marketplacefallback.DeliverooName)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatalf("création du produit de repli : %v", err)
	}
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("plusieurs produits de repli : %v", seen)
	}

	var n int
	var tvaIn, tvaTakeAway, tvaDelivery int
	if err := db.QueryRowContext(ctx, `SELECT count(*), max(tva_in_id), max(tva_take_away_id), max(tva_delivery_id) FROM products WHERE merchant_id = $1`, merchantID).Scan(&n, &tvaIn, &tvaTakeAway, &tvaDelivery); err != nil {
		t.Fatal(err)
	}
	std := models.MarketplaceTVAStandard
	if n != 1 || tvaIn != std.In || tvaTakeAway != std.TakeAway || tvaDelivery != std.Delivery {
		t.Errorf("produits = %d, TVA = %d/%d/%d, want 1 produit à %+v", n, tvaIn, tvaTakeAway, tvaDelivery, std)
	}

	// autre plateforme : produit distinct
	other, err := marketplacefallback.Product(ctx, db, merchantID, models.MarketplaceCategoryUberEats, marketplacefallback.UberEatsName)
	if err != nil {
		t.Fatal(err)
	}
	if seen[other] {
		t.Error("le produit de repli Uber Eats doit être distinct de celui de Deliveroo")
	}
	if _, err := strconv.Atoi(other); err != nil {
		t.Errorf("identifiant non numérique : %q", other)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

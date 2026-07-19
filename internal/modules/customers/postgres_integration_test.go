//go:build postgres_integration

package customers

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func strPtrC(s string) *string  { return &s }
func f64PtrC(f float64) *float64 { return &f }

func TestCustomersRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999928"
	const programID = "itest-cust-lp"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_progress_order WHERE loyalty_program_id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_progress WHERE loyalty_program_id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_rewards WHERE loyalty_program_id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_program_target_products WHERE loyalty_program_id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_program_reward_products WHERE loyalty_program_id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_programs WHERE id = $1`, programID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewCustomerRepository(db)

	// --- UpdateOrCreateCustomer : INSERT (InsertReturningID) puis UPDATE (SET dynamique) ---
	created, err := repo.UpdateOrCreateCustomer(ctx, &models.Customer{
		MerchantID:        merchantID,
		CustomerName:      strPtrC("dupont"),
		CustomerBrand:     strPtrC("WELLO_RESTO"),
		CustomerFirstName: strPtrC("jean"),
		CustomerLastName:  strPtrC("dupont"),
		CustomerTel:       strPtrC("0612345678"),
		CustomerEmail:     strPtrC("jean.dupont@example.com"),
		CustomerLat:       f64PtrC(48.85),
		CustomerLng:       f64PtrC(2.35),
	})
	if err != nil {
		t.Fatalf("UpdateOrCreateCustomer (insert) failed against postgres: %v", err)
	}
	if created == nil || *created == "" || *created == "0" {
		t.Fatalf("expected generated customer id, got %v", created)
	}
	customerID := *created

	updated, err := repo.UpdateOrCreateCustomer(ctx, &models.Customer{
		CustomerID:    &customerID,
		MerchantID:    merchantID,
		CustomerEmail: strPtrC("jean.dupont+new@example.com"),
	})
	if err != nil || *updated != customerID {
		t.Fatalf("UpdateOrCreateCustomer (update) = (%v, %v)", updated, err)
	}

	// --- lookups ---
	got, err := repo.GetCustomerByID(ctx, customerID, merchantID)
	if err != nil || got.CustomerEmail == nil || *got.CustomerEmail != "jean.dupont+new@example.com" {
		t.Fatalf("GetCustomerByID = (%+v, %v)", got, err)
	}
	byEmail, err := repo.FindCustomerByEmail(ctx, "JEAN.DUPONT+NEW@EXAMPLE.COM", merchantID)
	if err != nil || byEmail == nil || *byEmail.CustomerID != customerID {
		t.Fatalf("FindCustomerByEmail = (%+v, %v)", byEmail, err)
	}
	byPhone, err := repo.FindCustomerByPhone(ctx, "06 12 34 56 78", merchantID)
	if err != nil || byPhone == nil || *byPhone.CustomerID != customerID {
		t.Fatalf("FindCustomerByPhone = (%+v, %v)", byPhone, err)
	}

	// --- programme de fidélité ---
	var prodID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-cust-prod', 1200, 'itest', 0, 0, 0) RETURNING product_id`, merchantID).Scan(&prodID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	prodStr := strconv.FormatInt(prodID, 10)

	program, err := repo.CreateLoyaltyProgram(ctx, merchantID, programID, &CreateLoyaltyProgramRequest{
		Name:        "Carte fidelite",
		Description: "1 gratuit tous les 3",
		Target: LoyaltyProgramTargetPayload{
			Type:       "orders_count",
			Value:      3,
			OrderTypes: "IN TAKE_AWAY DELIVERY",
			ProductIDs: []string{prodStr},
		},
		Reward: LoyaltyProgramRewardPayload{
			Type:               "DISCOUNT",
			Value:              500,
			OrderTypes:         "IN TAKE_AWAY DELIVERY",
			MinOrderValue:      1000,
			MaxRewardsPerOrder: 1,
			ProductIDs:         []string{prodStr},
		},
	})
	if err != nil {
		t.Fatalf("CreateLoyaltyProgram failed against postgres: %v", err)
	}
	if program.ID != programID || len(program.Target.Products) != 1 || len(program.Reward.Products) != 1 {
		t.Fatalf("unexpected program: %+v", program)
	}

	programs, err := repo.GetLoyaltyPrograms(ctx, merchantID)
	if err != nil || len(programs) != 1 || len(programs[0].Target.Products) != 1 {
		t.Fatalf("GetLoyaltyPrograms = (%+v, %v)", programs, err)
	}

	program, err = repo.UpdateLoyaltyProgram(ctx, merchantID, programID, &UpdateLoyaltyProgramRequest{
		Name: strPtrC("Carte fidelite v2"),
	})
	if err != nil || program.Name != "Carte fidelite v2" {
		t.Fatalf("UpdateLoyaltyProgram = (%+v, %v)", program, err)
	}

	// --- progression manuelle + récompense ---
	rewards, err := repo.UpdateLoyaltyProgress(ctx, &LoyaltyProgressUpdateRequest{
		CustomerID:       customerID,
		LoyaltyProgramID: programID,
		CurrentValue:     3,
	}, merchantID)
	if err != nil {
		t.Fatalf("UpdateLoyaltyProgress failed against postgres: %v", err)
	}
	if rewards != 1 {
		t.Fatalf("expected 1 reward created at target, got %d", rewards)
	}

	loyalty, err := repo.GetCustomerLoyalty(ctx, customerID, merchantID)
	if err != nil {
		t.Fatalf("GetCustomerLoyalty failed against postgres: %v", err)
	}
	if len(loyalty.LoyaltyProgress) != 1 || loyalty.LoyaltyProgress[0].CurrentValue != 3 {
		t.Fatalf("unexpected loyalty progress: %+v", loyalty.LoyaltyProgress)
	}
	if len(loyalty.AvailableRewards) != 1 {
		t.Fatalf("expected 1 reward, got %+v", loyalty.AvailableRewards)
	}
	rewardID := loyalty.AvailableRewards[0].RewardID

	// --- usage / réactivation de la récompense ---
	if err := repo.UpdateLoyaltyReward(ctx, &LoyaltyRewardUpdateRequest{
		CustomerID: customerID,
		RewardID:   rewardID,
		IsUsed:     true,
	}, merchantID); err != nil {
		t.Fatalf("UpdateLoyaltyReward failed against postgres: %v", err)
	}
	var isUsed bool
	if err := db.QueryRowContext(ctx, `SELECT is_used FROM customer_rewards WHERE reward_id = $1`, rewardID).Scan(&isUsed); err != nil {
		t.Fatalf("read back reward: %v", err)
	}
	if !isUsed {
		t.Fatal("expected reward marked used")
	}

	// --- UpdateLoyaltyFromOrder (stats client + progression + palier) ---
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, customer_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by)
		VALUES ($1, $2, 1, 'WELLO_RESTO', 'ACCEPTED', 'IN', 'CLOSED', 2400, 240, 2160, 'itest')
		RETURNING order_id`, merchantID, customerID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, $2, $3, 2, 1200)`, orderIntID, prodID, merchantID); err != nil {
		t.Fatalf("seed orderitems: %v", err)
	}

	if err := repo.UpdateLoyaltyFromOrder(ctx, orderID); err != nil {
		t.Fatalf("UpdateLoyaltyFromOrder failed against postgres: %v", err)
	}
	var nbOrders, totalSpent int
	if err := db.QueryRowContext(ctx, `SELECT customer_nb_orders, customer_total_spent FROM customer WHERE customer_id = $1`, customerID).Scan(&nbOrders, &totalSpent); err != nil {
		t.Fatalf("read back customer stats: %v", err)
	}
	if nbOrders != 1 || totalSpent != 2400 {
		t.Fatalf("expected stats 1 order / 2400 spent, got %d / %d", nbOrders, totalSpent)
	}
	var progressValue int
	if err := db.QueryRowContext(ctx, `SELECT current_value FROM customer_loyalty_progress WHERE customer_id = $1 AND loyalty_program_id = $2`, customerID, programID).Scan(&progressValue); err != nil {
		t.Fatalf("read back progress: %v", err)
	}
	if progressValue != 4 {
		t.Fatalf("expected progress 3+1=4 after order, got %d", progressValue)
	}
	// Idempotence : la même commande n'est pas comptée deux fois
	if err := repo.UpdateLoyaltyFromOrder(ctx, orderID); err != nil {
		t.Fatalf("UpdateLoyaltyFromOrder (repeat) failed: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT current_value FROM customer_loyalty_progress WHERE customer_id = $1 AND loyalty_program_id = $2`, customerID, programID).Scan(&progressValue); err != nil {
		t.Fatalf("read back progress (repeat): %v", err)
	}
	if progressValue != 4 {
		t.Fatalf("expected progress unchanged on repeat, got %d", progressValue)
	}

	// --- ReactivateRewards ---
	if _, err := db.ExecContext(ctx, `UPDATE customer_rewards SET used_on_order_id = $1 WHERE reward_id = $2`, orderIntID, rewardID); err != nil {
		t.Fatalf("attach reward to order: %v", err)
	}
	if err := repo.ReactivateRewards(ctx, orderID); err != nil {
		t.Fatalf("ReactivateRewards failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT is_used FROM customer_rewards WHERE reward_id = $1`, rewardID).Scan(&isUsed); err != nil {
		t.Fatalf("read back reactivated reward: %v", err)
	}
	if isUsed {
		t.Fatal("expected reward reactivated")
	}

	// --- recherche / listing ---
	results, total, err := repo.SearchCustomers(ctx, merchantID, "dupont", "customer_first_name", "ASC", 1, 10)
	if err != nil || total != 1 || len(results) != 1 || results[0].CustomerID != customerID {
		t.Fatalf("SearchCustomers (nom) = (total=%d, %+v, %v)", total, results, err)
	}
	results, total, err = repo.SearchCustomers(ctx, merchantID, "06 12 34 56 78", "", "", 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("SearchCustomers (téléphone) = (total=%d, %v)", total, err)
	}
	listed, total, err := repo.ListCustomers(ctx, merchantID, "creation_date", "DESC", 1, 10)
	if err != nil || total != 1 || len(listed) != 1 {
		t.Fatalf("ListCustomers = (total=%d, %+v, %v)", total, listed, err)
	}

	// --- GetLoyaltyProgramByID + suppression douce ---
	byID, err := repo.GetLoyaltyProgramByID(ctx, merchantID, programID)
	if err != nil || byID.ID != programID {
		t.Fatalf("GetLoyaltyProgramByID = (%+v, %v)", byID, err)
	}
	if err := repo.DeleteLoyaltyProgram(ctx, merchantID, programID); err != nil {
		t.Fatalf("DeleteLoyaltyProgram failed against postgres: %v", err)
	}
	programs, err = repo.GetLoyaltyPrograms(ctx, merchantID)
	if err != nil || len(programs) != 0 {
		t.Fatalf("expected no programs after soft delete, got (%d, %v)", len(programs), err)
	}
}

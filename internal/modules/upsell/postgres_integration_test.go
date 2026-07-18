//go:build postgres_integration

package upsell

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestUpsellRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-upsell-m1"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM upsell_suggestions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	// --- CreateSuggestion + GetSuggestion ---
	provider := "anthropic"
	model := "claude"
	tokIn, tokOut := 100, 50
	id, err := repo.CreateSuggestion(ctx, CreateSuggestionParams{
		MerchantID:    merchantID,
		CartSignature: "itest-cart-sig",
		SuggestedItems: []SuggestedItem{
			{ProductID: "p1", Title: "Un dessert ?", Score: 0.9, Name: "Tiramisu", Price: 450},
		},
		Source:      "llm",
		Channel:     "POS",
		LLMProvider: &provider,
		LLMModel:    &model,
		TokensIn:    &tokIn,
		TokensOut:   &tokOut,
		LatencyMs:   120,
	})
	if err != nil {
		t.Fatalf("CreateSuggestion failed against postgres: %v", err)
	}

	got, err := repo.GetSuggestion(ctx, id)
	if err != nil {
		t.Fatalf("GetSuggestion failed against postgres: %v", err)
	}
	if got.MerchantID != merchantID || len(got.SuggestedItems) != 1 || got.SuggestedItems[0].Name != "Tiramisu" {
		t.Fatalf("unexpected suggestion: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("created_at not populated")
	}

	// --- RecordAcceptance : mismatch marchand, acceptation, idempotence ---
	if err := repo.RecordAcceptance(ctx, id, "other-merchant", RecordAcceptanceParams{}); !errors.Is(err, ErrSuggestionMerchantMismatch) {
		t.Fatalf("expected merchant mismatch error, got %v", err)
	}
	if err := repo.RecordAcceptance(ctx, id, merchantID, RecordAcceptanceParams{
		OrderID:       "itest-order-1",
		AcceptedItems: []AcceptedItem{{ProductID: "p1", Quantity: 1}},
		RevenueImpact: 4.50,
	}); err != nil {
		t.Fatalf("RecordAcceptance failed against postgres: %v", err)
	}
	// Rejouer : no-op silencieux
	if err := repo.RecordAcceptance(ctx, id, merchantID, RecordAcceptanceParams{}); err != nil {
		t.Fatalf("RecordAcceptance idempotency failed: %v", err)
	}
	if _, err := repo.GetSuggestion(ctx, "unknown-id"); !errors.Is(err, ErrSuggestionNotFound) {
		t.Fatalf("expected ErrSuggestionNotFound, got %v", err)
	}

	// --- DeleteOldSuggestions : vieillir artificiellement la ligne ---
	if _, err := db.ExecContext(ctx,
		`UPDATE upsell_suggestions SET created_at = now() - interval '13 months' WHERE id = $1`, id); err != nil {
		t.Fatalf("age row: %v", err)
	}
	deleted, err := repo.DeleteOldSuggestions(ctx, 12)
	if err != nil {
		t.Fatalf("DeleteOldSuggestions failed against postgres: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("expected at least 1 deleted row, got %d", deleted)
	}

	// --- ListFeaturedProducts ---
	_, err = db.ExecContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category, is_popular, available, enabled, status)
		VALUES ($1, 'ITest Burger', 950, 'itest', TRUE, TRUE, TRUE, 'available')`, merchantID)
	if err != nil {
		t.Fatalf("seed products: %v", err)
	}
	items, err := repo.ListFeaturedProducts(ctx, merchantID, 5)
	if err != nil {
		t.Fatalf("ListFeaturedProducts failed against postgres: %v", err)
	}
	if len(items) != 1 || items[0].Name != "ITest Burger" || items[0].Price != 950 {
		t.Fatalf("unexpected featured products: %+v", items)
	}

	// --- GetMerchantUpsellSettings : défauts sans ligne, puis avec ligne ---
	enabled, maxItems, err := repo.GetMerchantUpsellSettings(ctx, merchantID)
	if err != nil || enabled || maxItems != 3 {
		t.Fatalf("expected defaults (false, 3), got (%v, %d, err=%v)", enabled, maxItems, err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, enable_upsell, upsell_max_items)
		VALUES ($1, now(), TRUE, 25)`, merchantID)
	if err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	enabled, maxItems, err = repo.GetMerchantUpsellSettings(ctx, merchantID)
	if err != nil || !enabled || maxItems != 10 {
		t.Fatalf("expected (true, capped 10), got (%v, %d, err=%v)", enabled, maxItems, err)
	}
}

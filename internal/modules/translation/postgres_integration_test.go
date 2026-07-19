//go:build postgres_integration

package translation

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestTranslationRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-tr-m1"
	const enabledLang = "zt"
	const disabledLang = "zd"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_translation_languages WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM available_languages WHERE code IN ($1, $2)`, enabledLang, disabledLang)
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.ExecContext(ctx, `
		INSERT INTO available_languages (code, name, enabled) VALUES ($1, $2, true), ($3, $4, false)`,
		enabledLang, "ITest Zetatian", disabledLang, "ITest Zedish")
	if err != nil {
		t.Fatalf("seed available_languages: %v", err)
	}

	repo := NewRepository(db)

	// ListAvailableLanguages: only enabled ones.
	langs, err := repo.ListAvailableLanguages(ctx)
	if err != nil {
		t.Fatalf("ListAvailableLanguages failed against postgres: %v", err)
	}
	foundEnabled, foundDisabled := false, false
	for _, l := range langs {
		if l.Code == enabledLang {
			foundEnabled = true
		}
		if l.Code == disabledLang {
			foundDisabled = true
		}
	}
	if !foundEnabled {
		t.Fatal("expected enabled test language in ListAvailableLanguages")
	}
	if foundDisabled {
		t.Fatal("disabled test language must not appear in ListAvailableLanguages")
	}

	// IsLanguageEnabledForMerchant: false before activation.
	ok, err := repo.IsLanguageEnabledForMerchant(ctx, merchantID, enabledLang)
	if err != nil {
		t.Fatalf("IsLanguageEnabledForMerchant (before) failed: %v", err)
	}
	if ok {
		t.Fatal("expected language not enabled for merchant before activation")
	}

	// ActivateLanguageForMerchant: insert branch of the ON CONFLICT upsert.
	if err := repo.ActivateLanguageForMerchant(ctx, merchantID, enabledLang); err != nil {
		t.Fatalf("ActivateLanguageForMerchant (insert) failed against postgres: %v", err)
	}
	// Re-activation: DO UPDATE branch of the same upsert (idempotent).
	if err := repo.ActivateLanguageForMerchant(ctx, merchantID, enabledLang); err != nil {
		t.Fatalf("ActivateLanguageForMerchant (update) failed against postgres: %v", err)
	}

	ok, err = repo.IsLanguageEnabledForMerchant(ctx, merchantID, enabledLang)
	if err != nil {
		t.Fatalf("IsLanguageEnabledForMerchant (after) failed: %v", err)
	}
	if !ok {
		t.Fatal("expected language enabled for merchant after activation")
	}

	mlangs, err := repo.ListMerchantLanguages(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListMerchantLanguages failed against postgres: %v", err)
	}
	if len(mlangs) != 1 || mlangs[0].LangCode != enabledLang || mlangs[0].Name != "ITest Zetatian" {
		t.Fatalf("unexpected ListMerchantLanguages result: %+v", mlangs)
	}

	// ActivateLanguageForMerchantWithLimit: already-enabled short-circuit, then limit reached.
	if err := repo.ActivateLanguageForMerchantWithLimit(ctx, merchantID, enabledLang, 1); err != nil {
		t.Fatalf("ActivateLanguageForMerchantWithLimit (already enabled) failed: %v", err)
	}
	if err := repo.ActivateLanguageForMerchantWithLimit(ctx, merchantID, disabledLang, 1); err != models.ErrTranslationLanguagesLimitReached {
		t.Fatalf("expected ErrTranslationLanguagesLimitReached, got %v", err)
	}

	// DeactivateLanguageForMerchant.
	if err := repo.DeactivateLanguageForMerchant(ctx, merchantID, enabledLang); err != nil {
		t.Fatalf("DeactivateLanguageForMerchant failed against postgres: %v", err)
	}
	ok, err = repo.IsLanguageEnabledForMerchant(ctx, merchantID, enabledLang)
	if err != nil {
		t.Fatalf("IsLanguageEnabledForMerchant (after deactivate) failed: %v", err)
	}
	if ok {
		t.Fatal("expected language not enabled for merchant after deactivation")
	}
}

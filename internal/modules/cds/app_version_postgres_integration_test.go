//go:build postgres_integration

package cds

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Règle de POST /cds/app/version-check contre un vrai Postgres : version la
// plus haute au-delà du build installé, publiée, et pilote réservé aux
// merchants listés. Numéros très hauts et sans checksum : un écran réel de
// staging qui interrogerait l'API pendant le test refuserait d'installer.
func TestCheckAppVersion_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	const base = 2_100_000_000
	const pilotMerchant = "itest-cds-pilot"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM app_version_merchant WHERE app_id = 'cds' AND version_code >= $1`, base)
		_, _ = db.ExecContext(ctx, `DELETE FROM app_version WHERE app_id = 'cds' AND version_code >= $1`, base)
	}
	cleanup()
	t.Cleanup(cleanup)

	seed := func(code int, release time.Time) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO app_version (app_id, version_code, last_functional_version_code, download_url, release_date)
			VALUES ('cds', $1, 0, 'https://example.com/cds.apk', $2)`, code, release); err != nil {
			t.Fatalf("seed %d: %v", code, err)
		}
	}
	past := time.Now().UTC().Add(-time.Hour)
	seed(base+1, past)
	seed(base+2, time.Now().UTC().Add(24*time.Hour)) // pas encore publiée

	resp, err := repo.CheckAppVersion(ctx, base, "itest-cds-other")
	if err != nil {
		t.Fatalf("CheckAppVersion: %v", err)
	}
	if resp.Status != "update_available" || resp.VersionCode == nil || *resp.VersionCode != base+1 {
		t.Fatalf("attendu %d disponible, obtenu %+v", base+1, resp)
	}

	// Déjà au dernier build publié : rien à proposer.
	if resp, err = repo.CheckAppVersion(ctx, base+1, "itest-cds-other"); err != nil || resp.Status != "no_update" {
		t.Fatalf("build à jour: %+v, %v", resp, err)
	}

	// Version en pilote : seul le merchant listé la reçoit.
	seed(base+3, past)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO app_version_merchant (version_code, app_id, merchant_id) VALUES ($1, 'cds', $2)`, base+3, pilotMerchant); err != nil {
		t.Fatalf("seed pilote: %v", err)
	}
	if resp, err = repo.CheckAppVersion(ctx, base+1, "itest-cds-other"); err != nil || resp.Status != "no_update" {
		t.Fatalf("hors pilote: %+v, %v", resp, err)
	}
	if resp, err = repo.CheckAppVersion(ctx, base+1, pilotMerchant); err != nil || resp.Status != "update_available" || *resp.VersionCode != base+3 {
		t.Fatalf("pilote: %+v, %v", resp, err)
	}
}

//go:build postgres_integration

package appreleases

import (
	"context"
	"errors"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Vérifie le SQL de LatestPublic contre un vrai Postgres : version la plus
// haute, publiée, sans restriction pilote, et filtrée par app_id.
func TestLatestPublic_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const appID = "itest-appreleases"
	const otherAppID = "itest-appreleases-2"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM app_version_merchant WHERE app_id IN ($1, $2)`, appID, otherAppID)
		_, _ = db.ExecContext(ctx, `DELETE FROM app_version WHERE app_id IN ($1, $2)`, appID, otherAppID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	if _, _, _, err := repo.LatestPublic(ctx, appID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("sans version: err = %v, attendu ErrNotFound", err)
	}

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(24 * time.Hour)
	seed := func(app string, code int, url string, release time.Time) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO app_version (app_id, version_code, last_functional_version_code, download_url, release_date)
			VALUES ($1, $2, 0, $3, $4)`, app, code, url, release); err != nil {
			t.Fatalf("seed app_version %s/%d: %v", app, code, err)
		}
	}
	seed(appID, 10, "https://example.com/v10.apk", past)
	seed(appID, 11, "https://example.com/v11.apk", past)   // en pilote ci-dessous
	seed(appID, 12, "https://example.com/v12.apk", future) // pas encore publiée
	seed(otherAppID, 99, "https://example.com/other.apk", past)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO app_version_merchant (version_code, app_id, merchant_id) VALUES (11, $1, 'itest-merchant')`, appID); err != nil {
		t.Fatalf("seed pilote: %v", err)
	}

	code, release, url, err := repo.LatestPublic(ctx, appID)
	if err != nil {
		t.Fatalf("LatestPublic: %v", err)
	}
	if code != 10 || url != "https://example.com/v10.apk" {
		t.Fatalf("obtenu %d %s, attendu 10 (11 en pilote, 12 future, 99 autre app)", code, url)
	}
	if release.IsZero() {
		t.Error("release_date vide")
	}

	// Fin du pilote : la version 11 devient publique.
	if _, err := db.ExecContext(ctx, `DELETE FROM app_version_merchant WHERE app_id = $1`, appID); err != nil {
		t.Fatalf("fin pilote: %v", err)
	}
	if code, _, _, err = repo.LatestPublic(ctx, appID); err != nil || code != 11 {
		t.Fatalf("après pilote: code = %d, err = %v, attendu 11", code, err)
	}
}

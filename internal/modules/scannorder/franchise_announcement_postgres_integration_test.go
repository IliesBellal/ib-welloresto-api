//go:build postgres_integration

package scannorder

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestFranchiseHubAnnouncement_Postgres : bandeau d'annonce du hub (migration
// 196, D34) — renvoyé seulement s'il est activé, avec un texte, et jusqu'à sa
// date de fin incluse.
func TestFranchiseHubAnnouncement_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const franchiseID, slug = "fra-itest-announcement", "itest-announcement"
	if _, err := db.ExecContext(ctx, `INSERT INTO franchises (franchise_id, name, slug) VALUES ($1, 'ITest', $2)`, franchiseID, slug); err != nil {
		t.Fatalf("seed franchise: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM franchises WHERE franchise_id = $1`, franchiseID) })

	repo := NewRepository(db)
	set := func(enabled bool, text, url, endsOn interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			UPDATE franchises SET announcement_enabled = $1, announcement_text = $2, announcement_url = $3, announcement_ends_on = CAST($4 AS date)
			WHERE franchise_id = $5`, enabled, text, url, endsOn, franchiseID); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	get := func() *HubAnnouncement {
		t.Helper()
		f, _, err := repo.GetFranchiseHub(ctx, slug)
		if err != nil || f == nil {
			t.Fatalf("GetFranchiseHub: %v (franchise %v)", err, f)
		}
		return f.Announcement
	}
	today := time.Now().In(mustLoc(t, "Europe/Paris")).Format("2006-01-02")
	yesterday := time.Now().In(mustLoc(t, "Europe/Paris")).AddDate(0, 0, -1).Format("2006-01-02")

	if a := get(); a != nil {
		t.Errorf("par défaut : %+v, want nil", a)
	}
	set(true, "Nouvelle ouverture à Nancy", "https://exemple.fr/nancy", nil)
	if a := get(); a == nil || a.Text != "Nouvelle ouverture à Nancy" || a.URL == nil || *a.URL != "https://exemple.fr/nancy" {
		t.Errorf("actif sans fin : %+v", a)
	}
	set(true, "Burger du mois", nil, today)
	if a := get(); a == nil || a.Text != "Burger du mois" || a.URL != nil {
		t.Errorf("dernier jour (inclus) : %+v", a)
	}
	set(true, "Burger du mois", nil, yesterday)
	if a := get(); a != nil {
		t.Errorf("date de fin passée : %+v, want nil", a)
	}
	set(false, "Burger du mois", nil, nil)
	if a := get(); a != nil {
		t.Errorf("désactivé : %+v, want nil", a)
	}
	set(true, "", nil, nil)
	if a := get(); a != nil {
		t.Errorf("activé sans texte : %+v, want nil", a)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

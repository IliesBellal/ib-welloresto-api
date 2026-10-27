package franchises

import (
	"errors"
	"strings"
	"testing"

	"welloresto-api/internal/models"
)

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func TestApplyAnnouncement(t *testing.T) {
	t.Run("activation avec texte, lien et date", func(t *testing.T) {
		var a Announcement
		err := applyAnnouncement(&a, AnnouncementUpdate{
			Enabled: boolp(true), Text: strp("  Nouvelle   ouverture à Nancy "), URL: strp("https://exemple.fr/nancy"), EndsOn: strp("2026-11-30"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !a.Enabled || *a.Text != "Nouvelle ouverture à Nancy" || *a.URL != "https://exemple.fr/nancy" || *a.EndsOn != "2026-11-30" {
			t.Errorf("announcement = %+v", a)
		}
	})

	t.Run("chaînes vides : effacent lien et date", func(t *testing.T) {
		a := Announcement{Enabled: true, Text: strp("x"), URL: strp("https://a.fr"), EndsOn: strp("2026-11-30")}
		if err := applyAnnouncement(&a, AnnouncementUpdate{URL: strp(""), EndsOn: strp("")}); err != nil {
			t.Fatal(err)
		}
		if a.URL != nil || a.EndsOn != nil || a.Text == nil {
			t.Errorf("announcement = %+v", a)
		}
	})

	invalid := map[string]AnnouncementUpdate{
		"activé sans texte":   {Enabled: boolp(true)},
		"texte trop long":     {Text: strp(strings.Repeat("a", maxAnnouncementLen+1))},
		"lien http":           {Text: strp("x"), URL: strp("http://exemple.fr")},
		"lien sans hôte":      {Text: strp("x"), URL: strp("https://")},
		"lien javascript":     {Text: strp("x"), URL: strp("javascript:alert(1)")},
		"date invalide":       {Text: strp("x"), EndsOn: strp("30/11/2026")},
		"texte vidé en actif": {Text: strp("   ")},
	}
	for name, req := range invalid {
		t.Run(name, func(t *testing.T) {
			a := Announcement{Enabled: name == "texte vidé en actif", Text: strp("ancien")}
			if name == "activé sans texte" {
				a.Text = nil
			}
			if err := applyAnnouncement(&a, req); !errors.Is(err, models.ErrFranchiseAnnouncementInvalid) {
				t.Errorf("err = %v, want ErrFranchiseAnnouncementInvalid", err)
			}
		})
	}

	t.Run("désactivation : texte conservé", func(t *testing.T) {
		a := Announcement{Enabled: true, Text: strp("Burger du mois")}
		if err := applyAnnouncement(&a, AnnouncementUpdate{Enabled: boolp(false)}); err != nil {
			t.Fatal(err)
		}
		if a.Enabled || a.Text == nil {
			t.Errorf("announcement = %+v", a)
		}
	})
}

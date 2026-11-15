package menu

import (
	"testing"
	"time"

	"welloresto-api/internal/infrastructure/r2"
)

func TestVersionedImageURL(t *testing.T) {
	base := "https://cdn.example.com/wello_resto_images_storage/merchants/7/config_options/42.jpg"

	first := versionedImageURL(base, time.UnixMilli(1_700_000_000_000))
	if want := base + "?v=1700000000000"; first != want {
		t.Fatalf("versionedImageURL = %q, want %q", first, want)
	}

	// Deux uploads successifs sur la même clé R2 doivent donner deux URL
	// différentes, sinon le back-office et les bornes gardent l'ancienne image
	// en cache.
	second := versionedImageURL(base, time.UnixMilli(1_700_000_000_001))
	if first == second {
		t.Fatalf("deux uploads successifs ont produit la même URL : %q", first)
	}
}

func TestShouldDeleteOldOptionImage(t *testing.T) {
	const merchantID, optionID = "7", "42"
	newKey := r2.GenerateConfigOptionKey(merchantID, optionID, ".jpg")

	cases := []struct {
		name   string
		oldKey string
		want   bool
	}{
		{"pas d'ancienne image", "", false},
		{"même clé (même format) : on écraserait la nouvelle", newKey, false},
		{"autre format de la même option : l'ancien fichier est orphelin",
			r2.GenerateConfigOptionKey(merchantID, optionID, ".png"), true},
		{"fichier d'une autre option (image propagée par titre)",
			r2.GenerateConfigOptionKey(merchantID, "43", ".png"), false},
		// "4" est un préfixe de "42" : la comparaison doit porter sur l'id
		// complet, pas sur un début d'id.
		{"id d'option qui commence comme le nôtre",
			r2.GenerateConfigOptionKey(merchantID, "421", ".png"), false},
		{"fichier d'un autre marchand",
			r2.GenerateConfigOptionKey("8", optionID, ".png"), false},
		{"clé hors du dossier des options (image produit)",
			r2.GenerateProductKey(merchantID, optionID, ".png"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDeleteOldOptionImage(tc.oldKey, newKey, merchantID, optionID)
			if got != tc.want {
				t.Fatalf("shouldDeleteOldOptionImage(%q) = %v, want %v", tc.oldKey, got, tc.want)
			}
		})
	}
}

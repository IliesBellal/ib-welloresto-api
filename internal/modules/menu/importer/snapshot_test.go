package importer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TvaMapping a une clé composite. encoding/json refuse les clés de type struct :
// sans MarshalText/UnmarshalText, le snapshot ne se sérialiserait pas — et le
// commit n'aurait aucune décision à rejouer.
func TestTvaRateKeyTextRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		key  TvaRateKey
		want string
	}{
		{"taux entier sur place", TvaRateKey{Rate: 10, Channel: TvaChannelIn}, "10:0"},
		{"taux décimal en livraison", TvaRateKey{Rate: 5.5, Channel: TvaChannelDelivery}, "5.5:1"},
		{"taux nul à emporter", TvaRateKey{Rate: 0, Channel: TvaChannelTakeAway}, "0:3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.key.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText: %v", err)
			}
			if string(encoded) != tc.want {
				t.Fatalf("MarshalText = %q, want %q", encoded, tc.want)
			}

			var decoded TvaRateKey
			if err := decoded.UnmarshalText(encoded); err != nil {
				t.Fatalf("UnmarshalText: %v", err)
			}
			if decoded != tc.key {
				t.Fatalf("aller-retour = %+v, want %+v", decoded, tc.key)
			}
		})
	}

	var key TvaRateKey
	for _, invalid := range []string{"", "10", "dix:0", "10:canal"} {
		if err := key.UnmarshalText([]byte(invalid)); err == nil {
			t.Fatalf("UnmarshalText(%q) = nil, want une erreur", invalid)
		}
	}
}

func TestPreviewSnapshotRoundTrip(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	result, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	createdAt := time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC)
	snapshot := &PreviewSnapshot{
		Token:      "01234567-89ab-cdef-0123-456789abcdef",
		MerchantID: "m-1",
		Provider:   ZeltySlug,
		CreatedAt:  createdAt,
		Import:     imp,
		Decisions:  result.Decisions,
	}

	payload, err := snapshot.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := DecodePreviewSnapshot(payload)
	if err != nil {
		t.Fatalf("DecodePreviewSnapshot: %v", err)
	}

	if decoded.Token != snapshot.Token || decoded.MerchantID != "m-1" || decoded.Provider != ZeltySlug {
		t.Fatalf("métadonnées = %+v", decoded)
	}
	if !decoded.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", decoded.CreatedAt, createdAt)
	}

	// Le canonique doit traverser intact : c'est lui que le commit insère.
	if len(decoded.Import.Products) != len(imp.Products) ||
		len(decoded.Import.Tags) != len(imp.Tags) ||
		len(decoded.Import.Attributes) != len(imp.Attributes) {
		t.Fatalf("canonique tronqué: %d produits, %d libellés, %d groupes",
			len(decoded.Import.Products), len(decoded.Import.Tags), len(decoded.Import.Attributes))
	}

	original := imp.Products[0]
	restored := decoded.Import.Products[0]
	if restored.ExternalID != original.ExternalID || restored.PriceIn != original.PriceIn {
		t.Fatalf("produit restauré = %+v, want %+v", restored, original)
	}
	if restored.TvaRateIn == nil || original.TvaRateIn == nil || *restored.TvaRateIn != *original.TvaRateIn {
		t.Fatalf("taux restauré = %v, want %v", restored.TvaRateIn, original.TvaRateIn)
	}

	// Les décisions aussi, mapping de TVA à clé composite compris.
	if len(decoded.Decisions.TvaMapping) != len(result.Decisions.TvaMapping) {
		t.Fatalf("TvaMapping = %d entrées, want %d", len(decoded.Decisions.TvaMapping), len(result.Decisions.TvaMapping))
	}
	for key, want := range result.Decisions.TvaMapping {
		if got := decoded.Decisions.TvaMapping[key]; got != want {
			t.Fatalf("TvaMapping[%+v] = %d, want %d", key, got, want)
		}
	}
	if len(decoded.Decisions.TagClassification) != len(result.Decisions.TagClassification) {
		t.Fatalf("TagClassification = %d entrées, want %d",
			len(decoded.Decisions.TagClassification), len(result.Decisions.TagClassification))
	}
	if len(decoded.Decisions.CategoryPerProduct) != len(result.Decisions.CategoryPerProduct) {
		t.Fatalf("CategoryPerProduct = %d entrées, want %d",
			len(decoded.Decisions.CategoryPerProduct), len(result.Decisions.CategoryPerProduct))
	}

	// La clé sérialisée doit être lisible, pas un blob opaque.
	if !strings.Contains(payload, `"10:0"`) {
		t.Fatalf("le mapping de TVA n'apparaît pas sous la forme \"<taux>:<canal>\" dans %.200s", payload)
	}
}

func TestPreviewSnapshotRejectsEmptyPayload(t *testing.T) {
	if _, err := (&PreviewSnapshot{}).Encode(); err == nil {
		t.Fatal("Encode() sans canonique = nil, want une erreur")
	}
	if _, err := DecodePreviewSnapshot("{}"); err == nil {
		t.Fatal("DecodePreviewSnapshot(\"{}\") = nil, want une erreur")
	}
	if _, err := DecodePreviewSnapshot("pas du json"); err == nil {
		t.Fatal("DecodePreviewSnapshot(invalide) = nil, want une erreur")
	}
}

// Le PreviewResult est renvoyé tel quel en HTTP : il doit rester sérialisable,
// mapping de TVA compris.
func TestPreviewResultIsJSONSerializable(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	result, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(PreviewResult): %v", err)
	}

	var roundTrip PreviewResult
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal(PreviewResult): %v", err)
	}
	if len(roundTrip.Products) != len(result.Products) {
		t.Fatalf("produits après aller-retour = %d, want %d", len(roundTrip.Products), len(result.Products))
	}
}

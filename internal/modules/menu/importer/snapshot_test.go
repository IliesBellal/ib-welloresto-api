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
		{"taux entier sur place", TvaRateKey{Rate: 10, Channel: TvaChannelIn}, "10:IN"},
		{"taux décimal en livraison", TvaRateKey{Rate: 5.5, Channel: TvaChannelDelivery}, "5.5:DELIVERY"},
		{"taux nul à emporter", TvaRateKey{Rate: 0, Channel: TvaChannelTakeAway}, "0:TAKE_AWAY"},
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
	for _, invalid := range []string{"", "10", "dix:IN", "10:CANAL_INCONNU"} {
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
	if !strings.Contains(payload, `"10:IN"`) {
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

// Le contrat JSON d'ImportDecisions est verrouillé ici : c'est la seule
// structure de la chaîne d'import qui transite dans les deux sens — la preview
// la propose, le commit la reçoit amendée par le wizard. Sans tags explicites,
// encoding/json émettrait les noms de champs Go en PascalCase, à contre-courant
// du snake_case de tout le reste, et le front se brancherait sur des clés qui
// changeraient au premier renommage de champ.
func TestImportDecisionsJSONContract(t *testing.T) {
	decisions := ImportDecisions{
		TagClassification:  map[string]TagClass{"ZT1": TagClassCategory},
		CategoryPerProduct: map[string]string{"ZD1": "ZT1"},
		TvaMapping:         map[TvaRateKey]int{{Rate: 5.5, Channel: TvaChannelDelivery}: 7},
		NameCollisions:     map[string]NameCollisionResolution{"ZD2": CollisionSkip},
		AlreadyImported:    map[string]ReimportResolution{"ZD3": ReimportRecreate},
		ExcludedProducts:   map[string]bool{"ZD4": true},
	}

	payload, err := json.Marshal(decisions)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(payload, &generic); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	wantKeys := []string{
		"tag_classification", "category_per_product", "tva_mapping",
		"name_collisions", "already_imported", "excluded_products",
	}
	if len(generic) != len(wantKeys) {
		t.Fatalf("clés = %v, want %v", generic, wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := generic[key]; !ok {
			t.Fatalf("clé %q absente de %s", key, payload)
		}
	}

	// La clé composite du mapping de TVA reste rendue par TextMarshaler.
	if !strings.Contains(string(payload), `"5.5:DELIVERY"`) {
		t.Fatalf("clé de TVA absente ou mal formée dans %s", payload)
	}

	// Aller-retour complet : ce que le wizard renvoie doit se relire à
	// l'identique, sinon le commit rejouerait d'autres décisions que celles
	// prises à l'écran.
	var decoded ImportDecisions
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if decoded.TagClassification["ZT1"] != TagClassCategory {
		t.Fatalf("TagClassification = %v", decoded.TagClassification)
	}
	if decoded.CategoryPerProduct["ZD1"] != "ZT1" {
		t.Fatalf("CategoryPerProduct = %v", decoded.CategoryPerProduct)
	}
	if got := decoded.TvaMapping[TvaRateKey{Rate: 5.5, Channel: TvaChannelDelivery}]; got != 7 {
		t.Fatalf("TvaMapping[5.5, delivery] = %d, want 7", got)
	}
	if decoded.NameCollisions["ZD2"] != CollisionSkip {
		t.Fatalf("NameCollisions = %v", decoded.NameCollisions)
	}
	if decoded.AlreadyImported["ZD3"] != ReimportRecreate {
		t.Fatalf("AlreadyImported = %v", decoded.AlreadyImported)
	}
}

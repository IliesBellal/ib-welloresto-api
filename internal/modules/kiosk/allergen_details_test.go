package kiosk

import (
	"encoding/json"
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

// La borne reçoit les allergènes avec leur icône et leur couleur
// (allergen_details), en plus de la liste de noms historique (allergens) que
// les bornes déjà installées continuent de lire.
func TestMapProductEntryToKioskProduct_AllergenDetails(t *testing.T) {
	product := &models.ProductEntry{
		ProductID: "prod_1",
		Name:      "Burger",
		Price:     1200,
		Allergens: []models.AllergenEntry{
			{ID: "a1", Name: "Gluten", Code: "GLU", Icon: "🌾", Color: "#B45309"},
			{ID: "a2", Name: "Lait", Code: "LAI"},
		},
	}

	got := mapProductEntryToKioskProduct(product, models.OrderTypeIn)

	if want := []string{"Gluten", "Lait"}; !reflect.DeepEqual(got.Allergens, want) {
		t.Fatalf("Allergens = %v, want %v (liste de noms historique inchangée)", got.Allergens, want)
	}
	want := []KioskAllergen{
		{ID: "a1", Name: "Gluten", Code: "GLU", Icon: "🌾", Color: "#B45309"},
		{ID: "a2", Name: "Lait", Code: "LAI"},
	}
	if !reflect.DeepEqual(got.AllergenDetails, want) {
		t.Fatalf("AllergenDetails = %+v, want %+v", got.AllergenDetails, want)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := decoded["allergens"].([]any); !ok {
		t.Fatalf(`"allergens" doit rester une liste de noms (payload: %s)`, raw)
	}
	details, ok := decoded["allergen_details"].([]any)
	if !ok || len(details) != 2 {
		t.Fatalf(`"allergen_details" attendu avec 2 entrées (payload: %s)`, raw)
	}
	first := details[0].(map[string]any)
	if first["icon"] != "🌾" || first["color"] != "#B45309" || first["id"] != "a1" {
		t.Fatalf("premier allergène = %v, want icône/couleur/id renseignés", first)
	}
	second := details[1].(map[string]any)
	if _, present := second["icon"]; present {
		t.Fatalf("icône vide : la clé doit être omise (payload: %s)", raw)
	}
}

func TestMapProductEntryToKioskProduct_NoAllergensOmitsDetails(t *testing.T) {
	got := mapProductEntryToKioskProduct(&models.ProductEntry{ProductID: "p", Name: "Eau", Price: 200}, models.OrderTypeIn)
	raw, _ := json.Marshal(got)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if _, present := decoded["allergen_details"]; present {
		t.Fatalf("sans allergène, allergen_details doit être omis (payload: %s)", raw)
	}
}

// Les tags suivent le même principe : la borne reçoit leur couleur de fond
// (tag_details) en plus de la liste de noms historique (tags).
func TestMapProductEntryToKioskProduct_TagDetails(t *testing.T) {
	product := &models.ProductEntry{
		ProductID: "prod_1",
		Name:      "Burger",
		Price:     1200,
		Tags: []models.TagEntry{
			{ID: "t1", Name: "Végétarien", Color: "#2E7D32"},
			{ID: "t2", Name: "Nouveau"},
		},
	}

	got := mapProductEntryToKioskProduct(product, models.OrderTypeIn)

	if want := []string{"Végétarien", "Nouveau"}; !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("Tags = %v, want %v (liste de noms historique inchangée)", got.Tags, want)
	}
	want := []KioskTag{
		{ID: "t1", Name: "Végétarien", Color: "#2E7D32"},
		{ID: "t2", Name: "Nouveau"},
	}
	if !reflect.DeepEqual(got.TagDetails, want) {
		t.Fatalf("TagDetails = %+v, want %+v", got.TagDetails, want)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	details, ok := decoded["tag_details"].([]any)
	if !ok || len(details) != 2 {
		t.Fatalf(`"tag_details" attendu avec 2 entrées (payload: %s)`, raw)
	}
	if details[0].(map[string]any)["color"] != "#2E7D32" {
		t.Fatalf("couleur du premier tag absente (payload: %s)", raw)
	}
	if _, present := details[1].(map[string]any)["color"]; present {
		t.Fatalf("couleur vide : la clé doit être omise (payload: %s)", raw)
	}
}

func TestMapProductEntryToKioskProduct_NoTagsOmitsDetails(t *testing.T) {
	got := mapProductEntryToKioskProduct(&models.ProductEntry{ProductID: "p", Name: "Eau", Price: 200}, models.OrderTypeIn)
	raw, _ := json.Marshal(got)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if _, present := decoded["tag_details"]; present {
		t.Fatalf("sans tag, tag_details doit être omis (payload: %s)", raw)
	}
}

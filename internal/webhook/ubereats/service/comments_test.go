package service

import (
	"encoding/json"
	"testing"

	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func parseItem(t *testing.T, raw string) ueModels.UberCartItem {
	t.Helper()
	var item ueModels.UberCartItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("article illisible: %v", err)
	}
	return item
}

func TestOrderComment(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string // "" = pas de commentaire
	}{
		{"aucun commentaire", `{"cart": {"items": []}}`, ""},
		{"commentaire vide ou blanc", `{"cart": {"special_instructions": "   "}, "store_instructions": ""}`, ""},
		{
			"consignes de préparation (champ documenté)",
			`{"cart": {"special_instructions": "Bien cuites svp"}}`,
			"Bien cuites svp",
		},
		{
			"store_instructions conservé",
			`{"store_instructions": "Sonner deux fois"}`,
			"Sonner deux fois",
		},
		{
			"même texte dans les deux champs : une seule fois",
			`{"store_instructions": "Sans olives", "cart": {"special_instructions": "Sans olives"}}`,
			"Sans olives",
		},
		{
			"livraison par l'établissement : consignes de livraison",
			`{"eater": {"delivery": {"notes": "Code 1234B, 3e étage"}}}`,
			"Livraison : Code 1234B, 3e étage",
		},
		{
			"couverts demandés",
			`{"packaging": {"disposable_items": {"should_include": true}}}`,
			"Couverts demandés",
		},
		{
			"sans couverts : rien à signaler",
			`{"packaging": {"disposable_items": {"should_include": false}}}`,
			"",
		},
		{
			"tout ensemble",
			`{"cart": {"special_instructions": "Sans oignons"},
			  "eater": {"delivery": {"notes": "Laisser à la porte"}},
			  "packaging": {"disposable_items": {"should_include": true}}}`,
			"Sans oignons\nLivraison : Laisser à la porte\nCouverts demandés",
		},
	}
	for _, c := range cases {
		order := parseOrder(t, c.raw)
		got := orderComment(order)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s : %q, want aucun commentaire", c.name, *got)
		case c.want != "" && (got == nil || *got != c.want):
			if got == nil {
				t.Errorf("%s : aucun commentaire, want %q", c.name, c.want)
			} else {
				t.Errorf("%s : %q, want %q", c.name, *got, c.want)
			}
		}
	}
}

func TestItemComment(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"aucun commentaire", `{"title": "Pizza"}`, ""},
		{"consignes de préparation (champ documenté)", `{"special_instructions": "  Sans oignons  "}`, "Sans oignons"},
		{
			"allergie : allergènes et consignes",
			`{"special_requests": [{"allergy": {
			   "allergens_to_exclude": [{"type": "GLUTEN"}, {"type": "PEANUTS"}],
			   "allergy_instructions": "Pas de traces"}}]}`,
			"Allergie : gluten, arachides — Pas de traces",
		},
		{
			"allergène libre",
			`{"special_requests": [{"allergy": {"allergens_to_exclude": [{"type": "OTHER", "freeform_text": "Céleri"}]}}]}`,
			"Allergie : Céleri",
		},
		{
			"allergie : consignes seules",
			`{"special_requests": [{"allergy": {"allergy_instructions": "Allergique aux noix"}}]}`,
			"Allergie : Allergique aux noix",
		},
		{
			"allergie d'abord, puis consignes de préparation",
			`{"special_instructions": "Bien cuite",
			  "special_requests": [{"allergy": {"allergens_to_exclude": [{"type": "SOY"}]}}]}`,
			"Allergie : soja | Bien cuite",
		},
		{
			"ancien format customer_request conservé",
			`{"customer_request": {"allergy": {"instructions": "Sans lait"}, "special_instructions": "Coupée en 8"}}`,
			"Sans lait | Coupée en 8",
		},
		{
			"même texte dans les deux formats : une seule fois",
			`{"special_instructions": "Coupée en 8", "customer_request": {"special_instructions": "Coupée en 8"}}`,
			"Coupée en 8",
		},
		{"allergie vide", `{"special_requests": [{"allergy": {}}]}`, ""},
	}
	for _, c := range cases {
		if got := itemComment(parseItem(t, c.raw)); got != c.want {
			t.Errorf("%s : %q, want %q", c.name, got, c.want)
		}
	}
}

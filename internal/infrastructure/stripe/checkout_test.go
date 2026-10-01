package stripeclient

import (
	"testing"
	"welloresto-api/internal/models"
)

func strPtr(s string) *string { return &s }

func TestOptionsDescription(t *testing.T) {
	config := &models.ProductConfiguration{
		Attributes: []models.ConfigurationAttribute{
			{ID: "accompagnement", Options: []models.ConfigurationOption{
				{ID: "1", Label: strPtr("Frites"), Selected: true},
				{ID: "2", Label: strPtr("Salade"), Selected: false},
			}},
			{ID: "boisson", Options: []models.ConfigurationOption{
				{ID: "3", Label: strPtr(" Coca "), ExtraPrice: 150, Selected: true},
				{ID: "4", Label: nil, ExtraPrice: 50, Selected: true},
			}},
		},
	}

	got := optionsDescription(selectedOptions(config))
	if got == nil || *got != "Frites, Coca (+1,50 €)" {
		t.Fatalf("description = %v, want %q", got, "Frites, Coca (+1,50 €)")
	}
}

func TestOptionsDescription_NoOptions(t *testing.T) {
	if got := optionsDescription(selectedOptions(nil)); got != nil {
		t.Fatalf("description = %q, want nil", *got)
	}

	unselected := &models.ProductConfiguration{
		Attributes: []models.ConfigurationAttribute{
			{ID: "a", Options: []models.ConfigurationOption{{ID: "1", Label: strPtr("Frites")}}},
		},
	}
	if got := optionsDescription(selectedOptions(unselected)); got != nil {
		t.Fatalf("description = %q, want nil", *got)
	}
}

func TestFormatEuros(t *testing.T) {
	cases := map[int]string{0: "0,00 €", 5: "0,05 €", 150: "1,50 €", 1200: "12,00 €"}
	for cents, want := range cases {
		if got := formatEuros(cents); got != want {
			t.Errorf("formatEuros(%d) = %q, want %q", cents, got, want)
		}
	}
}

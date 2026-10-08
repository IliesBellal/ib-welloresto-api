package order_life_cycle

import (
	"database/sql"
	"testing"

	"welloresto-api/internal/models"
)

// Lot D conformité caisse : le surcoût facturé d'une option est figé à
// l'écriture — celui porté par la commande, à défaut le catalogue, jamais le
// catalogue pour une plateforme.
func TestFreezeOptionPrice(t *testing.T) {
	costs := map[string]optionCostEntry{
		"opt-catalog": {ok: true, catalogPrice: sql.NullInt64{Int64: 150, Valid: true}},
		"opt-free":    {ok: true, catalogPrice: sql.NullInt64{Int64: 0, Valid: true}},
	}
	val := func(p *int) any {
		if p == nil {
			return nil
		}
		return *p
	}
	for _, tc := range []struct {
		name, brand, option string
		payload             int
		want                any
	}{
		{"caisse : prix envoyé", models.BrandWelloResto, "opt-catalog", 120, 120},
		{"borne / ScanNOrder sans prix : catalogue", models.BrandWelloResto, "opt-catalog", 0, 150},
		{"option gratuite au catalogue", models.BrandWelloResto, "opt-free", 0, 0},
		{"option inconnue du catalogue, sans prix", models.BrandWelloResto, "opt-unknown", 0, nil},
		{"Uber Eats : prix de la plateforme, même nul", models.BrandUberEats, "opt-catalog", 0, 0},
		{"Uber Eats : prix de la plateforme", models.BrandUberEats, "opt-catalog", 90, 90},
		{"Deliveroo : jamais le catalogue", models.BrandDeliveroo, "opt-catalog", 0, 0},
	} {
		if got := val(freezeOptionPrice(costs, tc.brand, tc.option, tc.payload)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

// Commande 38912 du registre 934 : une pizza à 8,70 €, total envoyé 20,70 €,
// payé en deux fois (8,70 puis 12,00).
func TestOrderConsistency_Order38912(t *testing.T) {
	c := orderConsistency{Price: 2070, Lines: 870, Paid: 2070, Brand: models.BrandWelloResto}
	if !c.priceMismatch() {
		t.Fatalf("priceMismatch = false, want true (total 2070 pour des lignes à 870)")
	}
	if c.overpaid() {
		t.Fatalf("overpaid = true, want false : le payé égale le total enregistré")
	}
}

func TestOrderConsistency_Cases(t *testing.T) {
	tests := []struct {
		name         string
		c            orderConsistency
		wantMismatch bool
		wantOverpaid bool
	}{
		{"cohérente", orderConsistency{Price: 870, Lines: 870, Paid: 870, Brand: models.BrandWelloResto}, false, false},
		{"livraison comptée", orderConsistency{Price: 1170, Lines: 870, DeliveryFees: 300, Brand: models.BrandWelloResto}, false, false},
		{"total trop haut", orderConsistency{Price: 2070, Lines: 870, Brand: models.BrandWelloResto}, true, false},
		{"total trop bas", orderConsistency{Price: 500, Lines: 870, Brand: models.BrandWelloResto}, true, false},
		{"trop-perçu", orderConsistency{Price: 870, Lines: 870, Paid: 1000, Brand: models.BrandWelloResto}, false, true},
		{"Uber Eats : prix plateforme, jamais signalé", orderConsistency{Price: 1099, Lines: 900, Paid: 1500, Brand: models.BrandUberEats}, false, false},
		{"Deliveroo : prix plateforme, jamais signalé", orderConsistency{Price: 1390, Lines: 1000, Brand: models.BrandDeliveroo}, false, false},
		{"commande vide payée à zéro", orderConsistency{Brand: models.BrandWelloResto}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.priceMismatch(); got != tc.wantMismatch {
				t.Errorf("priceMismatch = %v, want %v", got, tc.wantMismatch)
			}
			if got := tc.c.overpaid(); got != tc.wantOverpaid {
				t.Errorf("overpaid = %v, want %v", got, tc.wantOverpaid)
			}
		})
	}
}

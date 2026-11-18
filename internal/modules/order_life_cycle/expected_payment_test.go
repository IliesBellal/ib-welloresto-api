package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestResolveExpectedPayment(t *testing.T) {
	tests := []struct {
		name       string
		in         *models.ExpectedPaymentPayload
		wantWrite  bool
		wantMOP    string // "" = nil
		wantTender int    // 0 = nil
	}{
		{name: "champ absent : annonce gardée", in: nil, wantWrite: false},
		{name: "mop vide : effacer", in: &models.ExpectedPaymentPayload{MOP: ""}, wantWrite: true},
		{name: "mop blanc : effacer", in: &models.ExpectedPaymentPayload{MOP: "  "}, wantWrite: true},
		{name: "mop inconnu : effacé sans erreur", in: &models.ExpectedPaymentPayload{MOP: "BITCOIN"}, wantWrite: true},
		{name: "mop non encaissable en main propre : effacé", in: &models.ExpectedPaymentPayload{MOP: "STRIPE"}, wantWrite: true},
		{name: "sur compte client : gardé, sans montant remis", in: &models.ExpectedPaymentPayload{MOP: "account", TenderAmount: intPtr(5000)}, wantWrite: true, wantMOP: "ACCOUNT"},
		{name: "carte", in: &models.ExpectedPaymentPayload{MOP: "CB"}, wantWrite: true, wantMOP: "CB"},
		{name: "minuscules et espaces normalisés", in: &models.ExpectedPaymentPayload{MOP: " tr "}, wantWrite: true, wantMOP: "TR"},
		{name: "espèces sans montant", in: &models.ExpectedPaymentPayload{MOP: "ES"}, wantWrite: true, wantMOP: "ES"},
		{name: "espèces avec montant", in: &models.ExpectedPaymentPayload{MOP: "ES", TenderAmount: intPtr(5000)}, wantWrite: true, wantMOP: "ES", wantTender: 5000},
		{name: "espèces montant nul ignoré", in: &models.ExpectedPaymentPayload{MOP: "ES", TenderAmount: intPtr(0)}, wantWrite: true, wantMOP: "ES"},
		{name: "espèces montant négatif ignoré", in: &models.ExpectedPaymentPayload{MOP: "ES", TenderAmount: intPtr(-5)}, wantWrite: true, wantMOP: "ES"},
		{name: "montant ignoré hors espèces", in: &models.ExpectedPaymentPayload{MOP: "CB", TenderAmount: intPtr(5000)}, wantWrite: true, wantMOP: "CB"},
		{name: "montant ignoré quand le mop est effacé", in: &models.ExpectedPaymentPayload{MOP: "", TenderAmount: intPtr(5000)}, wantWrite: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			write, mop, tender := resolveExpectedPayment(tt.in)
			if write != tt.wantWrite {
				t.Fatalf("write = %v, veut %v", write, tt.wantWrite)
			}
			if tt.wantMOP == "" {
				if mop != nil {
					t.Fatalf("mop = %q, veut nil", *mop)
				}
			} else if mop == nil || *mop != tt.wantMOP {
				t.Fatalf("mop = %v, veut %q", mop, tt.wantMOP)
			}
			if tt.wantTender == 0 {
				if tender != nil {
					t.Fatalf("tender = %d, veut nil", *tender)
				}
			} else if tender == nil || *tender != tt.wantTender {
				t.Fatalf("tender = %v, veut %d", tender, tt.wantTender)
			}
		})
	}
}

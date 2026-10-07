package cash_registers

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestBuildRegisterVATReport(t *testing.T) {
	categories := []registerVATCategory{
		{tvaID: -1, deliveryType: "DELIVERY", label: "Livraison", title: "TVA Delivery fees 20%", rate: 20},
		{tvaID: 20, deliveryType: "IN", label: "Sur place", title: "TVA 20%", rate: 20},
		{tvaID: 10, deliveryType: "IN", label: "Sur place", title: "TVA 10%", rate: 10},
		{tvaID: 55, deliveryType: "TAKE_AWAY", label: "À emporter", title: "TVA 5.5%", rate: 5.5}, // aucune vente
	}
	bucketsByOrder := map[int64][]registerVATBucket{
		// Commande 1 : 20 € à 10 % + 10 € à 20 %, 3 € de remise (2 € / 1 €).
		1: {{tvaID: 10, rate: 10, ttc: 2000}, {tvaID: 20, rate: 20, ttc: 1000}},
		// Commande 2 : 4 € à 10 % + 6 € dans une catégorie masquée (0, absente
		// des catégories affichées), 2 € de remise : 0,80 € imputés au 10 %.
		2: {{tvaID: 10, rate: 10, ttc: 400}, {tvaID: 0, rate: 0, ttc: 600}},
		// Commande 3 : 3 € de frais de livraison à 20 %, sans remise.
		3: {{tvaID: -1, rate: 20, ttc: 300}},
	}
	discounts := map[int64]int64{1: 300, 2: 200}

	r := buildRegisterVATReport(bucketsByOrder, []int64{3, 1, 2}, discounts, categories)

	want := []models.CashReportLine{
		{DeliveryType: "IN", Label: "Sur place", TVATitle: "TVA 10%", Rate: 10, TTC: 1800 + 320, HT: 1927, TVA: 193},
		{DeliveryType: "IN", Label: "Sur place", TVATitle: "TVA 20%", Rate: 20, TTC: 900, HT: 750, TVA: 150},
		{DeliveryType: "TAKE_AWAY", Label: "À emporter", TVATitle: "TVA 5.5%", Rate: 5.5},
		// HT des frais = 300 × 100 / 120 = 250 (l'ancienne requête donnait 240).
		{DeliveryType: "DELIVERY", Label: "Livraison", TVATitle: "TVA Delivery fees 20%", Rate: 20, TTC: 300, HT: 250, TVA: 50},
	}
	if len(r.Lines) != len(want) {
		t.Fatalf("lignes = %+v, want %+v", r.Lines, want)
	}
	for i := range want {
		if r.Lines[i] != want[i] {
			t.Fatalf("ligne %d = %+v, want %+v", i, r.Lines[i], want[i])
		}
	}
	// Brut affiché : 2000 + 1000 + 400 + 300 ; remises imputées aux catégories
	// affichées : 300 + 80.
	if r.GrossTTC != 3700 || r.Discounts != 380 {
		t.Fatalf("brut = %d, remises = %d, want 3700 et 380", r.GrossTTC, r.Discounts)
	}
	var net int
	for _, l := range r.Lines {
		net += l.TTC
	}
	if net != r.GrossTTC-r.Discounts {
		t.Fatalf("somme des TTC = %d, want brut − remises = %d", net, r.GrossTTC-r.Discounts)
	}
}

func TestBuildRegisterVATReport_RateChangedWithinCategory(t *testing.T) {
	// Même catégorie, deux taux figés différents (taux de catégorie modifié
	// entre deux ventes) : HT calculé à chaque taux, puis sommé.
	categories := []registerVATCategory{{tvaID: 10, deliveryType: "IN", title: "TVA 10%", rate: 10}}
	bucketsByOrder := map[int64][]registerVATBucket{
		1: {{tvaID: 10, rate: 7, ttc: 1070}},
		2: {{tvaID: 10, rate: 10, ttc: 1100}},
	}
	r := buildRegisterVATReport(bucketsByOrder, []int64{1, 2}, nil, categories)
	if len(r.Lines) != 1 || r.Lines[0].TTC != 2170 || r.Lines[0].HT != 2000 || r.Lines[0].TVA != 170 {
		t.Fatalf("lignes = %+v, want TTC 2170, HT 1000 + 1000, TVA 170", r.Lines)
	}
}

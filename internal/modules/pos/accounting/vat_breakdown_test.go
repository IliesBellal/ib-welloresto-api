package accounting

import (
	"reflect"
	"testing"
)

func TestAllocateLargestRemainder(t *testing.T) {
	tests := []struct {
		name    string
		total   int64
		weights []int64
		want    []int64
	}{
		{"exemple du document : 3 € sur 20 € + 10 €", 300, []int64{2000, 1000}, []int64{200, 100}},
		{"reste attribué au plus grand reste", 100, []int64{1, 1, 1}, []int64{34, 33, 33}},
		{"poids négatif ignoré", 100, []int64{-500, 300, 100}, []int64{0, 75, 25}},
		{"total négatif", -100, []int64{1, 1, 1}, []int64{-34, -33, -33}},
		{"total nul", 0, []int64{5, 5}, []int64{0, 0}},
		{"aucun poids positif", 100, []int64{0, -1}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := allocateLargestRemainder(tc.total, tc.weights)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("allocateLargestRemainder(%d, %v) = %v, want %v", tc.total, tc.weights, got, tc.want)
			}
			var sum int64
			for _, s := range got {
				sum += s
			}
			if got != nil && sum != tc.total {
				t.Fatalf("somme des parts = %d, want %d", sum, tc.total)
			}
		})
	}
}

func ttcByTitle(rows []TVARow) map[string]float64 {
	out := map[string]float64{}
	for _, r := range rows {
		out[r.TVATitle] += r.TTC
	}
	return out
}

func TestBuildManualVAT_DiscountProrated(t *testing.T) {
	// Commande 1 : 20 € à 10 %, 10 € à 20 %, 3 € de « Réduction montant ».
	// Commande 2 : 5 € à 10 %, sans remise.
	// Commande 3 : 4 € à 10 % et 6 € dans une catégorie masquée, 2 € de remise
	// répartis sur les deux (0,80 € visible, 1,20 € masqué).
	lines := []orderVATLine{
		{OrderID: 1, Title: "TVA 10", Rate: 10, Reported: true, TTC: 2000},
		{OrderID: 1, Title: "TVA 20", Rate: 20, Reported: true, TTC: 1000},
		{OrderID: 2, Title: "TVA 10", Rate: 10, Reported: true, TTC: 500},
		{OrderID: 3, Title: "TVA 10", Rate: 10, Reported: true, TTC: 400},
		{OrderID: 3, Title: "TVA Undefined", Rate: 0, Reported: false, TTC: 600},
	}
	payments := []orderPaymentLine{
		{OrderID: 1, MOP: "CB", Label: "Carte bancaire", Amount: 2700},
		{OrderID: 1, MOP: "CURRENCY", Label: "Réduction montant", Amount: 300},
		{OrderID: 2, MOP: "ES", Label: "Espèce", Amount: 500},
		{OrderID: 3, MOP: "PERCENTAGE", Label: "Réduction pourcentage", Amount: 200},
	}

	rows, summary := buildManualVAT(lines, payments)
	got := ttcByTitle(rows)
	want := map[string]float64{"TVA 10": 1800 + 500 + 320, "TVA 20": 900}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TTC par catégorie = %v, want %v", got, want)
	}
	if summary != (DiscountSummary{TTCBeforeDiscounts: 3900, Discounts: 380, TTC: 3520}) {
		t.Fatalf("résumé remises = %+v", summary)
	}
}

func TestBuildManualVAT_DiscountCappedAtOrderTotal(t *testing.T) {
	lines := []orderVATLine{{OrderID: 1, Title: "TVA 10", Rate: 10, Reported: true, TTC: 500}}
	payments := []orderPaymentLine{{OrderID: 1, MOP: "CURRENCY", Amount: 800}}
	rows, summary := buildManualVAT(lines, payments)
	if len(rows) != 1 || rows[0].TTC != 0 || summary.Discounts != 500 {
		t.Fatalf("remise supérieure à la commande : rows=%+v summary=%+v, want TTC 0 et remise plafonnée à 500", rows, summary)
	}
}

func TestBuildAutoReport_TTCEqualsPayments(t *testing.T) {
	lines := []orderVATLine{
		// Commande 1 : 20 € à 10 % + 10 € à 20 %, payée 27 € (3 € de remise).
		{OrderID: 1, Title: "TVA 10", Rate: 10, Reported: true, TTC: 2000},
		{OrderID: 1, Title: "TVA 20", Rate: 20, Reported: true, TTC: 1000},
		// Commande 2 : 10 € à 5,5 % + 2 € de frais à 20 %, payée en deux fois.
		{OrderID: 2, Title: "TVA 5.5", Rate: 5.5, Reported: true, TTC: 1000},
		{OrderID: 2, Title: "TVA 20", Rate: 20, Reported: true, TTC: 200},
		// Commande 3 : catégorie masquée d'ordinaire, mais payée : elle doit
		// apparaître pour que chaque euro tombe dans un taux.
		{OrderID: 3, Title: "TVA Undefined", Rate: 0, Reported: false, TTC: 400},
		// Commande 4 : close mais jamais payée → aucune TVA.
		{OrderID: 4, Title: "TVA 10", Rate: 10, Reported: true, TTC: 999},
	}
	payments := []orderPaymentLine{
		{OrderID: 1, MOP: "CB", Label: "Carte bancaire", Amount: 2700},
		{OrderID: 1, MOP: "CURRENCY", Label: "Réduction montant", Amount: 300},
		{OrderID: 2, MOP: "ES", Label: "Espèce", Amount: 700},
		{OrderID: 2, MOP: "CB", Label: "Carte bancaire", Amount: 500},
		{OrderID: 3, MOP: "STRIPE", Label: "ScanNOrder", Amount: 400},
		// Commande 5 : payée sans aucune ligne → non répartissable.
		{OrderID: 5, MOP: "ES", Label: "Espèce", Amount: 1000},
	}

	r := buildAutoReport(lines, payments)

	var tvaTTC, payTotal int64
	for _, row := range r.TVARows {
		tvaTTC += int64(row.TTC)
	}
	for _, p := range r.Payments {
		payTotal += p.Amount
	}
	if tvaTTC != payTotal || tvaTTC != 2700+1200+400 {
		t.Fatalf("TTC TVA = %d, encaissements = %d, want égaux à 4300", tvaTTC, payTotal)
	}
	got := ttcByTitle(r.TVARows)
	want := map[string]float64{"TVA 10": 1800, "TVA 20": 900 + 200, "TVA 5.5": 1000, "TVA Undefined": 400}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TTC par catégorie = %v, want %v", got, want)
	}
	wantPayments := []PaymentRow{{Label: "Carte bancaire", Amount: 3200}, {Label: "Espèce", Amount: 700}, {Label: "ScanNOrder", Amount: 400}}
	if !reflect.DeepEqual(r.Payments, wantPayments) {
		t.Fatalf("encaissements = %+v, want %+v", r.Payments, wantPayments)
	}
	if r.Summary != (DiscountSummary{TTCBeforeDiscounts: 4600, Discounts: 300, TTC: 4300}) {
		t.Fatalf("résumé remises = %+v", r.Summary)
	}
	if !reflect.DeepEqual(r.UnallocatedOrders, []int64{5}) {
		t.Fatalf("commandes non répartissables = %v, want [5]", r.UnallocatedOrders)
	}
}

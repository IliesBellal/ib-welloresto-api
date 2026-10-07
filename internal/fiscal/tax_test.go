package fiscal

import (
	"testing"
)

func TestBuildTaxDetails_RatesFeesAndDiscount(t *testing.T) {
	// Deux plats à 10 % (12 € + 8 €), une boisson à 20 % (5 €), livraison à
	// 20 % (3 €) ; remise de caisse de 2,80 € répartie au prorata.
	d := BuildTaxDetails([]TaxLine{{10, 1200}, {10, 800}, {20, 500}, {20, 300}}, 280)

	if d.Discount != 280 || d.TotalTTC() != 2800-280 {
		t.Fatalf("totals: discount=%d ttc=%d, want 280 / 2520", d.Discount, d.TotalTTC())
	}
	if len(d.Lines) != 2 || d.Lines[0].Rate != 10 || d.Lines[1].Rate != 20 {
		t.Fatalf("expected two lines sorted by rate, got %+v", d.Lines)
	}
	// 10 % : 2000 − 200 = 1800 ; 20 % : 800 − 80 = 720.
	if d.Lines[0].TTC != 1800 || d.Lines[1].TTC != 720 {
		t.Fatalf("discount not prorated: %+v", d.Lines)
	}
	for _, l := range d.Lines {
		if l.HT+l.TVA != l.TTC {
			t.Fatalf("HT + TVA != TTC on %+v", l)
		}
	}
	if d.Lines[0].HT != 1636 || d.Lines[1].HT != 600 {
		t.Fatalf("HT: got %d / %d, want 1636 / 600", d.Lines[0].HT, d.Lines[1].HT)
	}
}

func TestBuildTaxDetails_DiscountCappedAndOdd(t *testing.T) {
	// Remise supérieure au total : plafonnée, ticket à zéro.
	if d := BuildTaxDetails([]TaxLine{{5.5, 300}}, 500); d.Discount != 300 || len(d.Lines) != 0 {
		t.Fatalf("capped discount: %+v", d)
	}
	// Remise d'un centime sur trois taux égaux : la somme reste exacte.
	d := BuildTaxDetails([]TaxLine{{5.5, 100}, {10, 100}, {20, 100}}, 1)
	if d.TotalTTC() != 299 {
		t.Fatalf("expected 299, got %d", d.TotalTTC())
	}
	// Aucune ligne : ventilation vide, jamais nulle (JSON lines: []).
	if d := BuildTaxDetails(nil, 0); d.Lines == nil || len(d.Lines) != 0 {
		t.Fatalf("empty: %+v", d)
	}
}

func TestProrateTaxDetails_RefundSumsExactly(t *testing.T) {
	orig := BuildTaxDetails([]TaxLine{{10, 1800}, {20, 721}}, 0)
	got := ProrateTaxDetails(orig, -1000)
	if got.TotalTTC() != -1000 {
		t.Fatalf("refund total: got %d, want -1000", got.TotalTTC())
	}
	for _, l := range got.Lines {
		if l.TTC >= 0 || l.HT+l.TVA != l.TTC {
			t.Fatalf("refund line: %+v", l)
		}
	}
	if got.Discount != 0 {
		t.Fatalf("refund carries no discount, got %d", got.Discount)
	}
	if empty := ProrateTaxDetails(TaxDetails{}, -500); len(empty.Lines) != 0 {
		t.Fatalf("no original breakdown: %+v", empty)
	}
}

func TestParseTaxDetails(t *testing.T) {
	if _, ok := ParseTaxDetails([]byte(`{}`)); ok {
		t.Fatal("'{}' (tickets antérieurs) must not parse as a breakdown")
	}
	d, ok := ParseTaxDetails([]byte(`{"lines":[{"rate":10,"ttc":110,"ht":100,"tva":10}],"discount":0}`))
	if !ok || d.TotalHT() != 100 || d.TotalTVA() != 10 {
		t.Fatalf("parse: %+v %v", d, ok)
	}
}

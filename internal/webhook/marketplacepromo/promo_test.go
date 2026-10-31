package marketplacepromo

import (
	"reflect"
	"testing"
)

func lineTotal(lines []Line) int { return total(lines) }

// Commande réelle 0574D (Uber Eats, 28/10/2026) : deux paires de pizzas, une
// offerte dans chaque paire (BOGO), 74,68 € de lignes pour 46,19 € payés.
func TestAllocate_BOGO_commandeReelle(t *testing.T) {
	lines := []Line{
		{Quantity: 2, UnitPrice: 1399}, // 4 Fromage
		{Quantity: 2, UnitPrice: 1450}, // Reine
		{Quantity: 1, UnitPrice: 1320}, // Chèvre-Miel
		{Quantity: 1, UnitPrice: 450},  // Oasis
	}
	targets := []Target{{Line: 1, Units: 1}, {Line: 0, Units: 1}}

	got := Allocate(lines, targets, 7468-4619)

	if got.Method != MethodTargeted {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodTargeted)
	}
	want := []Part{
		{Line: 0, Quantity: 1, UnitPrice: 1399, Price: 1399},
		{Line: 0, Quantity: 1, UnitPrice: 1399, Price: 0},
		{Line: 1, Quantity: 1, UnitPrice: 1450, Price: 1450},
		{Line: 1, Quantity: 1, UnitPrice: 1450, Price: 0},
		{Line: 2, Quantity: 1, UnitPrice: 1320, Price: 1320},
		{Line: 3, Quantity: 1, UnitPrice: 450, Price: 450},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
	if got.Total() != 4619 {
		t.Fatalf("total = %d, want 4619", got.Total())
	}
}

// Commande FABD1 : le prix unitaire (15,89 €) inclut 1,90 € d'option, la
// promotion n'offre que le prix de base (13,99 €).
func TestAllocate_BOGO_optionPayanteNonOfferte(t *testing.T) {
	lines := []Line{{Quantity: 2, UnitPrice: 1589, Cap: 1399}}

	got := Allocate(lines, []Target{{Line: 0, Units: 1}}, 3178-1779)

	if got.Method != MethodTargeted {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodTargeted)
	}
	want := []Part{
		{Line: 0, Quantity: 1, UnitPrice: 1589, Price: 1589},
		{Line: 0, Quantity: 1, UnitPrice: 1589, Price: 190},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
	if got.Total() != 1779 {
		t.Fatalf("total = %d, want 1779", got.Total())
	}
}

func TestAllocate_unitesDesigneesMontantDifferent_auProrata(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 1000}, {Quantity: 1, UnitPrice: 3000}}
	// la plateforme désigne les deux articles mais ne retire que 800 au total
	got := Allocate(lines, []Target{{Line: 0, Units: 1}, {Line: 1, Units: 1}}, 800)

	if got.Method != MethodTargetedProrata {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodTargetedProrata)
	}
	if got.Total() != 4000-800 {
		t.Fatalf("total = %d, want 3200", got.Total())
	}
	// 800 réparti 1:3 → 200 et 600
	want := []Part{
		{Line: 0, Quantity: 1, UnitPrice: 1000, Price: 800},
		{Line: 1, Quantity: 1, UnitPrice: 3000, Price: 2400},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
}

func TestAllocate_sansUniteDesignee_repartiSurToutesLesLignes(t *testing.T) {
	// 20 % sur le panier : 1000 + 2×500 = 2000, remise 400
	lines := []Line{{Quantity: 1, UnitPrice: 1000}, {Quantity: 2, UnitPrice: 500}}

	got := Allocate(lines, nil, 400)

	if got.Method != MethodSpread {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodSpread)
	}
	want := []Part{
		{Line: 0, Quantity: 1, UnitPrice: 1000, Price: 800},
		{Line: 1, Quantity: 2, UnitPrice: 500, Price: 400},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
	if got.Total() != 1600 {
		t.Fatalf("total = %d, want 1600", got.Total())
	}
}

// Une remise qui ne tombe pas juste par unité scinde la ligne, au centime près.
func TestAllocate_centimesNonDivisibles_scindeLaLigne(t *testing.T) {
	lines := []Line{{Quantity: 3, UnitPrice: 1000}}

	got := Allocate(lines, nil, 100)

	want := []Part{
		{Line: 0, Quantity: 2, UnitPrice: 1000, Price: 967},
		{Line: 0, Quantity: 1, UnitPrice: 1000, Price: 966},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
	if got.Total() != 2900 {
		t.Fatalf("total = %d, want 2900", got.Total())
	}
}

func TestAllocate_unitesDesigneesInsuffisantes_repliSurToutesLesLignes(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 500}, {Quantity: 1, UnitPrice: 1500}}
	// une seule unité désignée (500) mais 1000 de remise
	got := Allocate(lines, []Target{{Line: 0, Units: 1}}, 1000)

	if got.Method != MethodSpread {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodSpread)
	}
	if got.Total() != 1000 {
		t.Fatalf("total = %d, want 1000", got.Total())
	}
}

func TestAllocate_remiseNulle_ligneInchangee(t *testing.T) {
	lines := []Line{{Quantity: 2, UnitPrice: 700}}

	got := Allocate(lines, []Target{{Line: 0, Units: 1}}, 0)

	if got.Method != MethodNone {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodNone)
	}
	want := []Part{{Line: 0, Quantity: 2, UnitPrice: 700, Price: 700}}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
}

func TestAllocate_remiseSuperieureAuxLignes_rejetee(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 500}}

	got := Allocate(lines, nil, 501)

	if got.Method != MethodRejected {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodRejected)
	}
	if got.Total() != 500 {
		t.Fatalf("total = %d, want 500 (lignes inchangées)", got.Total())
	}
}

func TestAllocate_cibleInvalideIgnoree(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 900}}

	got := Allocate(lines, []Target{{Line: 5, Units: 1}, {Line: -1, Units: 1}, {Line: 0, Units: 0}}, 300)

	if got.Method != MethodSpread {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodSpread)
	}
	if got.Total() != 600 {
		t.Fatalf("total = %d, want 600", got.Total())
	}
}

func TestAllocate_unitesDesigneesPlafonneesALaQuantite(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 800}}

	// la plateforme parle de 3 unités, la ligne n'en a qu'une
	got := Allocate(lines, []Target{{Line: 0, Units: 3}}, 800)

	if got.Method != MethodTargeted {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodTargeted)
	}
	if got.Total() != 0 {
		t.Fatalf("total = %d, want 0", got.Total())
	}
}

func TestProrata_poidsPersonnalises(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 1000}, {Quantity: 1, UnitPrice: 1000}}

	// seule la première ligne est remisée par la plateforme
	got := Prorata(lines, []int64{500, 0}, 500)

	want := []Part{
		{Line: 0, Quantity: 1, UnitPrice: 1000, Price: 500},
		{Line: 1, Quantity: 1, UnitPrice: 1000, Price: 1000},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parties = %+v\nwant     %+v", got.Parts, want)
	}
}

func TestProrata_poidsQuiDepasseraientUneLigne_repliSurLesMontants(t *testing.T) {
	lines := []Line{{Quantity: 1, UnitPrice: 300}, {Quantity: 1, UnitPrice: 700}}

	// tous les poids sur la 1re ligne (300 €) alors que la remise est de 500
	got := Prorata(lines, []int64{500, 0}, 500)

	if got.Method != MethodSpread {
		t.Fatalf("méthode = %s, want %s", got.Method, MethodSpread)
	}
	if got.Total() != 500 {
		t.Fatalf("total = %d, want 500", got.Total())
	}
}

// Quelle que soit la répartition, la somme des parties retombe sur les lignes
// moins la remise, et aucun prix n'est négatif.
func TestAllocate_invariants(t *testing.T) {
	lines := []Line{
		{Quantity: 3, UnitPrice: 1389, Cap: 1199},
		{Quantity: 1, UnitPrice: 457},
		{Quantity: 4, UnitPrice: 333},
	}
	for discount := 0; discount <= lineTotal(lines); discount += 37 {
		for _, targets := range [][]Target{nil, {{Line: 0, Units: 2}}, {{Line: 0, Units: 1}, {Line: 2, Units: 3}}} {
			got := Allocate(lines, targets, discount)
			if got.Method == MethodRejected {
				t.Fatalf("remise %d rejetée alors qu'elle tient dans les lignes", discount)
			}
			if want := lineTotal(lines) - discount; got.Total() != want {
				t.Fatalf("remise %d, cibles %v : total %d, want %d", discount, targets, got.Total(), want)
			}
			qty := make([]int, len(lines))
			for _, p := range got.Parts {
				if p.Price < 0 || p.Price > p.UnitPrice {
					t.Fatalf("remise %d : prix %d hors [0 ; %d]", discount, p.Price, p.UnitPrice)
				}
				qty[p.Line] += p.Quantity
			}
			for i, l := range lines {
				if qty[i] != l.Quantity {
					t.Fatalf("remise %d : ligne %d a %d unités, want %d", discount, i, qty[i], l.Quantity)
				}
			}
		}
	}
}

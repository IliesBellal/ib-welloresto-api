package tasks

import (
	"math"
	"sort"
	"testing"
)

// weightedDailySales simule une vente par jour pendant days jours, chaque
// vente au milieu de sa journée, avec le poids utilisé par la requête SQL.
func weightedDailySales(days int) float64 {
	sum := 0.0
	for k := 0; k < days; k++ {
		sum += math.Pow(0.5, (float64(k)+0.5)/popularHalfLifeDays)
	}
	return sum
}

func selectedIDs(candidates []popularCandidate) []int64 {
	ids := make([]int64, 0)
	for id := range selectPopularProducts(candidates) {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func assertIDs(t *testing.T, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("selected = %v, want %v", got, want)
		}
	}
}

// category construit une catégorie de len(scores) produits, d'IDs
// firstID, firstID+1, ..., chacun avec assez de commandes réelles pour que
// seul le score décide.
func category(name string, firstID int64, scores ...float64) []popularCandidate {
	out := make([]popularCandidate, len(scores))
	for i, s := range scores {
		out[i] = popularCandidate{ProductID: firstID + int64(i), Category: name, Score: s, Orders: 10}
	}
	return out
}

func TestPopularScore_RegularDailySaleIsWindowLength(t *testing.T) {
	got := popularScore(weightedDailySales(popularWindowDays), 365)
	if math.Abs(got-popularWindowDays) > 0.5 {
		t.Fatalf("score = %.2f, want about %d (one order a day over the window)", got, popularWindowDays)
	}
}

func TestPopularScore_RecentProductExtrapolatedOnPresence(t *testing.T) {
	// Présent depuis 10 jours, vendu une fois par jour : même rythme qu'un
	// produit installé, donc même score.
	got := popularScore(weightedDailySales(10), 10)
	if math.Abs(got-popularWindowDays) > 0.5 {
		t.Fatalf("score = %.2f, want about %d", got, popularWindowDays)
	}
}

func TestPopularScore_MinimumExposure(t *testing.T) {
	// Créé hier avec 1 vente : extrapolé sur 7 jours, pas sur 1.
	got := popularScore(weightedDailySales(1), 1)
	want := popularScore(weightedDailySales(1), popularMinExposureDays)
	if got != want {
		t.Fatalf("score = %.2f, want %.2f (exposure floored at %.0f days)", got, want, popularMinExposureDays)
	}
	if onFullWindow := popularScore(weightedDailySales(1), 365); !(got > onFullWindow) {
		t.Fatalf("score = %.2f, want above %.2f (recent product extrapolated)", got, onFullWindow)
	}
}

func TestPopularScore_OlderSalesWeighLess(t *testing.T) {
	recent := popularScore(math.Pow(0.5, 1/popularHalfLifeDays), 365)
	old := popularScore(math.Pow(0.5, 20/popularHalfLifeDays), 365)
	if !(recent > old) {
		t.Fatalf("recent = %.2f, old = %.2f: an older sale must weigh less", recent, old)
	}
	if popularScore(0, 365) != 0 {
		t.Fatal("no sale must give a score of 0")
	}
}

func TestPopularCategoryLimit(t *testing.T) {
	cases := map[int]int{1: 1, 2: 1, 4: 1, 7: 1, 8: 2, 11: 2, 12: 3, 40: 3}
	for size, want := range cases {
		if got := popularCategoryLimit(size); got != want {
			t.Errorf("popularCategoryLimit(%d) = %d, want %d", size, got, want)
		}
	}
}

func TestSelectPopular_CapByCategorySize(t *testing.T) {
	// 8 produits qui se vendent tous bien : plafond de 2.
	got := selectedIDs(category("pizzas", 1, 30, 29, 28, 27, 26, 25, 24, 23))
	assertIDs(t, got, 1, 2)
}

func TestSelectPopular_DominantCategoryDoesNotStarveOthers(t *testing.T) {
	// L'ancien top 10 global aurait tout donné aux pizzas.
	candidates := append(category("pizzas", 1, 90, 85, 80, 75, 70, 65, 60, 55, 50, 45, 40, 35),
		category("desserts", 100, 9, 3, 2, 1)...)
	candidates = append(candidates, category("boissons", 200, 6, 5, 1, 1)...)

	got := selectedIDs(candidates)
	assertIDs(t, got, 1, 2, 3, 100, 200)
}

func TestSelectPopular_AbsoluteThreshold(t *testing.T) {
	// Meilleur produit sous le seuil : pas de populaire dans la catégorie.
	got := selectedIDs(category("sauces", 1, popularMinScore-0.1, 1, 0, 0))
	assertIDs(t, got)
}

func TestSelectPopular_MinOrders(t *testing.T) {
	// Produit créé hier, 1 vente : son score extrapolé dépasse le seuil
	// (environ 4,6), mais une seule commande ne prouve rien.
	oneSale := popularScore(weightedDailySales(1), 1)
	if oneSale < popularMinScore {
		t.Fatalf("precondition: one-sale score %.2f should exceed %.0f", oneSale, popularMinScore)
	}
	candidates := []popularCandidate{{ProductID: 1, Category: "nouveautés", Score: oneSale, Orders: 1}}
	assertIDs(t, selectedIDs(candidates))

	candidates[0].Orders = popularMinOrders
	assertIDs(t, selectedIDs(candidates), 1)

	// La règle vaut aussi pour un produit déjà populaire.
	candidates[0].Orders, candidates[0].WasPopular = popularMinOrders-1, true
	assertIDs(t, selectedIDs(candidates))
}

func TestSelectPopular_LeaderRatio(t *testing.T) {
	// 12 produits (plafond 3) : le 2e atteint 60 % du 1er, le 3e 40 %.
	got := selectedIDs(category("plats", 1, 50, 30, 20, 1, 1, 1, 1, 1, 1, 1, 1, 1))
	assertIDs(t, got, 1, 2)
}

func TestSelectPopular_HomogeneousCategoryKeepsAllUpToCap(t *testing.T) {
	got := selectedIDs(category("plats", 1, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9))
	assertIDs(t, got, 1, 2, 3)
}

func TestSelectPopular_HysteresisOnAbsoluteThreshold(t *testing.T) {
	score := popularMinScore * 0.8 // sous le seuil, au-dessus de 75 % du seuil

	newcomer := category("desserts", 1, score)
	assertIDs(t, selectedIDs(newcomer))

	incumbent := category("desserts", 1, score)
	incumbent[0].WasPopular = true
	assertIDs(t, selectedIDs(incumbent), 1)

	dropped := category("desserts", 1, popularMinScore*0.7)
	dropped[0].WasPopular = true
	assertIDs(t, selectedIDs(dropped))
}

func TestSelectPopular_HysteresisOnLeaderRatio(t *testing.T) {
	// 8 produits (plafond 2) : le 2e à 45 % du 1er.
	candidates := category("plats", 1, 40, 18, 1, 1, 1, 1, 1, 1)
	assertIDs(t, selectedIDs(candidates), 1)

	candidates[1].WasPopular = true
	assertIDs(t, selectedIDs(candidates), 1, 2)
}

func TestSelectPopular_IncumbentWinsCloseCall(t *testing.T) {
	// 4 produits (plafond 1).
	close := category("desserts", 1, 10.5, 10, 1, 1)
	close[1].WasPopular = true
	assertIDs(t, selectedIDs(close), 2)

	clear := category("desserts", 1, 12, 10, 1, 1)
	clear[1].WasPopular = true
	assertIDs(t, selectedIDs(clear), 1)
}

func TestSelectPopular_TiesBrokenByProductID(t *testing.T) {
	candidates := []popularCandidate{
		{ProductID: 9, Category: "c", Score: 10, Orders: 10},
		{ProductID: 3, Category: "c", Score: 10, Orders: 10},
		{ProductID: 5, Category: "c", Score: 10, Orders: 10},
		{ProductID: 7, Category: "c", Score: 1, Orders: 10},
	}
	for i := 0; i < 20; i++ {
		assertIDs(t, selectedIDs(candidates), 3)
	}
}

func TestSelectPopular_Empty(t *testing.T) {
	assertIDs(t, selectedIDs(nil))
}

package tasks

import "testing"

// Comme en PHP, la simulation exige au moins avgDistMinItems LIGNES
// d'orderitems (count($order_items) < 5) ET avgDistMinItems items
// effectivement traités (total_items_processed < 5).

func TestSimulateAverageDistributionTime_NotEnoughLines(t *testing.T) {
	// 2 lignes seulement, même si les quantités cumulées dépassent 5.
	items := []distributionItem{
		{quantity: 3, orderedTS: 1000, turnaroundS: 120},
		{quantity: 4, orderedTS: 1010, turnaroundS: 180},
	}
	avg, processed := simulateAverageDistributionTime(items, 2)
	if avg != 0 || processed != 0 {
		t.Fatalf("attendu (0, 0) avec moins de %d lignes, obtenu (%d, %d)", avgDistMinItems, avg, processed)
	}
}

func TestSimulateAverageDistributionTime_NotEnoughProcessedItems(t *testing.T) {
	// 5 lignes mais des quantités/turnarounds invalides : moins de 5 items
	// effectivement traités → pas de moyenne.
	items := []distributionItem{
		{quantity: 0, orderedTS: 1000, turnaroundS: 120},
		{quantity: -2, orderedTS: 1000, turnaroundS: 120},
		{quantity: 1, orderedTS: 1000, turnaroundS: 0},
		{quantity: 1, orderedTS: 1000, turnaroundS: 100},
		{quantity: 1, orderedTS: 1000, turnaroundS: 100},
	}
	avg, processed := simulateAverageDistributionTime(items, 2)
	if avg != 0 || processed != 0 {
		t.Fatalf("attendu (0, 0) avec moins de %d items traités, obtenu (%d, %d)", avgDistMinItems, avg, processed)
	}
}

func TestSimulateAverageDistributionTime_WeightedMean(t *testing.T) {
	// 5 lignes qty 1 (60+80+100+120+140 = 500s) + 1 ligne qty 5 à 50s/item
	// (250s) → total 750s sur 10 items → moyenne pondérée 75s.
	items := []distributionItem{
		{quantity: 1, orderedTS: 1000, turnaroundS: 60},
		{quantity: 1, orderedTS: 1010, turnaroundS: 80},
		{quantity: 1, orderedTS: 1020, turnaroundS: 100},
		{quantity: 1, orderedTS: 1030, turnaroundS: 120},
		{quantity: 1, orderedTS: 1040, turnaroundS: 140},
		{quantity: 5, orderedTS: 1050, turnaroundS: 250},
	}
	avg, processed := simulateAverageDistributionTime(items, 3)
	if processed != 10 {
		t.Fatalf("attendu 10 items traités, obtenu %d", processed)
	}
	if avg != 75 {
		t.Fatalf("attendu moyenne 75s, obtenu %d", avg)
	}
}

func TestSimulateAverageDistributionTime_ClampFloor(t *testing.T) {
	// 5 lignes qty 2, perItem = round(8/2) = 4s → moyenne 4s → bornée à
	// avgDistFloorSec. (le filtre SQL exclut normalement ces turnarounds,
	// ceinture-bretelles)
	items := []distributionItem{
		{quantity: 2, orderedTS: 1000, turnaroundS: 8},
		{quantity: 2, orderedTS: 1010, turnaroundS: 8},
		{quantity: 2, orderedTS: 1020, turnaroundS: 8},
		{quantity: 2, orderedTS: 1030, turnaroundS: 8},
		{quantity: 2, orderedTS: 1040, turnaroundS: 8},
	}
	avg, processed := simulateAverageDistributionTime(items, 1)
	if processed != 10 {
		t.Fatalf("attendu 10 items traités, obtenu %d", processed)
	}
	if avg != avgDistFloorSec {
		t.Fatalf("attendu moyenne bornée à %ds, obtenu %d", avgDistFloorSec, avg)
	}
}

func TestSimulateAverageDistributionTime_ClampCeil(t *testing.T) {
	// perItem = 1500s → moyenne 1500s → bornée à avgDistCeilSec.
	items := []distributionItem{
		{quantity: 1, orderedTS: 1000, turnaroundS: 1500},
		{quantity: 1, orderedTS: 1001, turnaroundS: 1500},
		{quantity: 1, orderedTS: 1002, turnaroundS: 1500},
		{quantity: 1, orderedTS: 1003, turnaroundS: 1500},
		{quantity: 1, orderedTS: 1004, turnaroundS: 1500},
	}
	avg, processed := simulateAverageDistributionTime(items, 2)
	if processed != 5 {
		t.Fatalf("attendu 5 items traités, obtenu %d", processed)
	}
	if avg != avgDistCeilSec {
		t.Fatalf("attendu moyenne bornée à %ds, obtenu %d", avgDistCeilSec, avg)
	}
}

func TestSimulateAverageDistributionTime_CapacityDoesNotPanic(t *testing.T) {
	// Garde-fou : capacité invalide (0 ou négative) ne doit jamais paniquer
	// et la moyenne ne dépend pas de la capacité.
	items := []distributionItem{
		{quantity: 1, orderedTS: 1000, turnaroundS: 100},
		{quantity: 1, orderedTS: 1010, turnaroundS: 100},
		{quantity: 1, orderedTS: 1020, turnaroundS: 100},
		{quantity: 1, orderedTS: 1030, turnaroundS: 100},
		{quantity: 1, orderedTS: 1040, turnaroundS: 100},
	}
	for _, capacity := range []int{0, -1, 1, 100} {
		avg, processed := simulateAverageDistributionTime(items, capacity)
		if processed != 5 || avg != 100 {
			t.Fatalf("capacité %d : attendu (100, 5), obtenu (%d, %d)", capacity, avg, processed)
		}
	}
}

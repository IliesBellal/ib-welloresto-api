package tasks

import (
	"database/sql"
	"math"
	"testing"
)

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

// --- smoothDistributionTime (lissage par confiance + plafonnement) --------

func TestSmoothDistributionTime_NoPrevious_ReturnsRawAsIs(t *testing.T) {
	// Tout premier calcul du marchand : rien à lisser ni à plafonner.
	got := smoothDistributionTime(sql.NullInt64{}, 500, 5)
	if got != 500 {
		t.Fatalf("attendu 500 (valeur brute) sans historique, obtenu %d", got)
	}
}

func TestSmoothDistributionTime_LowSample_StaysCloseToPrevious(t *testing.T) {
	// Cas réel du ticket : matin creux, 5 items, la moyenne brute chute de
	// 20min (1200s, borne haute) à 8min (480s). Avec peu d'items, alpha est
	// faible : la valeur lissée doit rester proche de l'ancienne (1200s),
	// pas sauter directement à 480s.
	previous := sql.NullInt64{Valid: true, Int64: 1200}
	got := smoothDistributionTime(previous, 480, avgDistMinItems)

	alpha := float64(avgDistMinItems) / (float64(avgDistMinItems) + avgDistConfidenceK)
	wantUnclamped := int64(math.Round(1200*(1-alpha) + 480*alpha))
	maxDelta := int64(math.Round(math.Max(avgDistMaxDeltaAbsoluteSec, 1200*avgDistMaxDeltaRelative)))
	want := wantUnclamped
	if want < 1200-maxDelta {
		want = 1200 - maxDelta
	}

	if got != want {
		t.Fatalf("attendu %d (alpha=%.3f), obtenu %d", want, alpha, got)
	}
	if got >= 1200 || got <= 480 {
		t.Fatalf("attendu une valeur strictement entre l'ancienne (1200) et la brute (480), obtenu %d", got)
	}
}

func TestSmoothDistributionTime_ClampCapsMaxDeltaOnLowSample(t *testing.T) {
	// Avec le plafonnement (piste 3), même un échantillon minimal ne peut
	// pas faire bouger la moyenne de plus que le garde-fou en un cycle.
	previous := sql.NullInt64{Valid: true, Int64: 1200}
	got := smoothDistributionTime(previous, 480, avgDistMinItems)

	maxDelta := int64(math.Round(math.Max(avgDistMaxDeltaAbsoluteSec, 1200*avgDistMaxDeltaRelative)))
	minAllowed := 1200 - maxDelta
	if got < minAllowed {
		t.Fatalf("plafonnement violé : obtenu %d, minimum autorisé %d", got, minAllowed)
	}
}

func TestSmoothDistributionTime_HighSample_ConvergesFastTowardRaw(t *testing.T) {
	// Gros échantillon (coup de feu) : alpha proche de 1, la moyenne doit
	// suivre la réalité de près, sans être bridée artificiellement si le
	// déplacement respecte tout de même le plafond de variation.
	previous := sql.NullInt64{Valid: true, Int64: 100}
	got := smoothDistributionTime(previous, 120, 500) // alpha = 500/525 ≈ 0.952

	maxDelta := int64(math.Round(math.Max(avgDistMaxDeltaAbsoluteSec, 100*avgDistMaxDeltaRelative)))
	if got > 100+maxDelta {
		t.Fatalf("attendu une valeur plafonnée à %d au maximum, obtenu %d", 100+maxDelta, got)
	}
	// Avec un delta de 20s bien inférieur au plafond, la valeur lissée doit
	// être très proche de la brute (120s), pas de l'ancienne (100s).
	if got < 115 {
		t.Fatalf("attendu une convergence rapide vers la valeur brute (120), obtenu %d", got)
	}
}

func TestSmoothDistributionTime_StaysWithinGlobalBounds(t *testing.T) {
	// old et raw sont tous deux dans [avgDistFloorSec, avgDistCeilSec] :
	// aucune combinaison lissage+plafonnement ne doit en sortir.
	previous := sql.NullInt64{Valid: true, Int64: avgDistCeilSec}
	got := smoothDistributionTime(previous, avgDistFloorSec, 3)
	if got < avgDistFloorSec || got > avgDistCeilSec {
		t.Fatalf("valeur lissée hors bornes globales [%d, %d] : %d", avgDistFloorSec, avgDistCeilSec, got)
	}
}

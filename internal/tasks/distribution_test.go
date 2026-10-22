package tasks

import (
	"database/sql"
	"math"
	"testing"
)

// Comme en PHP, une moyenne exige au moins avgDistMinItems LIGNES
// d'orderitems (count($order_items) < 5) ET avgDistMinItems items
// exploitables (total_items_processed < 5). Les agrégats sont ceux que
// renvoie computeDistributionStats.

func TestAverageDistributionTime_NotEnoughLines(t *testing.T) {
	// 2 lignes seulement, même si les quantités cumulées dépassent 5.
	avg, processed := averageDistributionTime(merchantDistributionStats{lines: 2, items: 7, totalSeconds: 300})
	if avg != 0 || processed != 0 {
		t.Fatalf("attendu (0, 0) avec moins de %d lignes, obtenu (%d, %d)", avgDistMinItems, avg, processed)
	}
}

func TestAverageDistributionTime_NotEnoughUsableItems(t *testing.T) {
	// 5 lignes mais seulement 2 items exploitables → pas de moyenne.
	avg, processed := averageDistributionTime(merchantDistributionStats{lines: 5, items: 2, totalSeconds: 200})
	if avg != 0 || processed != 0 {
		t.Fatalf("attendu (0, 0) avec moins de %d items exploitables, obtenu (%d, %d)", avgDistMinItems, avg, processed)
	}
}

func TestAverageDistributionTime_WeightedMean(t *testing.T) {
	// 5 lignes qty 1 (60+80+100+120+140 = 500s) + 1 ligne qty 5 à 50s/item
	// (250s) → total 750s sur 10 items → moyenne pondérée 75s.
	avg, processed := averageDistributionTime(merchantDistributionStats{lines: 6, items: 10, totalSeconds: 750})
	if processed != 10 || avg != 75 {
		t.Fatalf("attendu (75, 10), obtenu (%d, %d)", avg, processed)
	}
}

func TestAverageDistributionTime_ClampFloor(t *testing.T) {
	// 10 items à 4s → bornée à avgDistFloorSec (le filtre SQL exclut
	// normalement ces turnarounds, ceinture-bretelles).
	avg, processed := averageDistributionTime(merchantDistributionStats{lines: 5, items: 10, totalSeconds: 40})
	if processed != 10 || avg != avgDistFloorSec {
		t.Fatalf("attendu (%d, 10), obtenu (%d, %d)", avgDistFloorSec, avg, processed)
	}
}

func TestAverageDistributionTime_ClampCeil(t *testing.T) {
	// 5 items à 1500s → bornée à avgDistCeilSec.
	avg, processed := averageDistributionTime(merchantDistributionStats{lines: 5, items: 5, totalSeconds: 7500})
	if processed != 5 || avg != avgDistCeilSec {
		t.Fatalf("attendu (%d, 5), obtenu (%d, %d)", avgDistCeilSec, avg, processed)
	}
}

func TestAverageDistributionTime_NoData(t *testing.T) {
	// Marchand absent de la map (aucun orderitem servi dans la fenêtre).
	avg, processed := averageDistributionTime(merchantDistributionStats{})
	if avg != 0 || processed != 0 {
		t.Fatalf("attendu (0, 0) sans données, obtenu (%d, %d)", avg, processed)
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

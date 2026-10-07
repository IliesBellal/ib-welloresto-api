package accounting

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeDeclarationChannels(t *testing.T) {
	filter, keys, err := normalizeDeclarationChannels(nil)
	if err != nil || filter != nil || len(keys) != 5 {
		t.Fatalf("vide = (%v, %v, %v), want aucun filtre et 5 canaux", filter, keys, err)
	}
	// Anciennes valeurs : restaurant = caisse + borne.
	filter, keys, err = normalizeDeclarationChannels([]string{"restaurant", "deliveroo"})
	want := []string{"WELLO_RESTO_POS", "KIOSK", "DELIVEROO"}
	if err != nil || !reflect.DeepEqual(filter, want) || !reflect.DeepEqual(keys, want) {
		t.Fatalf("restaurant+deliveroo = (%v, %v, %v), want %v", filter, keys, err, want)
	}
	// Tous les canaux cochés = aucun filtre (commandes sans canal comprises).
	filter, _, err = normalizeDeclarationChannels([]string{"WELLO_RESTO_POS", "kiosk", "SCANNORDER", "ubereats", "DELIVEROO"})
	if err != nil || filter != nil {
		t.Fatalf("tous les canaux = (%v, %v), want aucun filtre", filter, err)
	}
	if _, _, err := normalizeDeclarationChannels([]string{"telepathie"}); err == nil {
		t.Fatal("canal inconnu : erreur attendue")
	}
}

func TestDeclarationSegments(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, paris) }

	// Du 20/06 au 10/08 inclus : coupé aux 1er juillet et 1er août.
	got := declarationSegments(day(2025, 6, 20), day(2025, 8, 11), nil)
	want := []declarationSegment{
		{from: day(2025, 6, 20), to: day(2025, 7, 1)},
		{from: day(2025, 7, 1), to: day(2025, 8, 1)},
		{from: day(2025, 8, 1), to: day(2025, 8, 11)},
	}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].from.Equal(want[i].from) || !got[i].to.Equal(want[i].to) {
			t.Fatalf("segment %d = %v, want %v", i, got[i], want[i])
		}
	}

	// Un changement de mode en milieu de mois (normalement impossible) coupe
	// aussi ; un changement hors période est ignoré.
	got = declarationSegments(day(2025, 7, 1), day(2025, 8, 1), []time.Time{day(2025, 7, 15), day(2024, 1, 1)})
	if len(got) != 2 || !got[0].to.Equal(day(2025, 7, 15)) || !got[1].from.Equal(day(2025, 7, 15)) {
		t.Fatalf("segments avec changement au 15 = %v", got)
	}
}

func TestAggregateDeclarationShares(t *testing.T) {
	shares := []netVATShare{
		{Source: "WELLO_RESTO_POS", OrderType: "in", Rate: 10, TTC: 1100},
		{Source: "WELLO_RESTO_POS", OrderType: "in", Rate: 10, TTC: 1100},
		{Source: "KIOSK", OrderType: "take_away", Rate: 5.5, TTC: 1055},
	}
	rows := aggregateDeclarationShares("2025-07", "AUTO", shares)
	want := []vatDeclarationRow{
		{Month: "2025-07", ClosingMode: "AUTO", Channel: "KIOSK", OrderType: "take_away", Rate: 5.5, TTCCents: 1055, HTCents: 1000, VATCents: 55},
		{Month: "2025-07", ClosingMode: "AUTO", Channel: "WELLO_RESTO_POS", OrderType: "in", Rate: 10, TTCCents: 2200, HTCents: 2000, VATCents: 200},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("lignes = %+v, want %+v", rows, want)
	}
}

func TestFormatCSVAmountInEuros(t *testing.T) {
	if got := formatCSVAmount(1234); got != "12.34" {
		t.Fatalf("formatCSVAmount(1234) = %q, want 12.34 (euros, pas centimes)", got)
	}
}

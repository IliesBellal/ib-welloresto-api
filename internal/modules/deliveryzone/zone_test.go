package deliveryzone

import (
	"math"
	"reflect"
	"testing"
)

// Mêmes cas que wello-back-office/src/lib/deliveryZones.test.ts : les deux
// implémentations doivent donner les mêmes zones.

var metz = Point{Lat: 49.1193, Lng: 6.1757}

func pointAtBearing(origin Point, bearingDeg, distanceKm float64) Point {
	rad := bearingDeg * math.Pi / 180
	east, north := distanceKm*math.Sin(rad), distanceKm*math.Cos(rad)
	return Point{
		Lat: origin.Lat + north/kmPerDegLat,
		Lng: origin.Lng + east/(kmPerDegLat*math.Cos(origin.Lat*math.Pi/180)),
	}
}

func settings(zoningType string) Settings {
	m := metz
	return Settings{
		ZoningType:         zoningType,
		CardinalConeCount:  4,
		CardinalZoneRanges: "0-1,1-3,3-999",
		RadialConeCount:    8,
		RadialZoneRanges:   "0-3,3-5,5-999",
		GridCellSizeKm:     2,
		Merchant:           &m,
	}
}

func TestParseBands(t *testing.T) {
	got := ParseBands("3-5, 0-3,abc,5-5,7-6,5-999")
	want := []Band{{0, 3}, {3, 5}, {5, 999}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(ParseBands("")) != 0 {
		t.Fatal("chaîne vide = aucune plage")
	}
	// Comme Number("") en JS : "-3" vaut 0-3, "3-" est vide.
	if got := ParseBands("-3,3-"); !reflect.DeepEqual(got, []Band{{0, 3}}) {
		t.Fatalf("bornes vides : got %v", got)
	}
}

func TestLabel(t *testing.T) {
	radial := settings(ZoningRadial)
	cardinal := settings(ZoningCardinal)
	cardinal8 := settings(ZoningCardinal)
	cardinal8.CardinalConeCount = 8
	grid := settings(ZoningGrid)
	shortRadial := settings(ZoningRadial)
	shortRadial.RadialZoneRanges = "0-3,3-5"

	cases := []struct {
		name     string
		s        Settings
		bearing  float64
		distance float64
		want     string
	}{
		{"radial A1", radial, 10, 1, "A1"},
		{"radial B2", radial, 60, 4, "B2"},
		{"radial H3", radial, 350, 20, "H3"},
		{"radial au-delà", shortRadial, 10, 6, ""},
		{"cardinal N1", cardinal, 350, 0.5, "N1"},
		{"cardinal N2", cardinal, 40, 2, "N2"},
		{"cardinal E2", cardinal, 90, 2, "E2"},
		{"cardinal O3", cardinal, 270, 10, "O3"},
		{"cardinal8 NE2", cardinal8, 45, 2, "NE2"},
		{"cardinal8 SO1", cardinal8, 225, 0.5, "SO1"},
		{"grid centre", grid, 0, 0, "F6"},
		{"grid est", grid, 90, 2, "G6"},
		{"grid nord", grid, 0, 2, "F5"},
		{"grid sud", grid, 180, 10, "F11"},
		{"grid hors grille", grid, 180, 12, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.Label(pointAtBearing(metz, c.bearing, c.distance)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestGridHalfCells(t *testing.T) {
	for size, want := range map[float64]int{1: 10, 2: 5, 50: 1, 0: 0} {
		if got := GridHalfCells(size); got != want {
			t.Fatalf("size %v: got %d, want %d", size, got, want)
		}
	}
}

func TestGridOriginOverridesMerchant(t *testing.T) {
	s := settings(ZoningGrid)
	origin := Point{Lat: 49.2, Lng: 6.2}
	s.GridOrigin = &origin
	if got := s.Label(origin); got != "F6" {
		t.Fatalf("got %q, want F6", got)
	}
}

func TestCardinalFallsBackToLetters(t *testing.T) {
	s := settings(ZoningCardinal)
	s.CardinalConeCount = 6
	if got := s.Label(pointAtBearing(metz, 5, 0.5)); got != "A1" {
		t.Fatalf("got %q, want A1", got)
	}
}

func TestNothingToCompute(t *testing.T) {
	p := pointAtBearing(metz, 10, 1)
	none := settings("")
	noMerchant := settings(ZoningRadial)
	noMerchant.Merchant = nil
	noBands := settings(ZoningRadial)
	noBands.RadialZoneRanges = ""
	for name, s := range map[string]Settings{"sans zonage": none, "sans établissement": noMerchant, "sans plage": noBands} {
		if got := s.Label(p); got != "" {
			t.Fatalf("%s : got %q", name, got)
		}
	}
}

func TestCodeRespectsColumnLength(t *testing.T) {
	s := settings(ZoningCardinal)
	s.CardinalConeCount = 16
	s.CardinalZoneRanges = "0-1,1-2,2-3,3-4,4-5,5-6,6-7,7-8,8-9,9-10,10-20"
	p := pointAtBearing(metz, 22.5, 15) // NNE, plage 11
	if got := s.Label(p); got != "NNE11" {
		t.Fatalf("label: got %q", got)
	}
	if got := s.Code(p); got != "" {
		t.Fatalf("code trop long enregistré : %q", got)
	}
	if got := s.Code(pointAtBearing(metz, 22.5, 0.5)); got != "NNE1" {
		t.Fatalf("code: got %q", got)
	}
}

func TestValidPoint(t *testing.T) {
	zero, lat := 0.0, 49.1
	if _, ok := ValidPoint(&zero, &zero); ok {
		t.Fatal("0,0 doit être rejeté")
	}
	if _, ok := ValidPoint(nil, &lat); ok {
		t.Fatal("coordonnée absente doit être rejetée")
	}
	if _, ok := ValidPoint(&lat, &zero); !ok {
		t.Fatal("longitude 0 seule est valide (méridien de Greenwich)")
	}
}

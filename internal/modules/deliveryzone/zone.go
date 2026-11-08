// Package deliveryzone attribue une zone de livraison (A1, NE2, F6…) à une
// position, d'après la découpe choisie par l'établissement (merchant_parameters :
// zoning_type, cardinal_*, radial_*, grid_*).
//
// La convention est celle de l'aperçu du back-office
// (wello-back-office/src/lib/deliveryZones.ts, zoneLabelForPoint) : les deux
// implémentations doivent rester identiques, leurs tests portent les mêmes cas.
// Voir docs/zones-livraison.md.
//
//   - RADIAL : secteurs égaux A, B, C… dans le sens horaire, A commençant plein
//     nord ; plages numérotées 1, 2, 3… du centre vers l'extérieur (B2).
//   - CARDINAL : secteurs centrés sur leur direction (N couvre -45°…+45° avec
//     4 secteurs), nommés N/E/S/O, N/NE/…/NO ou N/NNE/…/NNO ; autre nombre de
//     secteurs : lettres. Zone = direction + plage (NE2).
//   - GRID : grille centrée sur l'origine (par défaut l'établissement), couvrant
//     au moins GridRadiusKm ; colonnes A, B… d'ouest en est, lignes 1, 2… du
//     nord au sud (F6 au centre avec des cases de 2 km).
package deliveryzone

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	ZoningCardinal = "CARDINAL"
	ZoningRadial   = "RADIAL"
	ZoningGrid     = "GRID"

	kmPerDegLat = 111.32

	// OpenEndedKm : une borne max supérieure ou égale vaut « sans limite »
	// (affichage seulement ; l'attribution utilise la borne telle quelle).
	OpenEndedKm = 100

	// GridRadiusKm : rayon minimal couvert par la grille autour de l'origine.
	GridRadiusKm = 10

	gridMaxHalfCells = 12

	// MaxCodeLength : taille de customer.customer_zone_code (varchar(4)).
	MaxCodeLength = 4
)

var cardinalNames = map[int][]string{
	2:  {"N", "S"},
	4:  {"N", "E", "S", "O"},
	8:  {"N", "NE", "E", "SE", "S", "SO", "O", "NO"},
	16: {"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSO", "SO", "OSO", "O", "ONO", "NO", "NNO"},
}

// Point est une position en degrés décimaux.
type Point struct {
	Lat float64
	Lng float64
}

// Settings est la découpe d'un établissement et sa position.
type Settings struct {
	ZoningType         string
	CardinalConeCount  int
	CardinalZoneRanges string
	RadialConeCount    int
	RadialZoneRanges   string
	GridCellSizeKm     float64
	GridOrigin         *Point
	Merchant           *Point
}

// Band est une plage de distance [Min, Max[ en km.
type Band struct {
	Min float64
	Max float64
}

// ValidPoint écarte une position absente ou à 0,0 : plusieurs flux
// (Deliveroo sans adresse, anciens imports) enregistrent 0,0 au lieu de NULL.
func ValidPoint(lat, lng *float64) (Point, bool) {
	if lat == nil || lng == nil {
		return Point{}, false
	}
	if *lat == 0 && *lng == 0 {
		return Point{}, false
	}
	if math.IsNaN(*lat) || math.IsNaN(*lng) || math.Abs(*lat) > 90 || math.Abs(*lng) > 180 {
		return Point{}, false
	}
	return Point{Lat: *lat, Lng: *lng}, true
}

func letter(index int) string { return string(rune('A' + index)) }

func sectorNames(zoningType string, count int) []string {
	if zoningType == ZoningCardinal {
		if names, ok := cardinalNames[count]; ok {
			return names
		}
	}
	names := make([]string, count)
	for i := range names {
		names[i] = letter(i)
	}
	return names
}

// parseBound reproduit Number(v.trim()) de JavaScript : chaîne vide = 0.
func parseBound(raw string) (float64, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, true
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// ParseBands lit "0-3,3-5,5-999" : plages illisibles ou vides (max <= min)
// ignorées, tri par borne min. La numérotation 1, 2, 3… suit cet ordre.
func ParseBands(csv string) []Band {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var bands []Band
	for _, part := range strings.Split(csv, ",") {
		bounds := strings.Split(part, "-")
		if len(bounds) < 2 {
			continue
		}
		minKm, okMin := parseBound(bounds[0])
		maxKm, okMax := parseBound(bounds[1])
		if !okMin || !okMax || minKm < 0 || maxKm <= minKm {
			continue
		}
		bands = append(bands, Band{Min: minKm, Max: maxKm})
	}
	sort.SliceStable(bands, func(i, j int) bool { return bands[i].Min < bands[j].Min })
	return bands
}

func clampCount(count int) int {
	if count < 0 {
		return 0
	}
	if count > 26 {
		return 26
	}
	return count
}

// GridHalfCells : nombre de cases de part et d'autre de la case centrale.
func GridHalfCells(cellSizeKm float64) int {
	if !(cellSizeKm > 0) {
		return 0
	}
	half := int(math.Ceil(GridRadiusKm/cellSizeKm - 0.5))
	if half < 1 {
		half = 1
	}
	if half > gridMaxHalfCells {
		half = gridMaxHalfCells
	}
	return half
}

func toLocalKm(origin, p Point) (east, north float64) {
	east = (p.Lng - origin.Lng) * kmPerDegLat * math.Cos(origin.Lat*math.Pi/180)
	north = (p.Lat - origin.Lat) * kmPerDegLat
	return east, north
}

// Label renvoie la zone de p, ou "" hors découpe (au-delà de la dernière
// plage, hors grille, entre deux plages) ou sans découpe configurée.
func (s Settings) Label(p Point) string {
	switch s.ZoningType {
	case ZoningGrid:
		origin := s.GridOrigin
		if origin == nil {
			origin = s.Merchant
		}
		size := s.GridCellSizeKm
		half := GridHalfCells(size)
		if origin == nil || half == 0 {
			return ""
		}
		east, north := toLocalKm(*origin, p)
		col := int(math.Floor(east/size+0.5)) + half
		row := half - int(math.Floor(north/size+0.5))
		if col < 0 || row < 0 || col > half*2 || row > half*2 {
			return ""
		}
		return letter(col) + strconv.Itoa(row+1)

	case ZoningCardinal, ZoningRadial:
		if s.Merchant == nil {
			return ""
		}
		count, ranges := s.RadialConeCount, s.RadialZoneRanges
		if s.ZoningType == ZoningCardinal {
			count, ranges = s.CardinalConeCount, s.CardinalZoneRanges
		}
		count = clampCount(count)
		bands := ParseBands(ranges)
		if count < 2 || len(bands) == 0 {
			return ""
		}
		east, north := toLocalKm(*s.Merchant, p)
		distance := math.Hypot(east, north)
		ring := -1
		for i, b := range bands {
			if distance >= b.Min && distance < b.Max {
				ring = i
				break
			}
		}
		if ring == -1 {
			return ""
		}
		width := 360 / float64(count)
		bearing := math.Mod(math.Atan2(east, north)*180/math.Pi+360, 360)
		shifted := bearing
		if s.ZoningType == ZoningCardinal {
			shifted = math.Mod(bearing+width/2, 360)
		}
		sector := int(math.Floor(shifted / width))
		if sector > count-1 {
			sector = count - 1
		}
		return sectorNames(s.ZoningType, count)[sector] + strconv.Itoa(ring+1)
	}
	return ""
}

// Code est Label borné à la colonne customer_zone_code : un libellé trop long
// (16 secteurs et 10 plages ou plus) n'est pas enregistré plutôt que tronqué.
func (s Settings) Code(p Point) string {
	label := s.Label(p)
	if len(label) > MaxCodeLength {
		return ""
	}
	return label
}

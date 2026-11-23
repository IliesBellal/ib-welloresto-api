package helpers

import "strings"

// ScanNOrderCuisine est un type de cuisine proposé au restaurateur pour le
// référencement de sa page ScanNOrder (docs/scannorder-seo.md).
//
//   - Code : valeur stockée (scannorder_settings.seo_cuisine_type, codes
//     séparés par des virgules) et échangée avec le back-office ;
//   - Label : la cuisine, telle que publiée dans servesCuisine (schema.org) et
//     affichée sur la page (« Pizza », « Libanaise ») ;
//   - Place : l'établissement, pour les titres et descriptions générés
//     (« Pizzeria », « Restaurant libanais ») — ce que tape l'internaute.
type ScanNOrderCuisine struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Place string `json:"place"`
}

// ScanNOrderMaxCuisines : nombre de types de cuisine retenus par
// établissement. Au-delà, le titre s'allonge sans rien apporter et le type
// principal se dilue.
const ScanNOrderMaxCuisines = 3

// scannorderCuisines : liste fermée, dans l'ordre d'affichage du back-office.
// Ne jamais renommer un code existant (il est stocké) ; un libellé peut
// changer librement.
var scannorderCuisines = []ScanNOrderCuisine{
	{"pizza", "Pizza", "Pizzeria"},
	{"burger", "Burger", "Burger"},
	{"kebab", "Kebab", "Kebab"},
	{"tacos", "Tacos", "Tacos"},
	{"poulet", "Poulet frit", "Fried chicken"},
	{"sandwich", "Sandwichs", "Sandwicherie"},
	{"bagel", "Bagels", "Bagels"},
	{"sushi", "Sushi", "Restaurant de sushi"},
	{"japonais", "Japonaise", "Restaurant japonais"},
	{"ramen", "Ramen", "Bar à ramen"},
	{"chinois", "Chinoise", "Restaurant chinois"},
	{"thai", "Thaïlandaise", "Restaurant thaïlandais"},
	{"vietnamien", "Vietnamienne", "Restaurant vietnamien"},
	{"coreen", "Coréenne", "Restaurant coréen"},
	{"asiatique", "Asiatique", "Restaurant asiatique"},
	{"indien", "Indienne", "Restaurant indien"},
	{"poke", "Poké", "Poké bowl"},
	{"libanais", "Libanaise", "Restaurant libanais"},
	{"turc", "Turque", "Restaurant turc"},
	{"grec", "Grecque", "Restaurant grec"},
	{"marocain", "Marocaine", "Restaurant marocain"},
	{"oriental", "Orientale", "Restaurant oriental"},
	{"africain", "Africaine", "Restaurant africain"},
	{"antillais", "Antillaise", "Restaurant antillais"},
	{"italien", "Italienne", "Restaurant italien"},
	{"francais", "Française", "Restaurant français"},
	{"traditionnel", "Traditionnelle", "Restaurant traditionnel"},
	{"brasserie", "Brasserie", "Brasserie"},
	{"bistrot", "Bistrot", "Bistrot"},
	{"gastronomique", "Gastronomique", "Restaurant gastronomique"},
	{"creperie", "Crêpes et galettes", "Crêperie"},
	{"espagnol", "Espagnole", "Restaurant espagnol"},
	{"portugais", "Portugaise", "Restaurant portugais"},
	{"mexicain", "Mexicaine", "Restaurant mexicain"},
	{"americain", "Américaine", "Restaurant américain"},
	{"bresilien", "Brésilienne", "Restaurant brésilien"},
	{"grillades", "Grillades", "Grill"},
	{"steakhouse", "Viandes", "Steakhouse"},
	{"fruits_de_mer", "Poissons et fruits de mer", "Restaurant de fruits de mer"},
	{"salades", "Salades", "Bar à salades"},
	{"healthy", "Healthy", "Restaurant healthy"},
	{"vegetarien", "Végétarienne", "Restaurant végétarien"},
	{"vegan", "Vegan", "Restaurant vegan"},
	{"halal", "Halal", "Restaurant halal"},
	{"brunch", "Brunch", "Brunch"},
	{"boulangerie", "Boulangerie", "Boulangerie"},
	{"patisserie", "Pâtisserie", "Pâtisserie"},
	{"desserts", "Desserts", "Desserts"},
	{"glacier", "Glaces", "Glacier"},
	{"cafe", "Café", "Coffee shop"},
	{"bubble_tea", "Bubble tea", "Bubble tea"},
	{"bar", "Bar", "Bar"},
}

var scannorderCuisineByCode = func() map[string]ScanNOrderCuisine {
	m := make(map[string]ScanNOrderCuisine, len(scannorderCuisines))
	for _, c := range scannorderCuisines {
		m[c.Code] = c
	}
	return m
}()

// ScanNOrderCuisines renvoie une copie de la liste fermée, pour le
// back-office et l'invite de la suggestion IA.
func ScanNOrderCuisines() []ScanNOrderCuisine {
	return append([]ScanNOrderCuisine(nil), scannorderCuisines...)
}

// ScanNOrderCuisineByCode cherche un type de cuisine par son code.
func ScanNOrderCuisineByCode(code string) (ScanNOrderCuisine, bool) {
	c, ok := scannorderCuisineByCode[code]
	return c, ok
}

// ParseScanNOrderCuisines lit la valeur stockée (codes séparés par des
// virgules), dans l'ordre, sans doublon ni élément vide. Un élément qui n'est
// pas un code connu — texte libre saisi avant la liste fermée — est gardé
// tel quel comme libellé : on ne perd pas une donnée déjà publiée.
func ParseScanNOrderCuisines(stored string) []ScanNOrderCuisine {
	var out []ScanNOrderCuisine
	seen := map[string]bool{}
	for _, raw := range strings.Split(stored, ",") {
		token := strings.TrimSpace(raw)
		if token == "" || seen[strings.ToLower(token)] {
			continue
		}
		seen[strings.ToLower(token)] = true
		if c, ok := scannorderCuisineByCode[token]; ok {
			out = append(out, c)
			continue
		}
		out = append(out, ScanNOrderCuisine{Label: token, Place: token})
	}
	return out
}

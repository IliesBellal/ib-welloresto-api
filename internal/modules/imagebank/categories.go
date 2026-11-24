package imagebank

// Category : catégorie de la banque d'images. Liste fermée : le script
// d'import refuse un code inconnu, et le back-office affiche les libellés
// renvoyés par GET /image-bank/categories.
type Category struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Categories, dans l'ordre d'affichage du popup.
var Categories = []Category{
	{"boissons-soft", "Sodas & canettes"},
	{"eaux", "Eaux"},
	{"jus", "Jus & smoothies"},
	{"boissons-chaudes", "Boissons chaudes"},
	{"bieres", "Bières & cidres"},
	{"vins", "Vins & champagnes"},
	{"aperitifs-cocktails", "Apéritifs & cocktails"},
	{"burgers", "Burgers"},
	{"sandwichs", "Sandwichs & paninis"},
	{"pizzas", "Pizzas"},
	{"tacos-wraps", "Tacos, wraps & kebabs"},
	{"salades", "Salades & bowls"},
	{"pates", "Pâtes & risottos"},
	{"plats", "Plats cuisinés"},
	{"viandes-grillades", "Viandes & grillades"},
	{"poissons", "Poissons & fruits de mer"},
	{"asiatique", "Cuisine asiatique & sushis"},
	{"accompagnements", "Accompagnements"},
	{"sauces", "Sauces"},
	{"ingredients", "Ingrédients & suppléments"},
	{"desserts", "Desserts & pâtisseries"},
	{"glaces", "Glaces"},
	{"boulangerie", "Boulangerie & viennoiseries"},
	{"petit-dejeuner", "Petit-déjeuner & brunch"},
	{"enfants", "Menus enfants"},
	{"ambiance", "Ambiance & salle"},
}

// IsCategory indique si code fait partie de la liste fermée.
func IsCategory(code string) bool {
	for _, c := range Categories {
		if c.Code == code {
			return true
		}
	}
	return false
}

// Formats d'image : square pour les vignettes (produit, option, catégorie),
// banner pour les bannières (ScanNOrder, hub franchise, veille borne, rubans).
const (
	FormatSquare = "square"
	FormatBanner = "banner"
)

// IsFormat indique si f est un format connu.
func IsFormat(f string) bool { return f == FormatSquare || f == FormatBanner }

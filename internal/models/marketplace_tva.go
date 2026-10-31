package models

import "strings"

// Catégories de TVA (tva_categories.tva_id) données aux produits créés
// automatiquement quand une commande marketplace contient un article inconnu.
// Les plateformes n'envoient aucun taux (docs/TVA_MARKETPLACE_AUDIT.md) : sans
// repère, tout est du repas préparé à 10 %, et les boissons alcoolisées à 20 %.
// Le restaurateur affine ensuite produit par produit.
//
// Les identifiants dépendent du mode de consommation : « 10 % » n'est pas la
// même catégorie sur place (5), à emporter (2) et en livraison (7). Avant le
// correctif de docs/marketplace-promotions-tva.md, Uber Eats écrivait (5, 9, 3),
// c'est-à-dire les catégories « boissons alcoolisées » de la livraison et de
// l'emporté, et Deliveroo ne renseignait rien (0 = non défini, 0 %).
type MarketplaceTVA struct {
	In       int
	TakeAway int
	Delivery int
}

var (
	// MarketplaceTVAStandard : produits alimentaires consommés immédiatement, 10 %.
	MarketplaceTVAStandard = MarketplaceTVA{In: 5, TakeAway: 2, Delivery: 7}
	// MarketplaceTVAAlcohol : boissons alcoolisées, 20 %.
	MarketplaceTVAAlcohol = MarketplaceTVA{In: 6, TakeAway: 3, Delivery: 9}
)

// MarketplaceTVAFor renvoie les catégories de TVA d'un produit créé depuis une
// plateforme.
func MarketplaceTVAFor(alcohol bool) MarketplaceTVA {
	if alcohol {
		return MarketplaceTVAAlcohol
	}
	return MarketplaceTVAStandard
}

// alcoholWords : mots (sans accent, en minuscules) qui désignent une boisson
// alcoolisée dans un nom de produit. Aide au choix du taux par défaut quand la
// plateforme ne dit rien ; le restaurateur reste maître du taux final.
var alcoholWords = map[string]bool{
	"biere": true, "bieres": true, "vin": true, "vins": true, "champagne": true,
	"prosecco": true, "cidre": true, "whisky": true, "whiskey": true, "vodka": true,
	"rhum": true, "gin": true, "tequila": true, "pastis": true, "ricard": true,
	"cognac": true, "calvados": true, "spritz": true, "aperol": true, "mojito": true,
	"sangria": true, "kir": true, "porto": true, "armagnac": true, "digestif": true,
	"heineken": true, "kronenbourg": true, "1664": true, "corona": true,
	"desperados": true, "leffe": true, "grimbergen": true, "chouffe": true,
	"carlsberg": true, "budweiser": true, "stella": true, "pelforth": true,
	"amstel": true, "guinness": true, "peroni": true, "moretti": true,
	"hoegaarden": true, "affligem": true, "paulaner": true, "jupiler": true,
	"ipa": true, "pression": true, "malibu": true, "get27": true, "jagermeister": true,
}

// alcoholFree : mentions qui disent le contraire (« bière sans alcool », « 0.0 »).
var alcoholFree = []string{"sans alcool", "0.0", "0,0", "0%", "0 %", "alcool free", "alcohol free"}

var accentFold = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "œ", "oe", "’", "'",
)

// LooksAlcoholic dit si le nom d'un produit évoque une boisson alcoolisée.
// Approximatif par nature : sert uniquement au taux par défaut d'un produit créé
// automatiquement.
func LooksAlcoholic(name string) bool {
	n := accentFold.Replace(strings.ToLower(name))
	for _, free := range alcoholFree {
		if strings.Contains(n, free) {
			return false
		}
	}
	for _, w := range strings.FieldsFunc(n, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if alcoholWords[w] {
			return true
		}
	}
	return false
}

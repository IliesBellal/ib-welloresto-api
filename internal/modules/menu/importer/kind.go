package importer

// ProductKind est la nature d'un produit telle que la lecture de carte par
// photo la propose (porte IA). Elle ne sert qu'à proposer des taux de TVA :
// une carte n'affiche jamais la TVA, et le restaurateur confirme en relecture
// (ImportDecisions.TvaConfirmed). Vide pour toutes les autres portes.
type ProductKind string

const (
	KindFood            ProductKind = "food"              // plats, sandwichs, desserts servis
	KindHotDrink        ProductKind = "hot_drink"         // café, thé
	KindSoftDrinkServed ProductKind = "soft_drink_served" // soda au verre, jus pressé, eau en carafe
	KindSoftDrinkSealed ProductKind = "soft_drink_sealed" // canette, bouteille capsulée
	KindPackagedFood    ProductKind = "packaged_food"     // consommation différée : emballé, bocal, sous vide
	KindAlcohol         ProductKind = "alcohol"           // bière, vin, cocktails, spiritueux
	KindOther           ProductKind = "other"             // indéterminé : taux à saisir
)

// AllProductKinds liste les natures dans l'ordre d'affichage (et de l'enum du
// schéma JSON de la porte IA).
var AllProductKinds = []ProductKind{
	KindFood, KindHotDrink, KindSoftDrinkServed, KindSoftDrinkSealed,
	KindPackagedFood, KindAlcohol, KindOther,
}

// IsKnown écarte une nature inconnue (décision du wizard non crue sur parole).
func (k ProductKind) IsKnown() bool {
	for _, known := range AllProductKinds {
		if k == known {
			return true
		}
	}
	return false
}

// KindTvaRates rend les taux de TVA proposés (sur place, à emporter,
// livraison) pour une nature. Table validée par Ilies le 2026-09-29 (voir
// docs/cadrage-import-carte-photo-ia.md § 5.7) : 10 % en consommation
// immédiate, 5,5 % pour les boissons fermées et aliments conditionnés à
// emporter ou livrés, 20 % pour l'alcool quel que soit le mode.
//
// KindOther, vide ou inconnue : nil partout, le taux reste à saisir (la preview
// le signale, le commit bloque tant qu'il manque).
func KindTvaRates(kind ProductKind) (in, takeAway, delivery *float64) {
	rate := func(v float64) *float64 { return &v }
	switch kind {
	case KindFood, KindHotDrink, KindSoftDrinkServed:
		return rate(10), rate(10), rate(10)
	case KindSoftDrinkSealed, KindPackagedFood:
		return rate(10), rate(5.5), rate(5.5)
	case KindAlcohol:
		return rate(20), rate(20), rate(20)
	default:
		return nil, nil, nil
	}
}

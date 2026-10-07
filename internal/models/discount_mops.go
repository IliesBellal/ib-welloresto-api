package models

import "strings"

// Remises accordées en caisse, enregistrées par l'app de caisse comme des
// « paiements » (méthodes « Réduction montant » / « Réduction pourcentage »,
// DISCOUNT historique). Ce n'est pas de l'argent encaissé et la TVA est due sur
// le prix réellement payé (CGI art. 267) : comme Square ou Lightspeed, les
// rapports les présentent en « Ventes brutes − Remises = Ventes nettes », la
// TVA est calculée sur le net, et elles n'apparaissent jamais parmi les moyens
// de paiement ni dans le comptage de caisse. Cf.
// docs/EXPORT_COMPTABLE_MODES_CLOTURE.md.

// DiscountMOPs liste les codes payments.mop qui sont des remises.
var DiscountMOPs = []string{"CURRENCY", "PERCENTAGE", "DISCOUNT"}

// DiscountMOPsSQL est DiscountMOPs sous forme de liste SQL, à placer après
// `p.mop IN ` / `p.mop NOT IN `.
const DiscountMOPsSQL = `('CURRENCY', 'PERCENTAGE', 'DISCOUNT')`

// discountPaymentLabels : libellés sous lesquels une remise peut apparaître
// dans un relevé de caisse saisi (cash_registers_custom_items, texte libre) —
// codes bruts, libellés FR de la table labels, libellé de l'app de caisse —
// comparés en majuscules.
var discountPaymentLabels = map[string]bool{
	"CURRENCY":              true,
	"PERCENTAGE":            true,
	"DISCOUNT":              true,
	"RÉDUCTION MONTANT":     true,
	"RÉDUCTION POURCENTAGE": true,
	"RÉDUCTION MONTA.":      true,
}

// IsDiscountMOP indique si un code de moyen de paiement est une remise.
func IsDiscountMOP(mop string) bool {
	switch strings.ToUpper(strings.TrimSpace(mop)) {
	case "CURRENCY", "PERCENTAGE", "DISCOUNT":
		return true
	}
	return false
}

// IsDiscountPaymentLabel indique si un libellé de relevé de caisse (code ou
// libellé affiché, casse indifférente) désigne une remise.
func IsDiscountPaymentLabel(label string) bool {
	return discountPaymentLabels[strings.ToUpper(strings.TrimSpace(label))]
}

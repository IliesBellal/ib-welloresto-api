package models

// HandCollectedMOPs liste les moyens de paiement qu'un livreur encaisse en
// main propre : espèces, terminal carte, titre-restaurant, chèque, chèque
// vacances. Seuls ces paiements portent le livreur dans payments.collected_by
// à la livraison d'un arrêt (delivery_sessions.FinalizeDeliveredStop). Tous
// les autres (Stripe, plateformes, récompense, borne) sont réglés sans lui et
// gardent leur encaisseur d'origine.
var HandCollectedMOPs = []string{"ES", "CB", "TR", "CHEQUE", "HOLIDAY_VOUCHER"}

// IsHandCollectedMOP indique si mop fait partie de HandCollectedMOPs.
func IsHandCollectedMOP(mop string) bool {
	for _, m := range HandCollectedMOPs {
		if m == mop {
			return true
		}
	}
	return false
}

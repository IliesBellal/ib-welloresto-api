package order_life_cycle

import (
	"strings"

	"welloresto-api/internal/models"
)

// Paiement attendu (docs/order-lifecycle.md, « Paiement attendu ») : le moyen
// de paiement qu'un client annonce (« je paierai en espèces à la livraison »).
// C'est une information de la commande (orders.expected_mop et
// expected_tender_amount), jamais un paiement : rien n'est signé dans la
// chaîne fiscale, et le statut « payé » reste dérivé de payments.

// mopES : espèces, seul moyen qui porte un montant remis (monnaie à rendre).
const mopES = "ES"

// resolveExpectedPayment traduit le champ expected_payment d'une requête.
//
//   - p == nil (champ absent) : write = false, l'annonce enregistrée est
//     gardée (borne, Scan&Order, plateformes, caisse d'une version antérieure) ;
//   - MOP vide ou inconnu : write = true et mop/tender nil, l'annonce est
//     effacée. Un moyen inconnu ne fait jamais échouer l'enregistrement de la
//     commande : c'est une information, pas une règle de gestion ;
//   - MOP encaissable en main propre (models.HandCollectedMOPs) ou compte
//     client (models.AccountMOP : « sur compte », réglé à la clôture de la
//     tournée sans argent) : mop normalisé en majuscules ; tender renseigné
//     pour les espèces seulement, et seulement s'il est strictement positif.
func resolveExpectedPayment(p *models.ExpectedPaymentPayload) (write bool, mop *string, tender *int) {
	if p == nil {
		return false, nil, nil
	}
	m := strings.ToUpper(strings.TrimSpace(p.MOP))
	if !models.IsHandCollectedMOP(m) && m != models.AccountMOP {
		return true, nil, nil
	}
	if m == mopES && p.TenderAmount != nil && *p.TenderAmount > 0 {
		t := *p.TenderAmount
		tender = &t
	}
	return true, &m, tender
}

package order_life_cycle

import (
	"strings"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Values written to orders.cancelled_by_type (C2).
const (
	CancelledByStaff    = "STAFF"
	CancelledByCustomer = "CUSTOMER"
	CancelledBySystem   = "SYSTEM"
	CancelledByPlatform = "PLATFORM"
)

// classifyCancelledByType maps the actor identifier already threaded through
// every deny/cancel call (DenyOrderRequest.UserID / DenyOrderInput.UserID —
// a real staff user id, or one of a small fixed set of sentinels) onto the
// C2 typology. This is the exhaustive list of sentinels used by every live
// cancellation path in this codebase (see docs/decisions.md for the full
// path-by-path recensement): anything else reaching DenyOrderLocal/
// DeleteOrderLocal is, by construction, a real authenticated user id, hence
// STAFF. An empty userID is left unclassified (nil -> SQL NULL) rather than
// guessed.
func classifyCancelledByType(userID string) *string {
	switch userID {
	case "":
		return nil
	case "SYSTEM", models.StripeWebhookUserID:
		return helpers.StringPtr(CancelledBySystem)
	case models.DeliverooWebhookUserID, models.UberEatsWebhookUserID:
		return helpers.StringPtr(CancelledByPlatform)
	case "SNO_CUSTOMER", "KIOSK":
		return helpers.StringPtr(CancelledByCustomer)
	default:
		return helpers.StringPtr(CancelledByStaff)
	}
}


// paymentCancelSource donne la source d'une annulation de paiement inscrite
// au journal d'audit (fiscal.CancelPayments, lot C conformité caisse), à
// partir des mêmes identifiants que classifyCancelledByType.
func paymentCancelSource(userID string) string {
	switch userID {
	case "SYSTEM":
		return fiscal.CancelSourceSystem
	case models.StripeWebhookUserID:
		return fiscal.CancelSourceStripe
	case models.UberEatsWebhookUserID:
		return fiscal.CancelSourceUberEats
	case models.DeliverooWebhookUserID:
		return fiscal.CancelSourceDeliveroo
	case "SNO_CUSTOMER", "KIOSK":
		return fiscal.CancelSourceCustomer
	default:
		return fiscal.CancelSourceStaff
	}
}

// orderCancelReason est le motif inscrit au journal pour les paiements d'une
// commande annulée ou refusée.
func orderCancelReason(label, reasonID, comment string) string {
	reason := label
	if id := strings.TrimSpace(reasonID); id != "" {
		reason += " (motif " + id + ")"
	}
	if c := strings.TrimSpace(comment); c != "" {
		reason += " : " + c
	}
	return reason
}

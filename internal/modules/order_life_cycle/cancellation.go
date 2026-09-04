package order_life_cycle

import (
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


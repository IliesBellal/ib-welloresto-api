package scannorder

import (
	"fmt"
	"strings"

	"welloresto-api/internal/models"
)

// StatusOnlineOrderingUnavailable : CreateOrderSNO refuse la commande, la carte
// reste consultable (docs/scannorder-carte-sans-stripe.md).
const StatusOnlineOrderingUnavailable = "online_ordering_unavailable"

// StatusStorefrontDisabled : l'établissement a masqué sa carte ScanNOrder.
const StatusStorefrontDisabled = "storefront_disabled"

// onlineOrderingEnabled indique si l'établissement peut encaisser une commande
// ScanNOrder. Toutes les commandes ScanNOrder sont payées en ligne : sans
// compte Stripe Connect, CreateCheckoutSession ne peut pas aboutir. La carte,
// elle, s'affiche dès la création de l'établissement.
//
// Conditions, toutes requises :
//   - carte affichée (storefront_enabled) ;
//   - commande en ligne ouverte (activated : posé par le webhook Stripe à la
//     validation du compte, puis commutateur back-office / POS) ;
//   - compte Stripe Connect enregistré (stripe_accounts.account_id) ;
//   - abonnement non suspendu (même règle que le contrôle historique de
//     CreateOrderSNO).
//
// Même définition, en SQL, dans docs/scannorder-carte-sans-stripe.md.
func onlineOrderingEnabled(row *models.MerchantRow, suspended bool) bool {
	if row == nil || suspended || !row.StorefrontEnabled || !row.Activated {
		return false
	}
	return row.AccountID != nil && strings.TrimSpace(*row.AccountID) != ""
}

// snoOnlineOrderingSQL : onlineOrderingEnabled en SQL, sans l'abonnement — à
// combiner avec NOT snoSubscriptionBlocked ou un contrôle de suspension.
// alias = alias de scannorder_settings.
func snoOnlineOrderingSQL(alias string) string {
	return fmt.Sprintf(`(%[1]s.storefront_enabled = true AND %[1]s.activated = true
		AND EXISTS (SELECT 1 FROM stripe_accounts sa_oo
		            WHERE sa_oo.merchant_id = %[1]s.merchant_id AND TRIM(sa_oo.account_id) <> ''))`, alias)
}

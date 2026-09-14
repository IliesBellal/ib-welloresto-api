package middleware

import "testing"

// TestIsSuspendedReadOnlyExempt — LOT B B2b-2 : la liste d'exemptions ne doit
// bloquer ni l'admin interne, ni la facturation, ni les exports fiscaux, ni
// la clôture de caisse — mais DOIT laisser l'ouverture de caisse bloquée par
// le garde générique (elle a son propre message dédié, voir
// cash_registers.Service.OpenCashRegister). Étendu en LOT B F1 avec les
// quatre groupes de consultation identifiés en revue B2c-2 (analytics,
// orders/pricing-upsell-list-history, cash_register/history, bookings) —
// avec vérification explicite que les routes sœurs mutatives du même groupe
// (orders/create, bookings/create) restent bloquées, et que
// orders/{id}/invoice/email-sms reste bloqué (décision explicite, pas un
// oubli).
//
// LOT B F1 — CORRECTIF : tous les chemins de ce test portaient un "/v1/" en
// tête, hérité de B2b-2, qui ne correspond à AUCUNE route réellement montée
// par cmd/api/routes.go (seul un tout petit groupe — /signup, /public,
// /auth/google, /merchants/{id}/onboarding — vit sous r.Route("/v1", ...) ;
// /admin, /billing, /pos, /accounting, /analytics, /orders, /bookings,
// /cash_register sont tous montés à la racine). Confirmé en tapant
// directement le serveur staging déployé : POST /admin/overrides → 401
// (route existe), POST /v1/admin/overrides → 404 (n'existe pas). Ce test
// passait déjà avant ce correctif — il ne validait que sa propre fonction
// pure, jamais contre le routeur réel — mais validait donc une hypothèse de
// chemin fausse depuis l'origine. Chemins corrigés ici pour refléter les
// vraies routes.
func TestIsSuspendedReadOnlyExempt(t *testing.T) {
	cases := []struct {
		path   string
		exempt bool
	}{
		{"/admin/merchants/303/overrides", true},
		{"/admin/overrides", true},
		{"/billing/sepa/setup", true},
		{"/billing/retry-now", true},
		{"/pos/reports/tva/export", true},
		{"/pos/accounting/export", true},
		{"/accounting/vat/export-csv", true},
		{"/cash_register/reg-1/close", true},
		{"/cash_register/reg-1/enclose", true},
		{"/cash_register/open", false},
		{"/menu/products", false},
		{"/subscriptions/items", false},
		{"/orders/create", false},
		// LOT B F1 — analytics et ses 4 sous-groupes (préfixe).
		{"/analytics/revenue", true},
		{"/analytics/merchants", true},
		{"/analytics/cancellations/by-staff", true},
		{"/analytics/clients/top", true},
		{"/analytics/upsell/by-staff", true},
		// LOT B F1 — orders : consultation exemptée, création toujours bloquée.
		{"/orders/pricing", true},
		{"/orders/upsell", true},
		{"/orders/list", true},
		{"/orders/history", true},
		{"/orders/{order_id}/invoice/email-sms", false},
		// LOT B F1 — cash_register/history.
		{"/cash_register/history", true},
		// LOT B F1 — bookings : recherche (racine) exemptée, création bloquée.
		{"/bookings", true},
		{"/bookings/", true},
		{"/bookings/create", false},
	}
	for _, c := range cases {
		if got := isSuspendedReadOnlyExempt(c.path); got != c.exempt {
			t.Errorf("isSuspendedReadOnlyExempt(%q) = %v, want %v", c.path, got, c.exempt)
		}
	}
}

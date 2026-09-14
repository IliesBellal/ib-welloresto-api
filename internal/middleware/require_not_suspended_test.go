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
func TestIsSuspendedReadOnlyExempt(t *testing.T) {
	cases := []struct {
		path   string
		exempt bool
	}{
		{"/v1/admin/merchants/303/overrides", true},
		{"/v1/admin/overrides", true},
		{"/v1/billing/sepa/setup", true},
		{"/v1/billing/retry-now", true},
		{"/v1/pos/reports/tva/export", true},
		{"/v1/pos/accounting/export", true},
		{"/v1/accounting/vat/export-csv", true},
		{"/v1/cash_register/reg-1/close", true},
		{"/v1/cash_register/reg-1/enclose", true},
		{"/v1/cash_register/open", false},
		{"/v1/menu/products", false},
		{"/v1/subscriptions/items", false},
		{"/v1/orders/create", false},
		// LOT B F1 — analytics et ses 4 sous-groupes (préfixe).
		{"/v1/analytics/revenue", true},
		{"/v1/analytics/merchants", true},
		{"/v1/analytics/cancellations/by-staff", true},
		{"/v1/analytics/clients/top", true},
		{"/v1/analytics/upsell/by-staff", true},
		// LOT B F1 — orders : consultation exemptée, création toujours bloquée.
		{"/v1/orders/pricing", true},
		{"/v1/orders/upsell", true},
		{"/v1/orders/list", true},
		{"/v1/orders/history", true},
		{"/v1/orders/{order_id}/invoice/email-sms", false},
		// LOT B F1 — cash_register/history.
		{"/v1/cash_register/history", true},
		// LOT B F1 — bookings : recherche (racine) exemptée, création bloquée.
		{"/v1/bookings", true},
		{"/v1/bookings/", true},
		{"/v1/bookings/create", false},
	}
	for _, c := range cases {
		if got := isSuspendedReadOnlyExempt(c.path); got != c.exempt {
			t.Errorf("isSuspendedReadOnlyExempt(%q) = %v, want %v", c.path, got, c.exempt)
		}
	}
}

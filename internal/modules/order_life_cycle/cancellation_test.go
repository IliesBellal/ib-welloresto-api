package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestClassifyCancelledByType(t *testing.T) {
	tests := []struct {
		name   string
		userID string
		want   *string
	}{
		{"empty user id is left unclassified", "", nil},
		{"SYSTEM sentinel (cron expiry)", "SYSTEM", strPtr(CancelledBySystem)},
		{"Stripe webhook (checkout session expired)", models.StripeWebhookUserID, strPtr(CancelledBySystem)},
		{"Deliveroo webhook", models.DeliverooWebhookUserID, strPtr(CancelledByPlatform)},
		{"Uber Eats webhook", models.UberEatsWebhookUserID, strPtr(CancelledByPlatform)},
		{"ScanNOrder self-service", "SNO_CUSTOMER", strPtr(CancelledByCustomer)},
		{"Kiosk self-service", "KIOSK", strPtr(CancelledByCustomer)},
		{"real numeric staff id", "226", strPtr(CancelledByStaff)},
		{"real user-<uuid> staff id", "user-3df789dd-44f7-4d7e-a35a-47032525e73", strPtr(CancelledByStaff)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCancelledByType(tt.userID)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("classifyCancelledByType(%q) = %v, want %v", tt.userID, ptrOrNil(got), ptrOrNil(tt.want))
			}
			if got != nil && *got != *tt.want {
				t.Fatalf("classifyCancelledByType(%q) = %q, want %q", tt.userID, *got, *tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func ptrOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

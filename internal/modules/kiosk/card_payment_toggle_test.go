package kiosk

import (
	"testing"
	"time"
)

func TestCardPaymentClosedToday(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}
	at := func(y int, m time.Month, d, h, min int) *time.Time {
		v := time.Date(y, m, d, h, min, 0, 0, paris)
		return &v
	}
	row := func(toggle, payAtCounter bool, closedAt *time.Time) *KioskSettingsRow {
		return &KioskSettingsRow{
			CardPaymentPOSToggle: toggle,
			PayAtCounterEnabled:  payAtCounter,
			CardPaymentEnabled:   true,
			CardPaymentClosedAt:  closedAt,
		}
	}
	now := *at(2026, time.October, 6, 21, 30)

	cases := []struct {
		name string
		row  *KioskSettingsRow
		now  time.Time
		want bool
	}{
		{"jamais fermé", row(true, true, nil), now, false},
		{"fermé ce midi", row(true, true, at(2026, time.October, 6, 12, 0)), now, true},
		{"fermé juste après minuit", row(true, true, at(2026, time.October, 6, 0, 0)), now, true},
		{"fermé hier soir : rouvert à minuit", row(true, true, at(2026, time.October, 5, 23, 59)), now, false},
		{"sans le droit : la fermeture est ignorée", row(false, true, at(2026, time.October, 6, 12, 0)), now, false},
		{"payer en caisse désactivé : jamais fermé", row(true, false, at(2026, time.October, 6, 12, 0)), now, false},
		// Passage à l'heure d'hiver (journée de 25 h) : minuit local reste la frontière.
		{"jour de changement d'heure", row(true, true, at(2026, time.October, 25, 0, 30)), *at(2026, time.October, 25, 23, 30), true},
		{"lendemain du changement d'heure", row(true, true, at(2026, time.October, 25, 23, 30)), *at(2026, time.October, 26, 0, 30), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// closedAt est stocké en UTC (timestamptz) : la comparaison ne doit
			// pas dépendre du fuseau porté par la valeur lue.
			if tc.row.CardPaymentClosedAt != nil {
				utc := tc.row.CardPaymentClosedAt.UTC()
				tc.row.CardPaymentClosedAt = &utc
			}
			if got := cardPaymentClosedToday(tc.row, tc.now.UTC(), paris); got != tc.want {
				t.Fatalf("cardPaymentClosedToday = %v, want %v", got, tc.want)
			}
		})
	}
}

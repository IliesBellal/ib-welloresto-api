package accounting

import (
	"testing"
	"time"
)

// Les bornes de l'export comptable sont des dates de calendrier interprétées
// dans le fuseau de l'établissement. Ce test verrouille la conversion, qui est
// la source des écarts constatés : une date convertie en UTC côté client
// reculait la période d'un jour et faisait basculer le mois du rapport.
func TestParseLocalDate(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		wantUTC string
	}{
		{"heure d'été (UTC+2)", "2025-08-01", "2025-07-31 22:00:00"},
		{"heure d'hiver (UTC+1)", "2025-01-01", "2024-12-31 23:00:00"},
		{"horodatage hérité tronqué", "2025-08-01 17:42:13", "2025-07-31 22:00:00"},
		{"espaces superflus", "  2025-08-01  ", "2025-07-31 22:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLocalDate(tc.in, paris)
			if err != nil {
				t.Fatalf("parseLocalDate(%q) = %v", tc.in, err)
			}
			if utc := got.UTC().Format("2006-01-02 15:04:05"); utc != tc.wantUTC {
				t.Fatalf("parseLocalDate(%q).UTC() = %s, want %s", tc.in, utc, tc.wantUTC)
			}
			if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 {
				t.Fatalf("parseLocalDate(%q) doit pointer minuit local, got %02d:%02d:%02d", tc.in, h, m, s)
			}
		})
	}

	for _, invalid := range []string{"", "   ", "01/08/2025", "2025-8-1", "pas-une-date"} {
		if _, err := parseLocalDate(invalid, paris); err == nil {
			t.Fatalf("parseLocalDate(%q) doit échouer", invalid)
		}
	}
}

// La borne haute est le lendemain 00:00:00 local, exclusive. Elle doit couvrir
// la dernière seconde du dernier jour quelle que soit la durée réelle de la
// journée — 23h ou 25h les jours de changement d'heure.
func TestExportPeriodUpperBound(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	cases := []struct {
		name        string
		lastDay     string
		wantUTC     string
		wantDisplay string
	}{
		{"fin août", "2025-08-31", "2025-08-31 22:00:00", "31/08/2025 23:59:59"},
		{"fin décembre", "2025-12-31", "2025-12-31 23:00:00", "31/12/2025 23:59:59"},
		{"jour à 23h (passage à l'heure d'été)", "2025-03-30", "2025-03-30 22:00:00", "30/03/2025 23:59:59"},
		{"jour à 25h (passage à l'heure d'hiver)", "2025-10-26", "2025-10-26 23:00:00", "26/10/2025 23:59:59"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastDayLocal, err := parseLocalDate(tc.lastDay, paris)
			if err != nil {
				t.Fatalf("parseLocalDate: %v", err)
			}

			toExclusive := lastDayLocal.AddDate(0, 0, 1)
			if utc := toExclusive.UTC().Format("2006-01-02 15:04:05"); utc != tc.wantUTC {
				t.Fatalf("borne exclusive = %s UTC, want %s", utc, tc.wantUTC)
			}

			// Borne affichée sur le PDF.
			if display := toExclusive.Add(-time.Second).Format("02/01/2006 15:04:05"); display != tc.wantDisplay {
				t.Fatalf("borne affichée = %s, want %s", display, tc.wantDisplay)
			}

			// Une commande créée à 23h30 le dernier jour doit rester dans la période.
			lateOrder := time.Date(lastDayLocal.Year(), lastDayLocal.Month(), lastDayLocal.Day(), 23, 30, 0, 0, paris)
			if !lateOrder.Before(toExclusive) {
				t.Fatalf("commande à 23h30 le %s exclue de la période", tc.lastDay)
			}
		})
	}
}

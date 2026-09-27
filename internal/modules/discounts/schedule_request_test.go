package discounts

import (
	"encoding/json"
	"testing"
)

// Couvre la lecture des créneaux et de la date de fin envoyés par le
// back-office : ce qui est saisi doit correspondre à ce que le pricing
// évalue (jour ISO, heure locale [début, fin[, fin à 00:00 = minuit).
// Voir docs/KIOSK_DECISIONS.md (2026-09-27).

func TestCreateScheduleRequest_Unmarshal(t *testing.T) {
	cases := []struct {
		name     string
		json     string
		wantDay  int
		wantFrom string
		wantTo   string
		wantErr  bool
	}{
		{"lundi HH:MM", `{"day_of_week":1,"available_from":"11:00","available_to":"14:00"}`, 1, "11:00:00", "14:00:00", false},
		{"dimanche ISO 7", `{"day_of_week":7,"available_from":"11:00","available_to":"14:00"}`, 7, "11:00:00", "14:00:00", false},
		{"dimanche JS 0 ramené à 7", `{"day_of_week":0,"available_from":"11:00","available_to":"14:00"}`, 7, "11:00:00", "14:00:00", false},
		{"format HH:MM:SS accepté", `{"day_of_week":2,"available_from":"11:00:00","available_to":"14:30:00"}`, 2, "11:00:00", "14:30:00", false},
		{"fin à minuit", `{"day_of_week":5,"available_from":"19:00","available_to":"00:00"}`, 5, "19:00:00", "00:00:00", false},
		{"journée entière", `{"day_of_week":5,"available_from":"00:00","available_to":"00:00"}`, 5, "00:00:00", "00:00:00", false},
		{"jour hors plage", `{"day_of_week":8,"available_from":"11:00","available_to":"14:00"}`, 0, "", "", true},
		{"heure illisible (devenait 00:00)", `{"day_of_week":1,"available_from":"11h","available_to":"14:00"}`, 0, "", "", true},
		{"créneau à l'envers", `{"day_of_week":1,"available_from":"14:00","available_to":"11:00"}`, 0, "", "", true},
		{"créneau vide", `{"day_of_week":1,"available_from":"11:00","available_to":"11:00"}`, 0, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s CreateScheduleRequest
			err := json.Unmarshal([]byte(tc.json), &s)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("attendu une erreur, got %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if s.DayOfWeek != tc.wantDay || s.AvailableFrom.Format("15:04:05") != tc.wantFrom || s.AvailableTo.Format("15:04:05") != tc.wantTo {
				t.Fatalf("got day=%d %s-%s, want day=%d %s-%s", s.DayOfWeek,
					s.AvailableFrom.Format("15:04:05"), s.AvailableTo.Format("15:04:05"), tc.wantDay, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

func TestUpdateDiscountRequest_ValidToAndSchedules(t *testing.T) {
	t.Run("valid_to null explicite : retirer la date de fin", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"valid_to":null}`), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !r.ClearValidTo || r.ValidTo != nil {
			t.Fatalf("attendu ClearValidTo, got ClearValidTo=%v ValidTo=%v", r.ClearValidTo, r.ValidTo)
		}
	})

	t.Run("valid_to absent : ne rien modifier", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"discount_name":"x"}`), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if r.ClearValidTo || r.ValidTo != nil {
			t.Fatalf("aucune modification attendue, got ClearValidTo=%v ValidTo=%v", r.ClearValidTo, r.ValidTo)
		}
	})

	t.Run("valid_to date : nouvelle date de fin", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"valid_to":"2026-10-31"}`), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if r.ClearValidTo || r.ValidTo == nil || r.ValidTo.Format("2006-01-02") != "2026-10-31" {
			t.Fatalf("attendu 2026-10-31, got ClearValidTo=%v ValidTo=%v", r.ClearValidTo, r.ValidTo)
		}
	})

	t.Run("schedules vide : retirer tous les créneaux", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"schedules":[]}`), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if r.Schedules == nil || len(r.Schedules) != 0 {
			t.Fatalf("attendu une liste vide non nil, got %#v", r.Schedules)
		}
	})

	t.Run("schedules absent : ne rien modifier", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"discount_name":"x"}`), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if r.Schedules != nil {
			t.Fatalf("attendu nil, got %#v", r.Schedules)
		}
	})

	t.Run("créneau invalide : requête refusée", func(t *testing.T) {
		var r UpdateDiscountRequest
		if err := json.Unmarshal([]byte(`{"schedules":[{"day_of_week":1,"available_from":"14:00","available_to":"11:00"}]}`), &r); err == nil {
			t.Fatal("attendu une erreur pour un créneau à l'envers")
		}
	})
}

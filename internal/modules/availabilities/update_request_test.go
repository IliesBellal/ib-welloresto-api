package availabilities

import (
	"encoding/json"
	"testing"
)

// Clé absente = inchangé (nil) ; null ou [] = liste vidée (non nil, vide) ;
// voir docs/AVAILABILITIES_EMPTY_LISTS.md.
func TestUpdateAvailabilityRequest_UnmarshalEmptyLists(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantProducts  []string
		wantSchedules int // -1 = nil (inchangé)
	}{
		{"clés absentes (toggle actif)", `{"available":false}`, nil, -1},
		{"null explicite (ancien back-office)", `{"product_ids":null,"schedules":null}`, []string{}, 0},
		{"listes vides", `{"product_ids":[],"schedules":[]}`, []string{}, 0},
		{"listes remplies", `{"product_ids":["1"],"schedules":[{"day_of_week":1,"start_time":"11:00","end_time":"14:00"}]}`, []string{"1"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req UpdateAvailabilityRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if (tc.wantProducts == nil) != (req.ProductIDs == nil) || len(req.ProductIDs) != len(tc.wantProducts) {
				t.Fatalf("ProductIDs = %#v, want %#v", req.ProductIDs, tc.wantProducts)
			}
			if tc.wantSchedules == -1 {
				if req.Schedules != nil {
					t.Fatalf("Schedules = %#v, want nil", req.Schedules)
				}
			} else if req.Schedules == nil || len(req.Schedules) != tc.wantSchedules {
				t.Fatalf("Schedules = %#v, want %d non-nil", req.Schedules, tc.wantSchedules)
			}
		})
	}
}

func TestUpdateAvailabilityRequest_UnmarshalKeepsOtherFields(t *testing.T) {
	var req UpdateAvailabilityRequest
	if err := json.Unmarshal([]byte(`{"name":"Midi","available":true,"product_ids":null}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Name == nil || *req.Name != "Midi" || req.Available == nil || !*req.Available {
		t.Fatalf("other fields lost: %+v", req)
	}
}

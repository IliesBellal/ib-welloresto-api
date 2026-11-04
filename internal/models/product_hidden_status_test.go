package models

import "testing"

func TestProductEntry_IsHiddenByStatus(t *testing.T) {
	cases := map[string]bool{
		"not_available": true, "NOT_AVAILABLE": true, " 0 ": true, "false": true, "unavailable": true,
		"available": false, "1": false, "out_of_stock": false, "": false, "removed_from_menu": false,
	}
	for status, want := range cases {
		p := ProductEntry{Status: status}
		if got := p.IsHiddenByStatus(); got != want {
			t.Errorf("status %q : masqué = %v, attendu %v", status, got, want)
		}
	}
}

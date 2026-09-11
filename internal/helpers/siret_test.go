package helpers

import "testing"

// TestComputeVATNumber covers LOT A Semaine 3, Chantier 12's formula:
// FR + a two-digit key + the SIREN, key = (12 + 3 × (SIREN mod 97)) mod 97.
func TestComputeVATNumber(t *testing.T) {
	tests := []struct {
		siren string
		want  string
	}{
		{siren: "732829320", want: "FR44732829320"},
		// 000000000 mod 97 = 0 ; key = 12 mod 97 = 12.
		{siren: "000000000", want: "FR12000000000"},
	}
	for _, tt := range tests {
		got, ok := ComputeVATNumber(tt.siren)
		if !ok {
			t.Fatalf("ComputeVATNumber(%q): ok = false, want true", tt.siren)
		}
		if got != tt.want {
			t.Fatalf("ComputeVATNumber(%q) = %q, want %q", tt.siren, got, tt.want)
		}
	}
}

func TestComputeVATNumber_InvalidSiren(t *testing.T) {
	for _, siren := range []string{"", "12345", "1234567890", "12345678a"} {
		if _, ok := ComputeVATNumber(siren); ok {
			t.Fatalf("ComputeVATNumber(%q): ok = true, want false", siren)
		}
	}
}

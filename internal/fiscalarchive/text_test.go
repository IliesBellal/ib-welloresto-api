package fiscalarchive

import "testing"

func TestTextProtectsFormulas(t *testing.T) {
	for in, want := range map[string]string{
		"Pizza":      "Pizza",
		"=SOMME(A1)": "'=SOMME(A1)",
		"+33 6":      "'+33 6",
		"-promo":     "'-promo",
		"@user":      "'@user",
		"\tTab":      "'\tTab",
		"":           "",
		"Café = bon": "Café = bon",
	} {
		if got := text(in); got != want {
			t.Errorf("text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEuros(t *testing.T) {
	for in, want := range map[int64]string{0: "0,00", 5: "0,05", 1100: "11,00", -300: "-3,00", -5: "-0,05"} {
		if got := euros(in); got != want {
			t.Errorf("euros(%d) = %q, want %q", in, got, want)
		}
	}
}

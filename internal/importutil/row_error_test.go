package importutil

import "testing"

func TestRowErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"avec colonne", RowErrorf(42, "Prix", "montant %q illisible", "offert"), `ligne 42, colonne "Prix": montant "offert" illisible`},
		{"sans colonne", RowErrorf(7, "", "ligne incomprehensible"), "ligne 7: ligne incomprehensible"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

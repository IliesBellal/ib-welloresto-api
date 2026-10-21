package order_life_cycle

import "testing"

// lineKeyID filtre ce qu'un client envoie comme discount_id / reward_id avant
// qu'il n'atteigne une requête Postgres : toute valeur hors d'un integer
// positif ferait échouer la commande (22P02, 22003).
func TestLineKeyID(t *testing.T) {
	for raw, want := range map[string]string{
		"85":         "85",
		" 85 ":       "85",
		"0085":       "85",
		"2147483647": "2147483647",
	} {
		got, ok := lineKeyID(raw)
		if !ok || got != want {
			t.Errorf("lineKeyID(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "  ", "0", "-1", "abc", "8 5", "2147483648", "99999999999", "1.5", "discount-3a7e"} {
		if got, ok := lineKeyID(raw); ok {
			t.Errorf("lineKeyID(%q) = (%q, true), want rejected", raw, got)
		}
	}
}

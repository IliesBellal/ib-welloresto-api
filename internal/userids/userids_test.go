package userids

import (
	"context"
	"testing"
)

func TestIsNumeric(t *testing.T) {
	for id, want := range map[string]bool{
		"263":       true,
		"0":         true,
		"":          false,
		"-1":        false,
		"user-3a7e": false,
		"KIOSK":     false,
		"12a":       false,
	} {
		if got := IsNumeric(id); got != want {
			t.Errorf("IsNumeric(%q) = %v, want %v", id, got, want)
		}
	}
}

// Resolve ne doit interroger la base que pour un "user-…" : tout autre id
// (numérique, vide, sentinelle, id de test) revient tel quel sans requête —
// ici avec une connexion nil, qui ferait paniquer la moindre requête.
func TestResolve_NoQueryOutsideLegacyPrefix(t *testing.T) {
	for _, id := range []string{"263", "", "  ", "KIOSK", "-1", "itest-u1", "u1"} {
		if got := Resolve(context.Background(), nil, id); got != id {
			t.Errorf("Resolve(%q) = %q, want unchanged", id, got)
		}
	}
}

// Toute colonne de Columns doit être unique : une colonne listée deux fois
// serait réécrite deux fois et compterait double dans le rapport.
func TestColumns_Unique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Columns {
		if seen[c.String()] {
			t.Errorf("colonne en double dans Columns : %s", c)
		}
		seen[c.String()] = true
		if c.Table == "users" {
			t.Errorf("users.user_id est réécrit à part, en dernier : ne pas le lister dans Columns")
		}
	}
}

// Seules les deux colonnes des charges utiles signées (fiscal/payloads.go :
// PaymentPayload.UserID, AuditLogPayload.UserID) sont protégées. En ajouter
// une sans signature dans sa table ferait échouer Apply ; en oublier une
// casserait la chaîne fiscale.
func TestColumns_SealedAreExactlyFiscalPayloads(t *testing.T) {
	want := map[string]bool{"payments.user_id": true, "audit_logs.user_id": true}
	for _, c := range Columns {
		if c.Sealed != want[c.String()] {
			t.Errorf("%s : Sealed = %v, want %v", c, c.Sealed, want[c.String()])
		}
		delete(want, c.String())
	}
	for missing := range want {
		t.Errorf("colonne signée absente de Columns : %s", missing)
	}
}

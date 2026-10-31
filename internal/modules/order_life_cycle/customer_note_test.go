package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestCustomerNoteComment(t *testing.T) {
	named := func(n string) *models.CustomerRequest { return &models.CustomerRequest{Name: &n} }
	cases := []struct {
		name     string
		existing *string
		customer *models.CustomerRequest
		want     string
	}{
		{"nom, pas de commentaire", nil, named("Samantha K"), "Client : Samantha K"},
		{"nom après le texte du client", strp("Sans oignons"), named("Samantha K"), "Sans oignons\nClient : Samantha K"},
		{"nom après l'avertissement de rupture", strp("Rupture de stock : X (à vérifier avant d'accepter)\nSans oignons"), named(" Samantha K "),
			"Rupture de stock : X (à vérifier avant d'accepter)\nSans oignons\nClient : Samantha K"},
		{"commentaire blanc", strp("   "), named("Samantha K"), "Client : Samantha K"},
	}
	for _, c := range cases {
		got := customerNoteComment(c.existing, c.customer)
		if got == nil || *got != c.want {
			t.Errorf("%s : %v, want %q", c.name, got, c.want)
		}
	}

	// sans nom, le commentaire reste tel quel
	existing := strp("Sans oignons")
	if got := customerNoteComment(existing, named("  ")); got != existing {
		t.Errorf("sans nom le commentaire doit rester inchangé : %v", got)
	}
	if got := customerNoteComment(nil, nil); got != nil {
		t.Errorf("sans client ni commentaire : %v, want nil", got)
	}
}

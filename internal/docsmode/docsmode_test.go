package docsmode

import (
	"strings"
	"testing"
)

func TestRecipientRule(t *testing.T) {
	cases := []struct {
		name                 string
		testMode             bool
		owner, merchant      string
		wantTo               string
		wantNotice           bool
		wantNoticeMentioning string
	}{
		{"responsable en priorité", false, "patron@example.com", "resto@example.com", "patron@example.com", false, ""},
		{"repli sur l'établissement", false, "", "resto@example.com", "resto@example.com", false, ""},
		{"adresse du responsable vide", false, "  ", "resto@example.com", "resto@example.com", false, ""},
		{"aucune adresse", false, "", "", TestRecipient, true, "Chez Camille"},
		{"phase de test", true, "patron@example.com", "resto@example.com", TestRecipient, true, "patron@example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			to, notice := Recipient(c.testMode, "Chez Camille", c.owner, c.merchant)
			if to != c.wantTo {
				t.Errorf("destinataire = %q, want %q", to, c.wantTo)
			}
			if (notice != "") != c.wantNotice || !strings.Contains(notice, c.wantNoticeMentioning) {
				t.Errorf("note = %q", notice)
			}
		})
	}
}

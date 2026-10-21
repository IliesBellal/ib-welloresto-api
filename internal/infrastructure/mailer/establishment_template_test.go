package mailer

import (
	"strings"
	"testing"
)

var camille = Establishment{ID: "212", Name: "Chez Camille", Address: "1 rue de la Gare, 57000 Metz"}

// Chaque mail adressé au restaurateur porte le nom, l'adresse et le
// merchant_id de l'établissement : un groupe peut recevoir ceux de plusieurs
// établissements à la même adresse.
func TestRestaurateurMailsShowTheEstablishment(t *testing.T) {
	cases := map[string]interface{}{
		"payout_documents.html":     PayoutDocumentsData{Establishment: camille, MerchantName: camille.Name},
		"commission_invoice.html":   CommissionInvoiceData{Establishment: camille, MerchantName: camille.Name, PayoutCount: 2},
		"subscription_invoice.html": SubscriptionInvoiceData{Establishment: camille, MerchantName: camille.Name},
		"payout_notification.html":  PayoutData{Establishment: camille, MerchantName: camille.Name},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			html, err := RenderTemplate(name, data)
			if err != nil {
				t.Fatalf("RenderTemplate() error = %v", err)
			}
			for _, want := range []string{"Établissement : <strong>Chez Camille</strong>", "1 rue de la Gare, 57000 Metz", "Identifiant Wello Resto : 212"} {
				if !strings.Contains(html, want) {
					t.Errorf("mail sans %q", want)
				}
			}
			if strings.Contains(html, "<no value>") {
				t.Error("le mail contient \"<no value>\" : un nom de champ du template est faux")
			}
		})
	}
}

// Établissement inconnu (nom vide) : pas de bloc vide.
func TestEstablishmentBlockOmittedWithoutName(t *testing.T) {
	html, err := RenderTemplate("payout_documents.html", PayoutDocumentsData{})
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	if strings.Contains(html, "Établissement :") {
		t.Error("bloc établissement affiché sans nom")
	}
}

func TestEstablishmentSubjectSuffix(t *testing.T) {
	if got := camille.SubjectSuffix(); got != " — Chez Camille" {
		t.Errorf("SubjectSuffix() = %q", got)
	}
	if got := (Establishment{ID: "212"}).SubjectSuffix(); got != "" {
		t.Errorf("SubjectSuffix() sans nom = %q, want vide", got)
	}
}

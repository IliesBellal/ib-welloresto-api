package mailer

import (
	"strings"
	"testing"
)

func demoRequestFixture(origin string) DemoRequestData {
	return DemoRequestData{
		EmailBaseData: EmailBaseData{
			BrandName:    "Wello Resto",
			BrandLogoURL: BrandLogoURL,
			SupportEmail: SupportEmail,
			Year:         2026,
		},
		Establishment:      "Le Comptoir",
		RestaurantType:     "Pizzeria",
		Phone:              "0600000000",
		Situation:          "caisse_a_remplacer",
		Origin:             origin,
		Slot:               "lundi 28 septembre 2026 à 09:00",
		GoogleCalendarLink: "https://calendar.google.com/calendar/render?action=TEMPLATE",
	}
}

// L'e-mail interne de demande de démo affiche l'origine (utm_*) quand la
// demande vient d'une campagne — ex. le QR code du dépliant papier.
func TestRenderDemoRequestTemplateWithOrigin(t *testing.T) {
	html, err := RenderTemplate("demo_request.html", demoRequestFixture("depliant · print · dpl_sno_brn_2610"))
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	for _, want := range []string{"Origine", "dpl_sno_brn_2610", "Le Comptoir", "lundi 28 septembre 2026 à 09:00"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered email is missing %q", want)
		}
	}
	if strings.Contains(html, "<no value>") {
		t.Error("rendered email contains \"<no value>\" — a template field name is wrong")
	}
}

// Visite directe (sans utm_*) : pas de ligne « Origine » vide.
func TestRenderDemoRequestTemplateWithoutOrigin(t *testing.T) {
	html, err := RenderTemplate("demo_request.html", demoRequestFixture(""))
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	if strings.Contains(html, "Origine") {
		t.Error("rendered email shows an \"Origine\" row although no utm_* was sent")
	}
}

package mailer

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Données d'exemple de chaque template (docs/emails-styles.md). Un template
// ajouté dans templates/ doit avoir sa ligne ici : TestEveryTemplateHasAFixture
// le rappelle.
func templateFixtures() map[string]interface{} {
	base := EmailBaseData{BrandName: "Wello Resto", BrandLogoURL: BrandLogoURL, SupportEmail: SupportEmail, Year: 2026}
	brand := NewMerchantBrand("Trattoria Nonna Lucia", "https://example.com/logo.png", "#8C1C2B", "#ffffff")
	booking := BookingMessageData{
		EmailBaseData: base, Brand: brand, MerchantName: brand.Name, CustomerName: "Camille",
		BookingNumber: "R-48213", DateLabel: "samedi 7 novembre 2026", TimeLabel: "20:00", PartySize: 4,
		ManagementLink: "https://rsv.welloresto.fr/restaurant/nonna-lucia/booking/R-48213",
		CalendarLink:   "https://calendar.google.com/calendar/render?action=TEMPLATE",
	}
	est := Establishment{ID: "212", Name: "Le Petit Zinc", Address: "4 place Saint-Louis, 57000 Metz"}
	dunning := map[string]interface{}{"MerchantName": est.Name, "Establishment": est, "SuspensionDeadline": "09/11/2026"}
	return map[string]interface{}{
		"booking_confirmation.html":   booking,
		"booking_reminder.html":       booking,
		"booking_modification.html":   booking,
		"booking_cancellation.html":   booking,
		"booking_reconfirmation.html": booking,
		"booking_post_visit.html":     BookingPostVisitData{EmailBaseData: base, Brand: brand, MerchantName: brand.Name, CustomerName: "Camille"},
		"waitlist_available.html":     WaitlistAvailableData{EmailBaseData: base, Brand: brand, MerchantName: brand.Name, CustomerName: "Camille", PartySize: 2, ExpiryMinutes: 15},
		"scannorder_payout.html": ScanNOrderConfirmationData{
			Brand: brand, MerchantName: brand.Name, OrderTotal: "33.00", MerchantCurrency: "€", OrderDate: "26/10/2026",
			TrackingURL: "https://scannorder.welloresto.fr/suivi/abc", PrivacyURL: "https://welloresto.fr/confidentialite",
		},
		"customer_refund.html": RefundData{
			Brand: brand, MerchantName: brand.Name, Amount: "15.50 EUR", PaymentMethod: "visa", PaymentDetail: "•••• 4242",
			ReceiptURL: "https://pay.stripe.com/receipts/abc",
		},
		"invoice_email.html": InvoiceEmailData{Brand: brand, MerchantName: brand.Name, CustomerName: "Camille", ReceiptNumber: "F-2026-0042", OrderNumber: "1042"},
		"payout_notification.html": PayoutData{
			Establishment: est, MerchantName: est.Name, Amount: "1284.60 €", ArrivalDate: "23/10/2026", PayoutID: "po_1Q8xK2",
		},
		"payout_documents.html": PayoutDocumentsData{
			Establishment: est, MerchantName: est.Name, PayoutID: "po_1Q8xK2", Amount: "1 284,60 €", ArrivalDate: "23/10/2026",
			BackOfficeURL: "https://backoffice.welloresto.fr/payouts",
		},
		"commission_invoice.html": CommissionInvoiceData{
			Establishment: est, MerchantName: est.Name, InvoiceNumber: "WRC-2026-10-0007", Month: "octobre 2026",
			Amount: "42,00 €", PayoutCount: 4, BackOfficeURL: "https://backoffice.welloresto.fr/invoices",
		},
		"subscription_invoice.html": SubscriptionInvoiceData{
			Establishment: est, MerchantName: est.Name, InvoiceNumber: "WR-2026-10-0412", Amount: "89,00 €",
			Period: "du 01/10/2026 au 31/10/2026", HostedURL: "https://invoice.stripe.com/i/abc",
		},
		"attestation_email.html": map[string]string{
			"MerchantName": est.Name, "Software": "Wello Resto", "Version": "2.1.6", "SupportEmail": SupportEmail,
		},
		"dunning_first_notice.html":    map[string]interface{}{"MerchantName": est.Name, "Establishment": est},
		"dunning_second_notice.html":   dunning,
		"dunning_weekly_reminder.html": map[string]interface{}{"MerchantName": est.Name, "Establishment": est},
		"dunning_final_notice.html":    dunning,
		"trial_expiry_reminder.html": map[string]interface{}{
			"MerchantName": est.Name, "Establishment": est, "DaysRemaining": 7, "TrialEndsAt": "02/11/2026",
		},
		"demo_confirmation.html": DemoConfirmationData{
			EmailBaseData: base, Establishment: "Le Comptoir", DateLabel: "lundi 28 septembre 2026", TimeLabel: "9h00",
			Phone: "06 00 00 00 00", GoogleCalendarLink: "https://calendar.google.com/calendar/render?action=TEMPLATE",
		},
		"demo_request.html":   demoRequestFixture("depliant · print · dpl_sno_brn_2610"),
		"otp.html":            MFAMailData{EmailBaseData: base, MFACode: "482913", ExpiresIn: 10},
		"password_reset.html": PasswordResetMailData{EmailBaseData: base, FirstName: "Sarah", ResetURL: "https://backoffice.welloresto.fr/reset-password?token=abc", ExpiresIn: 30},
		"account_confirmation.html": ConfirmationEmailData{
			EmailBaseData: base, FirstName: "Sarah", ConfirmationURL: "https://backoffice.welloresto.fr/confirm?token=abc",
		},
		"planning_week_published.html": map[string]interface{}{
			"Brand": NewMerchantBrand(est.Name, "", "#1F5135", ""), "MerchantName": est.Name, "EmployeeName": "Yanis",
			"WeekLabel": "la semaine du 2 au 8 novembre", "PlanningURL": "https://backoffice.welloresto.fr/planning/weeks/42",
			"Shifts": []struct{ DayLabel, StartTime, EndTime, PositionLabel string }{
				{"Lundi 2", "11:00", "15:00", "Salle"}, {"Mercredi 4", "18:00", "23:00", ""},
			},
		},
	}
}

// Chaque template se rend sans erreur ni trou. MAILER_PREVIEW_DIR=<dossier>
// écrit les rendus dans ce dossier pour les ouvrir dans un navigateur.
func TestEveryTemplateRenders(t *testing.T) {
	previewDir := os.Getenv("MAILER_PREVIEW_DIR")
	for name, data := range templateFixtures() {
		t.Run(name, func(t *testing.T) {
			html, err := RenderTemplate(name, data)
			if err != nil {
				t.Fatalf("RenderTemplate() error = %v", err)
			}
			for _, bad := range []string{"<no value>", "%!", "<nil>"} {
				if strings.Contains(html, bad) {
					t.Errorf("le rendu contient %q : un champ ou un printf du template est faux", bad)
				}
			}
			if !strings.Contains(html, "<!DOCTYPE html>") || !strings.Contains(html, "</html>") {
				t.Error("document incomplet : doc_start / doc_end manquant")
			}
			if previewDir != "" {
				if err := os.WriteFile(filepath.Join(previewDir, name), []byte(html), 0o644); err != nil {
					t.Fatalf("écriture de l'aperçu : %v", err)
				}
			}
		})
	}
}

func TestEveryTemplateHasAFixture(t *testing.T) {
	fixtures := templateFixtures()
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "partial_") {
			continue
		}
		if _, ok := fixtures[e.Name()]; !ok {
			t.Errorf("%s n'a pas de données d'exemple dans templateFixtures", e.Name())
		}
	}
}

// Les relances d'abonnement sont restées des mois sans template : le rendu
// échouait au moment de l'envoi, journalisé puis ignoré. Tout nom de template
// cité dans le code Go doit exister dans templates/.
func TestTemplatesReferencedInCodeExist(t *testing.T) {
	ref := regexp.MustCompile(`"([a-z0-9_]+\.html)"`)
	root := filepath.Join("..", "..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range ref.FindAllSubmatch(src, -1) {
				name := string(m[1])
				if _, err := fs.Stat(templateFS, "templates/"+name); err != nil {
					t.Errorf("%s cite le template %s, absent de templates/", path, name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewMerchantBrand(t *testing.T) {
	cases := []struct {
		color, text, wantColor, wantText string
	}{
		{"#8C1C2B", "#ffffff", "#8C1C2B", "#ffffff"},
		{"", "", DefaultBrandColor, DefaultBrandTextColor},
		{"red", "", DefaultBrandColor, DefaultBrandTextColor},
		{"#F2C4CE", "", "#F2C4CE", "#1a1a1a"}, // rose clair : texte foncé
		{"#0036d9", "nope", "#0036d9", "#ffffff"},
	}
	for _, c := range cases {
		b := NewMerchantBrand("X", "javascript:alert(1)", c.color, c.text)
		if b.Color != c.wantColor || b.TextColor != c.wantText {
			t.Errorf("NewMerchantBrand(%q, %q) = %q / %q, want %q / %q", c.color, c.text, b.Color, b.TextColor, c.wantColor, c.wantText)
		}
		if b.LogoURL != "" {
			t.Errorf("logo non http(s) conservé : %q", b.LogoURL)
		}
	}
}

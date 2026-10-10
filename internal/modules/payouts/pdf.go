package payouts

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// parisLocation affiche les dates des justificatifs à l'heure de Paris, quel
// que soit le fuseau du serveur (repli UTC si la base de fuseaux manque).
var parisLocation = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Paris"); err == nil {
		return loc
	}
	return time.UTC
}()

func formatDate(t time.Time) string { return t.In(parisLocation).Format("02/01/2006") }

// formatEUR formate des centimes à la française : 123456 -> "1 234,56 €".
func formatEUR(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := fmt.Sprintf("%d", cents/100)
	var grouped []string
	for len(whole) > 3 {
		grouped = append([]string{whole[len(whole)-3:]}, grouped...)
		whole = whole[:len(whole)-3]
	}
	grouped = append([]string{whole}, grouped...)
	return fmt.Sprintf("%s%s,%02d €", sign, strings.Join(grouped, " "), cents%100)
}

// document enveloppe gofpdf : texte UTF-8 (traduit en cp1252) et mise en page
// commune aux deux justificatifs.
type document struct {
	pdf *gofpdf.Fpdf
	tr  func(string) string
}

func newDocument() *document {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false)
	pdf.SetMargins(10, 12, 10)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()
	return &document{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("cp1252")}
}

func (d *document) text(style string, size float64, align string, h float64, s string) {
	d.pdf.SetFont("Arial", style, size)
	d.pdf.CellFormat(190, h, d.tr(s), "", 1, align, false, 0, "")
}

func (d *document) lines(size float64, h float64, lines ...string) {
	d.pdf.SetFont("Arial", "", size)
	d.pdf.MultiCell(190, h, d.tr(strings.Join(lines, "\n")), "", "L", false)
}

func (d *document) section(title string) {
	d.pdf.Ln(4)
	d.text("B", 12, "L", 8, title)
}

// row imprime une ligne à deux colonnes (libellé / montant), éventuellement en gras.
func (d *document) row(bold bool, label, amount string) {
	style := ""
	if bold {
		style = "B"
	}
	d.pdf.SetFont("Arial", style, 10)
	d.pdf.CellFormat(130, 8, d.tr(label), "1", 0, "L", false, 0, "")
	d.pdf.CellFormat(60, 8, d.tr(amount), "1", 1, "R", false, 0, "")
}

func (d *document) bytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := d.pdf.Output(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func plural(n int, one, many string) string {
	if n > 1 {
		return many
	}
	return one
}

// buildStatementPDF produit le relevé de versement d'un payout : totaux par
// canal, déductions et net versé, sans le détail des commandes. invoice, s'il
// existe, est la facture de commission jointe au même envoi.
func buildStatementPDF(m *Merchant, ref Ref, s *Summary, invoice *Invoice) ([]byte, error) {
	d := newDocument()

	d.text("B", 16, "L", 9, "Relevé de versement")
	d.text("", 9, "L", 5, "Document récapitulatif établi par Wello Resto. Il ne tient pas lieu de facture.")
	if documentsTestMode {
		d.pdf.SetTextColor(180, 90, 0)
		d.text("B", 9, "L", 6, "SPÉCIMEN — document d'essai")
		d.pdf.SetTextColor(0, 0, 0)
	}

	d.section("Établissement")
	establishment := []string{m.Name}
	for _, kv := range [][2]string{{"Adresse", m.Address}, {"SIRET", m.SIRET}, {"N° TVA intracommunautaire", m.VATNumber}} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			establishment = append(establishment, kv[0]+" : "+v)
		}
	}
	d.lines(10, 5.5, establishment...)

	d.section("Versement")
	d.lines(10, 5.5,
		"Référence Stripe : "+ref.PayoutID,
		"Date d'arrivée sur votre compte : "+formatDate(ref.ArrivalDate),
		fmt.Sprintf("Période des ventes : du %s au %s", formatDate(s.PeriodStart), formatDate(s.PeriodEnd)),
	)

	d.section("Encaissements")
	for _, c := range s.Channels {
		if c.Channel == ChannelOther && c.Count == 0 {
			continue
		}
		d.row(false, fmt.Sprintf("%s — %d %s", c.Channel.Label(), c.Count, plural(c.Count, "paiement", "paiements")), formatEUR(c.Amount))
	}
	d.row(true, "Total des encaissements", formatEUR(s.Sales))

	d.section("Déductions et ajustements")
	d.row(false, fmt.Sprintf("Remboursements — %d %s", s.RefundCount, plural(s.RefundCount, "remboursement", "remboursements")), formatEUR(s.Refunds))
	if s.Adjustments != 0 {
		d.row(false, "Litiges et ajustements", formatEUR(s.Adjustments))
	}
	_, vat := splitVAT(s.Commission)
	d.row(false, fmt.Sprintf("Commission Wello Resto TTC (dont TVA %s)", formatEUR(vat)), formatEUR(-s.Commission))
	d.row(false, "Frais de traitement des paiements (Stripe)", formatEUR(-s.StripeFees))
	if !s.Reconciled() {
		d.row(false, "Écart non rapproché par Stripe", formatEUR(s.Discrepancy))
	}

	d.pdf.Ln(3)
	d.row(true, "Net versé sur votre compte bancaire", formatEUR(s.Net))

	if !s.Reconciled() {
		d.pdf.Ln(4)
		d.pdf.SetTextColor(180, 90, 0)
		d.lines(9, 5, "Attention : les lignes de ce versement n'expliquent pas entièrement son montant (écart ci-dessus). Contactez le support pour le faire vérifier.")
		d.pdf.SetTextColor(0, 0, 0)
	}

	if invoice != nil {
		d.pdf.Ln(4)
		d.lines(9, 5, fmt.Sprintf("La facture %s de la commission ci-dessus (%s TTC) est jointe à ce relevé.", invoice.Number, formatEUR(invoice.AmountTTC)))
	}

	d.pdf.Ln(6)
	d.text("I", 8, "R", 5, fmt.Sprintf("Document généré le %s", formatDate(time.Now())))
	return d.bytes()
}

// buildInvoicePDF produit la facture de la commission de la plateforme sur un
// payout, acquittée par prélèvement sur les encaissements.
func buildInvoicePDF(m *Merchant, ref Ref, inv *Invoice) ([]byte, error) {
	d := newDocument()

	d.text("B", 16, "L", 9, "FACTURE")
	d.text("B", 11, "L", 6, "N° "+inv.Number)
	d.text("", 10, "L", 5.5, "Date d'émission : "+formatDate(inv.IssuedAt))
	if inv.Series == seriesTest {
		d.pdf.SetTextColor(180, 90, 0)
		d.text("B", 9, "L", 6, "SPÉCIMEN — facture d'essai, sans valeur comptable")
		d.pdf.SetTextColor(0, 0, 0)
	}

	d.section("Émetteur")
	d.lines(10, 5.5, issuer.lines()...)

	d.section("Client")
	client := []string{m.Name}
	for _, kv := range [][2]string{{"Adresse", m.Address}, {"SIRET", m.SIRET}, {"N° TVA intracommunautaire", m.VATNumber}} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			client = append(client, kv[0]+" : "+v)
		}
	}
	d.lines(10, 5.5, client...)

	d.section("Prestation")
	d.lines(10, 5.5,
		"Commission de service Wello Resto (commandes en ligne et bornes de commande)",
		fmt.Sprintf("Encaissements du %s au %s, versement Stripe du %s (réf. %s)",
			formatDate(inv.PeriodStart), formatDate(inv.PeriodEnd), formatDate(ref.ArrivalDate), ref.PayoutID),
	)

	d.pdf.Ln(3)
	d.row(false, "Total HT", formatEUR(inv.AmountHT))
	d.row(false, fmt.Sprintf("TVA %d %%", vatRatePercent), formatEUR(inv.AmountVAT))
	d.row(true, "Total TTC", formatEUR(inv.AmountTTC))

	d.pdf.Ln(5)
	d.lines(9, 5, fmt.Sprintf("Facture acquittée : montant prélevé sur les encaissements du restaurateur lors du versement Stripe du %s.", formatDate(ref.ArrivalDate)))
	return d.bytes()
}

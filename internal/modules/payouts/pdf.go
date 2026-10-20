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

var frenchMonths = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

// monthLabel : "octobre 2026".
func monthLabel(t time.Time) string {
	t = t.In(parisLocation)
	return fmt.Sprintf("%s %d", frenchMonths[t.Month()-1], t.Year())
}

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
// commune aux justificatifs.
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

// specimen imprime le bandeau d'essai en phase de test.
func (d *document) specimen(label string) {
	d.pdf.SetTextColor(180, 90, 0)
	d.text("B", 9, "L", 6, label)
	d.pdf.SetTextColor(0, 0, 0)
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

// identityLines liste les mentions d'un établissement, sans les lignes vides.
func identityLines(m *Merchant) []string {
	out := []string{m.Name}
	for _, kv := range [][2]string{{"Adresse", m.Address}, {"SIRET", m.SIRET}, {"N° TVA intracommunautaire", m.VATNumber}} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			out = append(out, kv[0]+" : "+v)
		}
	}
	return out
}

// buildStatementPDF produit le relevé de versement d'un payout : totaux par
// canal, déductions et net versé, sans le détail des commandes.
func buildStatementPDF(m *Merchant, ref Ref, s *Summary) ([]byte, error) {
	d := newDocument()

	d.text("B", 16, "L", 9, "Relevé de versement")
	d.text("", 9, "L", 5, "Document récapitulatif établi par Wello Resto. Il ne tient pas lieu de facture.")
	if statementsTestMode {
		d.specimen("SPÉCIMEN — document d'essai")
	}

	d.section("Établissement")
	d.lines(10, 5.5, identityLines(m)...)

	d.section("Versement")
	d.lines(10, 5.5,
		"Référence de versement : "+ref.PayoutID,
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
	d.row(false, "Frais de transactions", formatEUR(-(s.Commission + s.StripeFees)))
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

	d.pdf.Ln(6)
	d.text("I", 8, "R", 5, fmt.Sprintf("Document généré le %s", formatDate(time.Now())))
	return d.bytes()
}

// buildInvoicePDF produit la facture mensuelle de la commission de la
// plateforme, acquittée par prélèvement à la source sur les encaissements.
func buildInvoicePDF(m *Merchant, inv *Invoice) ([]byte, error) {
	d := newDocument()

	d.text("B", 16, "L", 9, "FACTURE")
	d.text("B", 11, "L", 6, "N° "+inv.Number)
	d.text("", 10, "L", 5.5, "Date d'émission : "+formatDate(inv.IssuedAt))
	if inv.Series == seriesTest {
		d.specimen("SPÉCIMEN — facture d'essai, sans valeur comptable")
	}

	d.section("Émetteur")
	d.lines(10, 5.5, issuer.lines()...)

	d.section("Client")
	d.lines(10, 5.5, identityLines(m)...)

	d.section("Prestation")
	d.lines(10, 5.5,
		"Commission de service Wello Resto (commandes en ligne et bornes de commande)",
		fmt.Sprintf("%s : %d %s Stripe arrivé%s du %s au %s",
			monthLabel(inv.PeriodMonth), inv.PayoutCount, plural(inv.PayoutCount, "versement", "versements"),
			plural(inv.PayoutCount, "", "s"), formatDate(inv.PeriodStart), formatDate(inv.PeriodEnd)),
		"Le détail de chaque versement figure dans le récapitulatif joint.",
	)

	d.pdf.Ln(3)
	d.row(false, "Total HT", formatEUR(inv.AmountHT))
	d.row(false, fmt.Sprintf("TVA %d %%", vatRatePercent), formatEUR(inv.AmountVAT))
	d.row(true, "Total TTC", formatEUR(inv.AmountTTC))

	d.pdf.Ln(5)
	d.lines(9, 5, "Facture acquittée : montant prélevé à la source sur les encaissements du restaurateur, avant chaque versement Stripe.")
	return d.bytes()
}

// recapColumns : largeurs des colonnes du récapitulatif (190 mm au total).
var recapColumns = [...]float64{20, 50, 24, 22, 24, 24, 26}

func (d *document) recapHeader() {
	d.pdf.SetFont("Arial", "B", 8)
	d.pdf.SetFillColor(235, 235, 235)
	for i, label := range []string{"Arrivée", "Référence Stripe", "Encaissements", "Remb. et ajust.", "Commission TTC", "Frais Stripe", "Net versé"} {
		align := "R"
		if i < 2 {
			align = "L"
		}
		d.pdf.CellFormat(recapColumns[i], 7, d.tr(label), "1", 0, align, true, 0, "")
	}
	d.pdf.Ln(-1)
}

func (d *document) recapRow(bold bool, cells [7]string) {
	style := ""
	if bold {
		style = "B"
	}
	for i, cell := range cells {
		size := 8.0
		align := "R"
		if i == 1 {
			size = 7 // la référence Stripe est longue
		}
		if i < 2 {
			align = "L"
		}
		d.pdf.SetFont("Arial", style, size)
		d.pdf.CellFormat(recapColumns[i], 6.5, d.tr(cell), "1", 0, align, false, 0, "")
	}
	d.pdf.Ln(-1)
}

// buildRecapPDF produit le récapitulatif mensuel des commissions : une ligne
// par virement du mois, dont le total de commission égale celui de la facture.
func buildRecapPDF(m *Merchant, inv *Invoice, lines []Line) ([]byte, error) {
	d := newDocument()

	d.text("B", 16, "L", 9, "Récapitulatif des commissions — "+monthLabel(inv.PeriodMonth))
	d.text("", 9, "L", 5, "Document récapitulatif établi par Wello Resto, joint à la facture "+inv.Number+".")
	if inv.Series == seriesTest {
		d.specimen("SPÉCIMEN — document d'essai")
	}

	d.section("Établissement")
	d.lines(10, 5.5, identityLines(m)...)

	d.section("Versements du mois")
	d.recapHeader()

	var sales, other, commission, fees, net int64
	discrepancy := false
	for _, l := range lines {
		sales += l.Sales()
		other += l.Refunds + l.Adjustments
		commission += l.Commission
		fees += l.StripeFees
		net += l.Net
		discrepancy = discrepancy || l.Discrepancy != 0
		d.recapRow(false, [7]string{
			formatDate(l.ArrivalDate), l.PayoutID, formatEUR(l.Sales()), formatEUR(l.Refunds + l.Adjustments),
			formatEUR(l.Commission), formatEUR(l.StripeFees), formatEUR(l.Net),
		})
	}
	d.recapRow(true, [7]string{
		"Total", fmt.Sprintf("%d %s", len(lines), plural(len(lines), "versement", "versements")),
		formatEUR(sales), formatEUR(other), formatEUR(commission), formatEUR(fees), formatEUR(net),
	})

	d.section("Facture")
	d.lines(10, 5.5, fmt.Sprintf("Commission du mois : %s HT + %s de TVA (%d %%) = %s TTC, facture %s.",
		formatEUR(inv.AmountHT), formatEUR(inv.AmountVAT), vatRatePercent, formatEUR(inv.AmountTTC), inv.Number))
	if commission != inv.AmountTTC {
		d.pdf.SetTextColor(180, 90, 0)
		d.lines(9, 5, fmt.Sprintf("Attention : le total des commissions listées (%s) diffère du montant facturé.", formatEUR(commission)))
		d.pdf.SetTextColor(0, 0, 0)
	}
	if discrepancy {
		d.pdf.SetTextColor(180, 90, 0)
		d.lines(9, 5, "Certains versements présentent un écart non rapproché par Stripe : voir leur relevé.")
		d.pdf.SetTextColor(0, 0, 0)
	}

	d.pdf.Ln(6)
	d.text("I", 8, "R", 5, fmt.Sprintf("Document généré le %s", formatDate(time.Now())))
	return d.bytes()
}

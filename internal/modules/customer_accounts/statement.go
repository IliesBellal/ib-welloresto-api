package customer_accounts

import (
	"bytes"
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/jung-kurt/gofpdf"
)

// Relevé de compte client : solde au début de la période, mouvements et
// commandes réglées sur le compte (avec leur ticket), solde à la fin.

// statementLine : une ligne du relevé.
type statementLine struct {
	Date    time.Time
	Label   string
	Delta   int64
	Balance int64
}

// statement : contenu d'un relevé.
type statement struct {
	From, To time.Time // jours locaux, bornes incluses
	Opening  int64
	Lines    []statementLine
	Closing  int64
}

// parsePeriod lit les dates AAAA-MM-JJ du relevé (défaut : les trois derniers
// mois, jusqu'à aujourd'hui).
func parsePeriod(fromRaw, toRaw string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	to := today
	from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -2, 0)
	if strings.TrimSpace(toRaw) != "" {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(toRaw), loc)
		if err != nil {
			return from, to, fmt.Errorf("%w: to", models.ErrInvalidInput)
		}
		to = t
	}
	if strings.TrimSpace(fromRaw) != "" {
		f, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(fromRaw), loc)
		if err != nil {
			return from, to, fmt.Errorf("%w: from", models.ErrInvalidInput)
		}
		from = f
	}
	if from.After(to) {
		return from, to, fmt.Errorf("%w: period", models.ErrInvalidInput)
	}
	return from, to, nil
}

// buildStatement découpe l'historique d'un compte sur une période.
func buildStatement(entries []models.CustomerAccountEntry, from, to time.Time) statement {
	list := append([]models.CustomerAccountEntry(nil), entries...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Date < list[j].Date })
	end := to.AddDate(0, 0, 1)
	st := statement{From: from, To: to}
	var balance int64
	for _, e := range list {
		at := time.Unix(e.Date, 0).In(from.Location())
		if !at.Before(end) {
			break
		}
		balance += e.Delta
		if at.Before(from) {
			st.Opening = balance
			continue
		}
		st.Lines = append(st.Lines, statementLine{Date: at, Label: entryLabel(e), Delta: e.Delta, Balance: balance})
	}
	st.Closing = balance
	return st
}

var operationLabels = map[string]string{
	models.AccountOperationDeposit:  "Versement",
	models.AccountOperationRefund:   "Remboursement du solde",
	models.AccountOperationWriteOff: "Abandon de créance",
}

// entryLabel : libellé d'une ligne d'historique.
func entryLabel(e models.CustomerAccountEntry) string {
	if op := e.Operation; op != nil {
		label := operationLabels[op.OperationType]
		if label == "" {
			label = "Mouvement"
		}
		if op.MOP != nil {
			label += " · " + models.AccountMOPLabel(*op.MOP)
		}
		return label
	}
	if p := e.Payment; p != nil {
		label := "Commande n° " + p.OrderNum
		if p.ReceiptNumber != "" {
			label += " (ticket " + p.ReceiptNumber + ")"
		}
		if e.Delta > 0 {
			label += " remboursée"
		}
		return label
	}
	return "Mouvement"
}

// formatMoney : 1234,50 €.
func formatMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d,%02d €", sign, cents/100, cents%100)
}

// balanceLabel : « Avance 12,00 € », « Ardoise 8,50 € », « Soldé ».
func balanceLabel(balance int64) string {
	switch {
	case balance > 0:
		return "Avance " + formatMoney(balance)
	case balance < 0:
		return "Ardoise " + formatMoney(-balance)
	}
	return "Soldé"
}

// renderStatementPDF met le relevé en page (A4).
func renderStatementPDF(m MerchantInfo, c CustomerContact, st statement) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(190, 8, tr(m.Name), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	for _, line := range []string{m.Address, siretLine(m.SIRET), m.Phone} {
		if strings.TrimSpace(line) != "" {
			pdf.CellFormat(190, 5, tr(line), "", 1, "L", false, 0, "")
		}
	}
	pdf.Ln(6)
	pdf.SetFont("Arial", "B", 13)
	pdf.CellFormat(190, 8, tr("Relevé de compte client"), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(190, 6, tr("Client : "+c.Name), "", 1, "L", false, 0, "")
	pdf.CellFormat(190, 6, tr(fmt.Sprintf("Période : du %s au %s", st.From.Format("02/01/2006"), st.To.Format("02/01/2006"))), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	header := func() {
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(25, 7, "Date", "1", 0, "L", false, 0, "")
		pdf.CellFormat(85, 7, tr("Opération"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 7, tr("Débit"), "1", 0, "R", false, 0, "")
		pdf.CellFormat(25, 7, tr("Crédit"), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 7, "Solde", "1", 1, "R", false, 0, "")
		pdf.SetFont("Arial", "", 9)
	}
	header()
	pdf.CellFormat(110, 7, tr(fmt.Sprintf("Solde au %s", st.From.Format("02/01/2006"))), "1", 0, "L", false, 0, "")
	pdf.CellFormat(50, 7, "", "1", 0, "R", false, 0, "")
	pdf.CellFormat(30, 7, tr(formatMoney(st.Opening)), "1", 1, "R", false, 0, "")
	for _, l := range st.Lines {
		if pdf.GetY() > 270 {
			pdf.AddPage()
			header()
		}
		debit, credit := "", ""
		if l.Delta < 0 {
			debit = formatMoney(-l.Delta)
		} else {
			credit = formatMoney(l.Delta)
		}
		label := l.Label
		if len([]rune(label)) > 52 {
			label = string([]rune(label)[:51]) + "…"
		}
		pdf.CellFormat(25, 7, l.Date.Format("02/01/2006"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(85, 7, tr(label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 7, tr(debit), "1", 0, "R", false, 0, "")
		pdf.CellFormat(25, 7, tr(credit), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 7, tr(formatMoney(l.Balance)), "1", 1, "R", false, 0, "")
	}
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(160, 7, tr(fmt.Sprintf("Solde au %s", st.To.Format("02/01/2006"))), "1", 0, "L", false, 0, "")
	pdf.CellFormat(30, 7, tr(formatMoney(st.Closing)), "1", 1, "R", false, 0, "")

	pdf.Ln(6)
	pdf.SetFont("Arial", "", 10)
	pdf.MultiCell(190, 5, tr(balanceLabel(st.Closing)+"."), "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("Arial", "I", 8)
	pdf.MultiCell(190, 4, tr("Un solde positif est une avance utilisable sur vos prochaines commandes ; un solde négatif "+
		"reste à régler. Les versements ne sont pas des ventes : la TVA figure sur le ticket de chaque commande. "+
		"Ce relevé n'est pas une facture."), "", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func siretLine(siret string) string {
	if strings.TrimSpace(siret) == "" {
		return ""
	}
	return "SIRET " + strings.TrimSpace(siret)
}

// statementFor prépare le relevé d'un client.
func (s *Service) statementFor(ctx context.Context, merchantID string, customerID int64, fromRaw, toRaw string) (MerchantInfo, CustomerContact, statement, error) {
	if err := s.requireCustomer(ctx, merchantID, customerID); err != nil {
		return MerchantInfo{}, CustomerContact{}, statement{}, err
	}
	m, err := s.repo.Merchant(ctx, merchantID)
	if err != nil {
		return m, CustomerContact{}, statement{}, err
	}
	from, to, err := parsePeriod(fromRaw, toRaw, s.deps.Now(), m.Loc)
	if err != nil {
		return m, CustomerContact{}, statement{}, err
	}
	c, err := s.repo.Contact(ctx, merchantID, customerID)
	if err != nil {
		return m, c, statement{}, err
	}
	entries, err := s.repo.Entries(ctx, merchantID, customerID)
	if err != nil {
		return m, c, statement{}, err
	}
	return m, c, buildStatement(entries, from, to), nil
}

// StatementPDF renvoie le relevé PDF d'un client et son nom de fichier.
func (s *Service) StatementPDF(ctx context.Context, customerID int64, fromRaw, toRaw string) ([]byte, string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, "", models.ErrUnauthorized
	}
	m, c, st, err := s.statementFor(ctx, user.MerchantID, customerID, fromRaw, toRaw)
	if err != nil {
		return nil, "", err
	}
	pdf, err := renderStatementPDF(m, c, st)
	if err != nil {
		return nil, "", err
	}
	return pdf, statementFileName(customerID, st), nil
}

func statementFileName(customerID int64, st statement) string {
	return fmt.Sprintf("releve_compte_%d_%s_%s.pdf", customerID, st.From.Format("20060102"), st.To.Format("20060102"))
}

// SendStatement envoie le relevé PDF par email (à email, sinon à l'adresse du
// client) et renvoie l'adresse utilisée.
func (s *Service) SendStatement(ctx context.Context, customerID int64, fromRaw, toRaw, email string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}
	if s.deps.Mailer == nil {
		return "", fmt.Errorf("customer_accounts: no mailer configured")
	}
	m, c, st, err := s.statementFor(ctx, user.MerchantID, customerID, fromRaw, toRaw)
	if err != nil {
		return "", err
	}
	to := strings.TrimSpace(email)
	if to == "" {
		to = c.Email
	}
	if addr, err := mail.ParseAddress(to); err != nil || addr.Address != to {
		return "", models.ErrAccountNoContact
	}
	pdf, err := renderStatementPDF(m, c, st)
	if err != nil {
		return "", err
	}
	data := mailer.CustomerAccountEmailData{
		Brand: s.brand(ctx, user.MerchantID, m.Name), MerchantName: m.Name, CustomerName: c.Name,
		BalanceLabel: balanceLabel(st.Closing),
		PeriodLabel:  fmt.Sprintf("du %s au %s", st.From.Format("02/01/2006"), st.To.Format("02/01/2006")),
	}
	if st.Closing < 0 {
		data.AmountDue = formatMoney(-st.Closing)
		data.PaymentURL = s.paymentURLIfAvailable(ctx, user.MerchantID, customerID)
	}
	s.deps.Mailer.SendAsyncWithAttachment(m.Name, mailer.InvoiceEmail, to, "Relevé de votre compte · "+m.Name,
		"customer_account_statement.html", data, pdf, statementFileName(customerID, st))
	return to, nil
}

// brand : identité visuelle des emails de l'établissement.
func (s *Service) brand(ctx context.Context, merchantID, name string) mailer.MerchantBrand {
	if s.deps.Brand == nil {
		return mailer.NewMerchantBrand(name, "", "", "")
	}
	return s.deps.Brand.Resolve(ctx, merchantID, name)
}

package payouts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/docsmode"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

var day = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

// sale : un paiement de 10,00 € dont 0,20 € de commission Wello Resto et
// 0,31 € de frais Stripe (la forme réelle d'une ligne de compte connecté).
func sale(id, pi string, created time.Time) stripeclient.PayoutTransaction {
	return stripeclient.PayoutTransaction{
		ID: id, Type: "charge", ReportingCategory: "charge",
		Amount: 1000, CommissionFee: 20, StripeFee: 31, Net: 949,
		Created: created, PaymentIntentID: pi,
	}
}

func TestAggregate_SplitsSalesByChannelAndReconciles(t *testing.T) {
	txs := []stripeclient.PayoutTransaction{
		sale("txn_1", "pi_online", day.AddDate(0, 0, -20)),
		sale("txn_2", "pi_online", day.AddDate(0, 0, -10)),
		sale("txn_3", "pi_kiosk", day.AddDate(0, 0, -5)),
		sale("txn_4", "pi_unknown", day.AddDate(0, 0, -3)),
		{ID: "txn_r", Type: "refund", ReportingCategory: "refund", Amount: -300, CommissionFee: -6, StripeFee: 0, Net: -294, Created: day.AddDate(0, 0, -2), PaymentIntentID: "pi_online"},
		{ID: "txn_p", Type: "payout", Amount: -3000, Net: -3000}, // la ligne du payout lui-même est ignorée
	}
	channels := map[string]Channel{"pi_online": ChannelScanNOrder, "pi_kiosk": ChannelKiosk}
	// 4 ventes nettes 949 chacune + remboursement net -294
	const payout = 4*949 - 294

	s := Aggregate(txs, channels, payout)
	if !s.Reconciled() {
		t.Fatalf("écart inattendu : %d", s.Discrepancy)
	}

	byChannel := map[Channel]ChannelTotal{}
	for _, c := range s.Channels {
		byChannel[c.Channel] = c
	}
	if got := byChannel[ChannelScanNOrder]; got.Count != 2 || got.Amount != 2000 {
		t.Errorf("ScanNOrder = %+v, want 2 ventes / 2000", got)
	}
	if got := byChannel[ChannelKiosk]; got.Count != 1 || got.Amount != 1000 {
		t.Errorf("Borne = %+v, want 1 vente / 1000", got)
	}
	if got := byChannel[ChannelOther]; got.Count != 1 || got.Amount != 1000 {
		t.Errorf("Autres = %+v, want 1 vente / 1000 (PaymentIntent inconnu)", got)
	}
	if s.Sales != 4000 || s.Refunds != -300 || s.RefundCount != 1 {
		t.Errorf("ventes %d / remboursements %d (%d), want 4000 / -300 (1)", s.Sales, s.Refunds, s.RefundCount)
	}
	if s.Commission != 4*20-6 || s.StripeFees != 4*31 {
		t.Errorf("commission %d / frais %d, want %d / %d", s.Commission, s.StripeFees, 4*20-6, 4*31)
	}
	if s.Net != payout {
		t.Errorf("net = %d, want %d", s.Net, payout)
	}
	if !s.PeriodStart.Equal(day.AddDate(0, 0, -20)) || !s.PeriodEnd.Equal(day.AddDate(0, 0, -3)) {
		t.Errorf("période = %v → %v, want celle des ventes", s.PeriodStart, s.PeriodEnd)
	}
}

func TestAggregate_DiscrepancyIsReportedNotHidden(t *testing.T) {
	// le payout de 5000 n'est expliqué qu'à hauteur de 949 : l'écart est 4051
	s := Aggregate([]stripeclient.PayoutTransaction{sale("txn_1", "pi_1", day)}, nil, 5000)

	if s.Reconciled() || s.Discrepancy != 5000-949 {
		t.Errorf("écart = %d (rapproché=%v), want %d", s.Discrepancy, s.Reconciled(), 5000-949)
	}
	if s.Net != 5000 {
		t.Errorf("net = %d, want le montant du payout", s.Net)
	}
}

func TestAggregate_UnknownLineTypesGoToAdjustments(t *testing.T) {
	txs := []stripeclient.PayoutTransaction{
		sale("txn_1", "pi_1", day),
		{ID: "txn_d", Type: "adjustment", ReportingCategory: "dispute", Amount: -1500, Net: -1500, Created: day},
	}
	s := Aggregate(txs, nil, 949-1500)
	if !s.Reconciled() {
		t.Fatalf("écart inattendu : %d", s.Discrepancy)
	}
	if s.Adjustments != -1500 {
		t.Errorf("ajustements = %d, want -1500", s.Adjustments)
	}
}

func TestSplitVAT(t *testing.T) {
	for _, tc := range []struct{ ttc, ht, vat int64 }{
		{120, 100, 20},
		{20, 17, 3},
		{1, 1, 0},
		{0, 0, 0},
		{-120, -100, -20},
	} {
		ht, vat := splitVAT(tc.ttc)
		if ht != tc.ht || vat != tc.vat {
			t.Errorf("splitVAT(%d) = %d/%d, want %d/%d", tc.ttc, ht, vat, tc.ht, tc.vat)
		}
		if ht+vat != tc.ttc {
			t.Errorf("splitVAT(%d): HT+TVA = %d", tc.ttc, ht+vat)
		}
	}
}

func TestFormatEUR(t *testing.T) {
	for cents, want := range map[int64]string{
		0: "0,00 €", 5: "0,05 €", 123456: "1 234,56 €", 100000000: "1 000 000,00 €", -2050: "-20,50 €",
	} {
		if got := formatEUR(cents); got != want {
			t.Errorf("formatEUR(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestMonthLabel(t *testing.T) {
	// le 1er octobre à minuit à Paris est le 30 septembre 22h UTC : le mois affiché reste octobre
	if got := monthLabel(time.Date(2026, 10, 1, 0, 0, 0, 0, parisLocation)); got != "octobre 2026" {
		t.Errorf("monthLabel = %q", got)
	}
	if got := monthLabel(time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)); got != "février 2026" {
		t.Errorf("monthLabel = %q", got)
	}
}

func TestChannelOf(t *testing.T) {
	for _, tc := range []struct {
		source   string
		hasKiosk bool
		want     Channel
	}{
		{"SCANNORDER", false, ChannelScanNOrder},
		{"KIOSK", false, ChannelKiosk},
		{"SCANNORDER", true, ChannelKiosk},
		{"WELLO_RESTO_POS", false, ChannelOther},
		{"", false, ChannelOther},
	} {
		if got := channelOf(tc.source, tc.hasKiosk); got != tc.want {
			t.Errorf("channelOf(%q, %v) = %q, want %q", tc.source, tc.hasKiosk, got, tc.want)
		}
	}
}

func TestIssuerLinesOmitUnknownMentions(t *testing.T) {
	text := strings.Join(issuer.lines(), " | ")

	for _, want := range []string{"Wello Resto (BINYA SAS)", "SIREN : 103020558", "Capital social : 1 000 €", "13 rue Principale, 57450 Theding"} {
		if !strings.Contains(text, want) {
			t.Errorf("mentions de l'émetteur sans %q : %s", want, text)
		}
	}
	// RCS et n° de TVA ne sont pas connus : leurs lignes sont omises, pas imprimées vides
	if strings.Contains(text, "RCS") || strings.Contains(text, "TVA") || strings.Contains(text, "COMPLÉTER") {
		t.Errorf("une mention inconnue est imprimée : %s", text)
	}
}

func TestRecipientFor(t *testing.T) {
	m := &Merchant{Name: "Chez Camille", Email: "camille@example.com"}
	to, notice := recipientFor(m)
	if to != testRecipient || !strings.Contains(notice, "Chez Camille") {
		t.Errorf("recipientFor = %q, %q (phase de test : tout part chez %s)", to, notice, testRecipient)
	}
}

// ---- Faux ----

type fakeRepo struct {
	pending  []Pending
	merchant *Merchant
	channels map[string]Channel
	done     map[string]Done
	failures map[string]string
	listed   map[string][]Listed

	// facturation mensuelle
	candidates   []string
	lines        map[string][]Line // virements à facturer, par établissement
	skipped      []string
	issued       []NewInvoice
	monthTaken   bool
	invoices     []*Invoice
	invoiceSent  map[int64]InvoiceSent
	invoiceFails map[int64]string
	invoiceLines []Line
}

func (r *fakeRepo) InsertPending(_ context.Context, ref Ref) error {
	r.pending = append(r.pending, Pending{Ref: ref})
	return nil
}
func (r *fakeRepo) ListPending(context.Context, int) ([]Pending, error) { return r.pending, nil }
func (r *fakeRepo) RecordFailure(_ context.Context, id, reason string, _ bool) error {
	r.failures[id] = reason
	return nil
}
func (r *fakeRepo) MarkDone(_ context.Context, id string, d Done) error { r.done[id] = d; return nil }
func (r *fakeRepo) ChannelsByPaymentIntent(context.Context, []string) (map[string]Channel, error) {
	return r.channels, nil
}
func (r *fakeRepo) MerchantByStripeAccount(context.Context, string) (*Merchant, error) {
	return r.merchant, nil
}
func (r *fakeRepo) MerchantByID(context.Context, string) (*Merchant, error) { return r.merchant, nil }
func (r *fakeRepo) ConnectedAccounts(context.Context) ([]string, error)     { return nil, nil }
func (r *fakeRepo) ListForMerchant(_ context.Context, merchantID string, _ int) ([]Listed, error) {
	return r.listed[merchantID], nil
}
func (r *fakeRepo) GetForMerchant(_ context.Context, merchantID, payoutID string) (*Listed, error) {
	for _, l := range r.listed[merchantID] {
		if l.PayoutID == payoutID {
			return &l, nil
		}
	}
	return nil, nil
}
func (r *fakeRepo) InvoiceCandidates(context.Context, time.Time, time.Time) ([]string, error) {
	return r.candidates, nil
}
func (r *fakeRepo) PayoutsToInvoice(_ context.Context, merchantID string, _ time.Time) ([]Line, error) {
	return r.lines[merchantID], nil
}
func (r *fakeRepo) SkipInvoicing(_ context.Context, ids []string) error {
	r.skipped = append(r.skipped, ids...)
	return nil
}
func (r *fakeRepo) IssueInvoice(_ context.Context, in NewInvoice) (*Invoice, error) {
	if r.monthTaken {
		return nil, nil
	}
	r.issued = append(r.issued, in)
	ht, vat := splitVAT(in.AmountTTC)
	inv := &Invoice{
		ID: int64(len(r.invoices) + 1), Number: fmt.Sprintf("TEST-2026-%06d", len(r.invoices)+1), Series: in.Series,
		MerchantID: in.MerchantID, PeriodMonth: in.PeriodMonth, AmountTTC: in.AmountTTC, AmountHT: ht, AmountVAT: vat,
		PayoutCount: len(in.PayoutIDs), PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd, IssuedAt: in.IssuedAt,
	}
	r.invoices = append(r.invoices, inv)
	return inv, nil
}
func (r *fakeRepo) InvoiceLines(context.Context, int64) ([]Line, error) { return r.invoiceLines, nil }
func (r *fakeRepo) UnsentInvoices(context.Context, int) ([]Invoice, error) {
	var out []Invoice
	for _, inv := range r.invoices {
		if _, sent := r.invoiceSent[inv.ID]; !sent {
			out = append(out, *inv)
		}
	}
	return out, nil
}
func (r *fakeRepo) RecordInvoiceFailure(_ context.Context, id int64, reason string, _ bool) error {
	r.invoiceFails[id] = reason
	return nil
}
func (r *fakeRepo) MarkInvoiceSent(_ context.Context, id int64, sent InvoiceSent) error {
	r.invoiceSent[id] = sent
	return nil
}
func (r *fakeRepo) ListInvoicesForMerchant(context.Context, string, int) ([]Invoice, error) {
	return nil, nil
}
func (r *fakeRepo) GetInvoiceForMerchant(_ context.Context, merchantID, number string) (*Invoice, error) {
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.Number == number {
			return inv, nil
		}
	}
	return nil, nil
}

type fakeStripe struct{ txs []stripeclient.PayoutTransaction }

func (f fakeStripe) GetPayout(context.Context, string, string) (*stripeclient.PayoutInfo, error) {
	return nil, errors.New("non utilisé")
}
func (f fakeStripe) ListPaidPayouts(context.Context, string, time.Time) ([]stripeclient.PayoutInfo, error) {
	return nil, nil
}
func (f fakeStripe) ListPayoutTransactions(context.Context, string, string) ([]stripeclient.PayoutTransaction, error) {
	return f.txs, nil
}

type fakeMail struct {
	// relevés de virement
	to          string
	data        mailer.PayoutDocumentsData
	attachments []mailer.Attachment
	calls       int
	err         error
	// factures mensuelles
	invoiceTo          string
	invoiceData        mailer.CommissionInvoiceData
	invoiceAttachments []mailer.Attachment
	invoiceCalls       int
	invoiceErr         error
}

func (m *fakeMail) SendPayoutDocuments(to string, data mailer.PayoutDocumentsData, att []mailer.Attachment) error {
	m.calls++
	m.to, m.data, m.attachments = to, data, att
	return m.err
}
func (m *fakeMail) SendCommissionInvoice(to string, data mailer.CommissionInvoiceData, att []mailer.Attachment) error {
	m.invoiceCalls++
	m.invoiceTo, m.invoiceData, m.invoiceAttachments = to, data, att
	return m.invoiceErr
}

type fakeStore struct {
	uploaded []string
	signed   []string
	err      error
}

func (f *fakeStore) UploadPrivateFile(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	f.uploaded = append(f.uploaded, key)
	return key, f.err
}
func (f *fakeStore) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	f.signed = append(f.signed, key)
	return "https://r2.example/" + key + "?sig=1", nil
}

func newFixture(txs []stripeclient.PayoutTransaction, amount int64) (*Service, *fakeRepo, *fakeMail, Pending) {
	repo := &fakeRepo{
		merchant: &Merchant{ID: "212", Name: "Chez Camille", Address: "1 rue de la Gare, 57000 Metz", SIRET: "12345678900012", Email: "camille@example.com"},
		channels: map[string]Channel{"pi_online": ChannelScanNOrder, "pi_kiosk": ChannelKiosk},
		done:     map[string]Done{}, failures: map[string]string{},
		listed: map[string][]Listed{}, lines: map[string][]Line{},
		invoiceSent: map[int64]InvoiceSent{}, invoiceFails: map[int64]string{},
	}
	mail := &fakeMail{}
	svc := NewService(repo, fakeStripe{txs: txs}, nil, mail, nil)
	svc.now = func() time.Time { return time.Date(2026, 11, 5, 10, 0, 0, 0, parisLocation) }
	p := Pending{Ref: Ref{PayoutID: "po_1", AccountID: "acct_1", Amount: amount, Currency: "eur", ArrivalDate: day}}
	repo.pending = []Pending{p}
	return svc, repo, mail, p
}

// ---- Relevé par virement ----

func TestProcess_SendsStatementWithBackOfficeLink(t *testing.T) {
	txs := []stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day), sale("txn_2", "pi_kiosk", day)}
	svc, repo, mail, _ := newFixture(txs, 2*949)

	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	if mail.to != testRecipient {
		t.Errorf("destinataire = %q, want %q (phase de test)", mail.to, testRecipient)
	}
	if len(mail.attachments) != 1 || !strings.HasPrefix(mail.attachments[0].Name, "releve-versement-2026-10-07") || !strings.HasPrefix(string(mail.attachments[0].Content), "%PDF") {
		t.Errorf("pièces jointes = %v : un seul PDF, le relevé", mail.attachments)
	}
	if want := docsmode.BackOfficeURL + docsmode.TabVirementsPath; mail.data.BackOfficeURL != want {
		t.Errorf("lien = %q, want %q", mail.data.BackOfficeURL, want)
	}
	if mail.data.TestNotice == "" {
		t.Error("bandeau de test absent")
	}

	// les totaux du virement sont gardés : c'est de quoi construire le récapitulatif mensuel
	d := repo.done["po_1"]
	if d.MerchantID != "212" || d.Recipient != testRecipient {
		t.Errorf("done = %+v", d)
	}
	want := Line{ScanNOrder: 1000, Kiosk: 1000, Commission: 40, StripeFees: 62}
	if d.Totals != want {
		t.Errorf("totaux = %+v, want %+v", d.Totals, want)
	}
}

func TestProcess_NotReconciledRetriesThenSendsWithDiscrepancy(t *testing.T) {
	svc, repo, mail, p := newFixture([]stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day)}, 5000)

	// premiers passages : Stripe n'a peut-être pas fini de rapprocher, rien ne part
	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Retry != 1 || res.Done != 0 || mail.calls != 0 {
		t.Fatalf("ProcessPending = %+v, %v, mails = %d", res, err, mail.calls)
	}

	// passé reconcileRetries essais, le relevé part quand même, écart signalé
	p.Attempts = reconcileRetries
	repo.pending = []Pending{p}
	if res, _ := svc.ProcessPending(context.Background()); res.Done != 1 {
		t.Fatalf("après %d essais le payout doit partir : %+v", reconcileRetries, res)
	}
	if repo.done["po_1"].Totals.Discrepancy != 5000-949 {
		t.Errorf("écart gardé = %d", repo.done["po_1"].Totals.Discrepancy)
	}
}

func TestProcess_MailFailureRetries(t *testing.T) {
	svc, repo, mail, _ := newFixture([]stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day)}, 949)
	mail.err = errors.New("brevo indisponible")

	res, _ := svc.ProcessPending(context.Background())
	if res.Retry != 1 || repo.failures["po_1"] == "" {
		t.Fatalf("res = %+v, failures = %v", res, repo.failures)
	}
	if _, done := repo.done["po_1"]; done {
		t.Error("le payout ne doit pas être clos après un échec d'envoi")
	}
}

func TestProcess_UnknownMerchantStillSent(t *testing.T) {
	svc, repo, mail, _ := newFixture([]stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day)}, 949)
	repo.merchant = nil

	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	if mail.to != testRecipient || mail.data.MerchantName != "acct_1" {
		t.Errorf("destinataire %q, restaurateur %q : un compte sans restaurateur doit être identifié par son compte Stripe", mail.to, mail.data.MerchantName)
	}
}

// ---- Facture et récapitulatif mensuels ----

func monthlyFixture() (*Service, *fakeRepo, *fakeMail) {
	svc, repo, mail, _ := newFixture(nil, 0)
	repo.candidates = []string{"212"}
	repo.lines["212"] = []Line{
		{PayoutID: "po_a", ArrivalDate: time.Date(2026, 10, 2, 0, 0, 0, 0, parisLocation), ScanNOrder: 10000, Commission: 200, StripeFees: 310, Net: 9490},
		{PayoutID: "po_b", ArrivalDate: time.Date(2026, 10, 9, 0, 0, 0, 0, parisLocation), Kiosk: 5000, Commission: 100, StripeFees: 150, Net: 4750},
		{PayoutID: "po_c", ArrivalDate: time.Date(2026, 10, 30, 0, 0, 0, 0, parisLocation), Kiosk: 2000, Refunds: -500, Commission: 30, StripeFees: 60, Net: 1410},
	}
	repo.invoiceLines = repo.lines["212"]
	return svc, repo, mail
}

func TestRunMonthlyInvoices_IssuesOneInvoiceForAllThePayoutsOfTheMonth(t *testing.T) {
	svc, repo, mail := monthlyFixture()

	res, err := svc.RunMonthlyInvoices(context.Background())
	if err != nil || res.Issued != 1 || res.Sent != 1 {
		t.Fatalf("RunMonthlyInvoices = %+v, %v", res, err)
	}

	in := repo.issued[0]
	if in.AmountTTC != 330 || len(in.PayoutIDs) != 3 || in.Series != seriesTest || in.MerchantID != "212" {
		t.Errorf("facture émise = %+v, want 330 centimes TTC pour 3 virements en série TEST", in)
	}
	// novembre → on facture octobre, virements arrivés avant le 1er novembre
	if got := in.PeriodMonth.In(parisLocation); got.Month() != time.October || got.Day() != 1 {
		t.Errorf("mois facturé = %v, want le 1er octobre", got)
	}
	if !in.PeriodStart.Equal(repo.lines["212"][0].ArrivalDate) || !in.PeriodEnd.Equal(repo.lines["212"][2].ArrivalDate) {
		t.Errorf("période = %v → %v", in.PeriodStart, in.PeriodEnd)
	}

	if mail.invoiceTo != testRecipient || mail.invoiceData.Month != "octobre 2026" || mail.invoiceData.Amount != "3,30 €" || mail.invoiceData.PayoutCount != 3 {
		t.Errorf("mail = to %q, %+v", mail.invoiceTo, mail.invoiceData)
	}
	if want := docsmode.BackOfficeURL + docsmode.TabFacturesPath; mail.invoiceData.BackOfficeURL != want {
		t.Errorf("lien = %q, want %q", mail.invoiceData.BackOfficeURL, want)
	}
	if len(mail.invoiceAttachments) != 2 ||
		mail.invoiceAttachments[0].Name != "facture-TEST-2026-000001.pdf" ||
		mail.invoiceAttachments[1].Name != "recapitulatif-commissions-2026-10.pdf" {
		t.Errorf("pièces jointes = %v : la facture puis le récapitulatif", mail.invoiceAttachments)
	}
	for _, a := range mail.invoiceAttachments {
		if !strings.HasPrefix(string(a.Content), "%PDF") {
			t.Errorf("%s n'est pas un PDF", a.Name)
		}
	}
	if _, ok := repo.invoiceSent[1]; !ok {
		t.Error("la facture doit être marquée envoyée")
	}
}

func TestRunMonthlyInvoices_WaitsUntilTheThird(t *testing.T) {
	svc, repo, mail := monthlyFixture()
	svc.now = func() time.Time { return time.Date(2026, 11, 2, 10, 0, 0, 0, parisLocation) }

	res, err := svc.RunMonthlyInvoices(context.Background())
	if err != nil || res.Issued != 0 || len(repo.issued) != 0 || mail.invoiceCalls != 0 {
		t.Fatalf("avant le 3 rien ne doit être émis : %+v, %v, mails = %d", res, err, mail.invoiceCalls)
	}
}

func TestRunMonthlyInvoices_FailedMailIsRetriedWithoutIssuingAgain(t *testing.T) {
	svc, repo, mail := monthlyFixture()
	mail.invoiceErr = errors.New("brevo indisponible")

	res, _ := svc.RunMonthlyInvoices(context.Background())
	if res.Issued != 1 || res.Sent != 0 || res.Retry != 1 || repo.invoiceFails[1] == "" {
		t.Fatalf("premier passage = %+v, échecs = %v", res, repo.invoiceFails)
	}

	// l'établissement n'est plus candidat (facture émise) ; la facture reste à envoyer
	repo.candidates = nil
	mail.invoiceErr = nil
	res, _ = svc.RunMonthlyInvoices(context.Background())
	if res.Issued != 0 || res.Sent != 1 || len(repo.issued) != 1 {
		t.Fatalf("second passage = %+v, factures émises = %d : la même facture doit partir, sans être réémise", res, len(repo.issued))
	}
}

func TestRunMonthlyInvoices_NothingToInvoice(t *testing.T) {
	for name, commissions := range map[string][]int64{"nulle": {0, 0}, "négative": {-20, 10}} {
		svc, repo, mail := monthlyFixture()
		repo.lines["212"] = []Line{
			{PayoutID: "po_a", ArrivalDate: day, Commission: commissions[0]},
			{PayoutID: "po_b", ArrivalDate: day, Commission: commissions[1]},
		}

		res, err := svc.RunMonthlyInvoices(context.Background())
		if err != nil || res.Issued != 0 || mail.invoiceCalls != 0 {
			t.Errorf("commission %s : %+v, %v, mails = %d", name, res, err, mail.invoiceCalls)
		}
		if len(repo.skipped) != 2 {
			t.Errorf("commission %s : virements sortis de la facturation = %v, want les 2", name, repo.skipped)
		}
	}
}

func TestRunMonthlyInvoices_MonthAlreadyInvoiced(t *testing.T) {
	svc, repo, mail := monthlyFixture()
	repo.monthTaken = true

	res, err := svc.RunMonthlyInvoices(context.Background())
	if err != nil || res.Issued != 0 || mail.invoiceCalls != 0 {
		t.Errorf("%+v, %v, mails = %d : une facture existe déjà pour ce mois", res, err, mail.invoiceCalls)
	}
}

// ---- Liens de téléchargement ----

func TestDownloadLinks(t *testing.T) {
	svc, repo, _, _ := newFixture(nil, 0)
	store := &fakeStore{}
	svc.store = store
	repo.listed = map[string][]Listed{"212": {{PayoutID: "po_1", StatementKey: "payouts/212/po_1/releve-versement-2026-10-07.pdf"}, {PayoutID: "po_nopdf"}}}
	repo.invoices = []*Invoice{{ID: 1, Number: "TEST-2026-000001", MerchantID: "212",
		InvoiceKey: "payouts/212/invoices/TEST-2026-000001/facture-TEST-2026-000001.pdf"}}

	link, err := svc.DownloadLink(context.Background(), "212", "po_1")
	if err != nil || link.Filename != "releve-versement-2026-10-07.pdf" || link.Kind != KindStatement || !strings.Contains(link.DownloadURL, "po_1") {
		t.Fatalf("lien du relevé = %+v, %v", link, err)
	}
	link, err = svc.InvoiceLink(context.Background(), "212", "TEST-2026-000001", KindInvoice)
	if err != nil || link.Filename != "facture-TEST-2026-000001.pdf" {
		t.Fatalf("lien de la facture = %+v, %v", link, err)
	}

	for name, call := range map[string]func() error{
		"relevé non archivé":    func() error { _, err := svc.DownloadLink(context.Background(), "212", "po_nopdf"); return err },
		"relevé inconnu":        func() error { _, err := svc.DownloadLink(context.Background(), "212", "po_x"); return err },
		"relevé d'un autre":     func() error { _, err := svc.DownloadLink(context.Background(), "999", "po_1"); return err },
		"récapitulatif absent":  func() error { _, err := svc.InvoiceLink(context.Background(), "212", "TEST-2026-000001", KindRecap); return err },
		"facture d'un autre":    func() error { _, err := svc.InvoiceLink(context.Background(), "999", "TEST-2026-000001", KindInvoice); return err },
		"facture inconnue":      func() error { _, err := svc.InvoiceLink(context.Background(), "212", "TEST-2026-000099", KindInvoice); return err },
	} {
		if err := call(); !errors.Is(err, ErrDocumentUnavailable) {
			t.Errorf("%s : err = %v, want ErrDocumentUnavailable", name, err)
		}
	}
	if len(store.signed) != 2 {
		t.Errorf("liens signés = %v, want 2 (aucun lien pour un refus)", store.signed)
	}

	svc.store = nil
	if _, err := svc.DownloadLink(context.Background(), "212", "po_1"); !errors.Is(err, ErrDocumentUnavailable) {
		t.Errorf("sans R2 : err = %v, want ErrDocumentUnavailable", err)
	}
}

// TestWritePDFsForInspection écrit les PDF dans le dossier PAYOUT_PDF_DIR pour
// les relire à l'œil ; ignoré sans cette variable.
func TestWritePDFsForInspection(t *testing.T) {
	dir := os.Getenv("PAYOUT_PDF_DIR")
	if dir == "" {
		t.Skip("PAYOUT_PDF_DIR non défini")
	}

	txs := []stripeclient.PayoutTransaction{
		sale("txn_1", "pi_online", day.AddDate(0, 0, -25)), sale("txn_2", "pi_online", day.AddDate(0, 0, -12)),
		sale("txn_3", "pi_kiosk", day.AddDate(0, 0, -4)),
		{ID: "txn_r", Type: "refund", ReportingCategory: "refund", Amount: -300, CommissionFee: -6, Net: -294, Created: day.AddDate(0, 0, -2)},
	}
	svc, _, mail, _ := newFixture(txs, 3*949-294)
	if res, err := svc.ProcessPending(context.Background()); err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	svc2, _, mail2 := monthlyFixture()
	if res, err := svc2.RunMonthlyInvoices(context.Background()); err != nil || res.Sent != 1 {
		t.Fatalf("RunMonthlyInvoices = %+v, %v", res, err)
	}

	for _, a := range append(append([]mailer.Attachment{}, mail.attachments...), mail2.invoiceAttachments...) {
		if err := os.WriteFile(filepath.Join(dir, a.Name), a.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

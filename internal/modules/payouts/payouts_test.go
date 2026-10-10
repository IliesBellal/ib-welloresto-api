package payouts

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// ---- Flux complet avec des faux ----

type fakeRepo struct {
	pending  []Pending
	merchant *Merchant
	channels map[string]Channel
	invoice  *Invoice
	created  int
	done     map[string]Done
	failures map[string]string
	listed   map[string][]Listed
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
func (r *fakeRepo) ConnectedAccounts(context.Context) ([]string, error) { return nil, nil }
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
func (r *fakeRepo) InvoiceForPayout(context.Context, string) (*Invoice, error) {
	return r.invoice, nil
}
func (r *fakeRepo) CreateInvoice(_ context.Context, in NewInvoice) (*Invoice, error) {
	r.created++
	ht, vat := splitVAT(in.AmountTTC)
	r.invoice = &Invoice{Number: "TEST-2026-000001", Series: in.Series, MerchantID: in.MerchantID, PayoutID: in.PayoutID,
		AmountTTC: in.AmountTTC, AmountHT: ht, AmountVAT: vat, PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd, IssuedAt: in.IssuedAt}
	return r.invoice, nil
}

type fakeStripe struct {
	txs []stripeclient.PayoutTransaction
}

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
	to          string
	data        mailer.PayoutDocumentsData
	attachments []mailer.Attachment
	calls       int
	err         error
}

func (m *fakeMail) SendPayoutDocuments(to string, data mailer.PayoutDocumentsData, att []mailer.Attachment) error {
	m.calls++
	m.to, m.data, m.attachments = to, data, att
	return m.err
}

func newFixture(txs []stripeclient.PayoutTransaction, amount int64) (*Service, *fakeRepo, *fakeMail, Pending) {
	repo := &fakeRepo{
		merchant: &Merchant{ID: "212", Name: "Chez Camille", Address: "1 rue de la Gare, 57000 Metz", SIRET: "12345678900012", Email: "camille@example.com"},
		channels: map[string]Channel{"pi_online": ChannelScanNOrder, "pi_kiosk": ChannelKiosk},
		done:     map[string]Done{}, failures: map[string]string{},
	}
	mail := &fakeMail{}
	svc := NewService(repo, fakeStripe{txs: txs}, nil, mail, nil)
	svc.now = func() time.Time { return day }
	p := Pending{Ref: Ref{PayoutID: "po_1", AccountID: "acct_1", Amount: amount, Currency: "eur", ArrivalDate: day}}
	repo.pending = []Pending{p}
	return svc, repo, mail, p
}

func TestProcess_SendsStatementAndInvoiceToTestRecipient(t *testing.T) {
	txs := []stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day), sale("txn_2", "pi_kiosk", day)}
	svc, repo, mail, _ := newFixture(txs, 2*949)

	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	if mail.to != testRecipient {
		t.Errorf("destinataire = %q, want %q (phase de test)", mail.to, testRecipient)
	}
	if len(mail.attachments) != 2 || !strings.HasPrefix(mail.attachments[0].Name, "releve-versement-2026-10-07") || !strings.HasPrefix(mail.attachments[1].Name, "facture-TEST-2026-000001") {
		t.Errorf("pièces jointes = %v", mail.attachments)
	}
	for _, a := range mail.attachments {
		if !strings.HasPrefix(string(a.Content), "%PDF") {
			t.Errorf("%s n'est pas un PDF", a.Name)
		}
	}
	if repo.invoice == nil || repo.invoice.AmountTTC != 40 || repo.invoice.Series != seriesTest {
		t.Errorf("facture = %+v, want 40 centimes TTC en série TEST", repo.invoice)
	}
	if mail.data.InvoiceNumber != "TEST-2026-000001" || mail.data.TestNotice == "" {
		t.Errorf("mail = %+v", mail.data)
	}
	if d, ok := repo.done["po_1"]; !ok || d.MerchantID != "212" || d.Recipient != testRecipient {
		t.Errorf("done = %+v", d)
	}
}

func TestProcess_SecondRunReusesInvoice(t *testing.T) {
	txs := []stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day)}
	svc, repo, mail, _ := newFixture(txs, 949)

	// premier passage : le mail échoue, la facture est quand même émise
	mail.err = errors.New("brevo indisponible")
	if _, err := svc.ProcessPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, done := repo.done["po_1"]; done || repo.failures["po_1"] == "" {
		t.Fatalf("le payout ne doit pas être clos après un échec d'envoi (done=%v, failures=%v)", repo.done, repo.failures)
	}

	// second passage : mail OK, la facture n'est pas réémise
	mail.err = nil
	if res, _ := svc.ProcessPending(context.Background()); res.Done != 1 {
		t.Fatalf("second passage = %+v", res)
	}
	if repo.created != 1 {
		t.Errorf("factures émises = %d, want 1", repo.created)
	}
}

func TestProcess_NotReconciledRetriesThenSendsWithDiscrepancy(t *testing.T) {
	svc, repo, mail, p := newFixture([]stripeclient.PayoutTransaction{sale("txn_1", "pi_online", day)}, 5000)

	// premiers passages : Stripe n'a peut-être pas fini de rapprocher, rien ne part
	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Retry != 1 || res.Done != 0 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	if mail.calls != 0 || repo.created != 0 {
		t.Errorf("rien ne doit partir tant qu'on espère le rapprochement (mails=%d, factures=%d)", mail.calls, repo.created)
	}

	// passé reconcileRetries essais, le relevé part quand même, écart signalé
	p.Attempts = reconcileRetries
	repo.pending = []Pending{p}
	if res, _ := svc.ProcessPending(context.Background()); res.Done != 1 {
		t.Fatalf("après %d essais le payout doit partir : %+v", reconcileRetries, res)
	}
	if mail.calls != 1 || !strings.HasPrefix(string(mail.attachments[0].Content), "%PDF") {
		t.Errorf("mails = %d, pièces jointes = %v", mail.calls, mail.attachments)
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

type fakeStore struct {
	signed []string
	err    error
}

func (f *fakeStore) UploadPrivateFile(context.Context, string, io.Reader, string) (string, error) {
	return "", nil
}
func (f *fakeStore) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	f.signed = append(f.signed, key)
	return "https://r2.example/" + key + "?sig=1", f.err
}

func TestDownloadLink(t *testing.T) {
	svc, repo, _, _ := newFixture(nil, 0)
	store := &fakeStore{}
	svc.store = store
	repo.listed = map[string][]Listed{"212": {{PayoutID: "po_1", StatementKey: "payouts/212/po_1/releve-versement-2026-10-07.pdf"}}}

	link, err := svc.DownloadLink(context.Background(), "212", "po_1", KindStatement)
	if err != nil || link.Filename != "releve-versement-2026-10-07.pdf" || !strings.Contains(link.DownloadURL, "po_1") {
		t.Fatalf("lien = %+v, %v", link, err)
	}

	// pas de facture archivée pour ce payout
	if _, err := svc.DownloadLink(context.Background(), "212", "po_1", KindInvoice); !errors.Is(err, ErrDocumentUnavailable) {
		t.Errorf("facture absente : err = %v, want ErrDocumentUnavailable", err)
	}
	// un autre établissement ne voit pas ce payout
	if _, err := svc.DownloadLink(context.Background(), "999", "po_1", KindStatement); !errors.Is(err, ErrDocumentUnavailable) {
		t.Errorf("autre établissement : err = %v, want ErrDocumentUnavailable", err)
	}
	if len(store.signed) != 1 {
		t.Errorf("liens signés = %v, want un seul (aucun lien pour un refus)", store.signed)
	}

	// sans archivage R2, aucun document n'est téléchargeable
	svc.store = nil
	if _, err := svc.DownloadLink(context.Background(), "212", "po_1", KindStatement); !errors.Is(err, ErrDocumentUnavailable) {
		t.Errorf("sans R2 : err = %v, want ErrDocumentUnavailable", err)
	}
}

func TestProcess_NegativeCommissionSendsStatementOnly(t *testing.T) {
	refund := stripeclient.PayoutTransaction{ID: "txn_r", Type: "refund", ReportingCategory: "refund", Amount: -1000, CommissionFee: -20, StripeFee: 0, Net: -980, Created: day}
	svc, repo, mail, _ := newFixture([]stripeclient.PayoutTransaction{refund}, -980)

	res, _ := svc.ProcessPending(context.Background())
	if res.Done != 1 {
		t.Fatalf("ProcessPending = %+v", res)
	}
	if len(mail.attachments) != 1 || repo.created != 0 {
		t.Errorf("relevé seul attendu (pièces jointes=%d, factures=%d)", len(mail.attachments), repo.created)
	}
}

// TestWritePDFsForInspection écrit les deux PDF dans le dossier PAYOUT_PDF_DIR
// pour les relire à l'œil ; ignoré sans cette variable.
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
	svc, repo, mail, _ := newFixture(txs, 3*949-294)
	if res, err := svc.ProcessPending(context.Background()); err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	_ = repo
	for _, a := range mail.attachments {
		if err := os.WriteFile(filepath.Join(dir, a.Name), a.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

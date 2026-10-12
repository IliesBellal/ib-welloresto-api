package subscriptioninvoices

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/docsmode"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

var paidAt = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

type fakeRepo struct {
	pending  []Pending
	inserted []Ref
	merchant *Merchant
	done     map[string]Done
	failures map[string]string
	listed   map[string][]Listed
	subs     map[string]string
}

func (r *fakeRepo) InsertPending(_ context.Context, ref Ref) error {
	r.inserted = append(r.inserted, ref)
	return nil
}
func (r *fakeRepo) ListPending(context.Context, int) ([]Pending, error) { return r.pending, nil }
func (r *fakeRepo) RecordFailure(_ context.Context, id, reason string, _ bool) error {
	r.failures[id] = reason
	return nil
}
func (r *fakeRepo) MarkDone(_ context.Context, id string, d Done) error { r.done[id] = d; return nil }
func (r *fakeRepo) Merchant(context.Context, string) (*Merchant, error) { return r.merchant, nil }
func (r *fakeRepo) MerchantBySubscription(_ context.Context, sub string) (string, error) {
	return r.subs[sub], nil
}
func (r *fakeRepo) ListForMerchant(_ context.Context, id string, _ int) ([]Listed, error) {
	return r.listed[id], nil
}
func (r *fakeRepo) GetForMerchant(_ context.Context, id, invoiceID string) (*Listed, error) {
	for _, l := range r.listed[id] {
		if l.InvoiceID == invoiceID {
			return &l, nil
		}
	}
	return nil, nil
}

type fakeStripe struct {
	info     *stripeclient.InvoiceInfo
	invoices []stripeclient.InvoiceInfo
	pdf      []byte
	pdfErr   error
}

func (f fakeStripe) GetInvoice(context.Context, string) (*stripeclient.InvoiceInfo, error) {
	return f.info, nil
}
func (f fakeStripe) ListPaidInvoices(context.Context, time.Time) ([]stripeclient.InvoiceInfo, error) {
	return f.invoices, nil
}
func (f fakeStripe) FetchInvoicePDF(context.Context, string) ([]byte, error) { return f.pdf, f.pdfErr }

type fakeMail struct {
	to         string
	data       mailer.SubscriptionInvoiceData
	attachment mailer.Attachment
	calls      int
	err        error
}

func (m *fakeMail) SendSubscriptionInvoice(to string, data mailer.SubscriptionInvoiceData, a mailer.Attachment) error {
	m.calls++
	m.to, m.data, m.attachment = to, data, a
	return m.err
}

type fakeStore struct {
	uploaded []string
	err      error
}

func (s *fakeStore) UploadPrivateFile(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	s.uploaded = append(s.uploaded, key)
	return key, s.err
}
func (s *fakeStore) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://r2.example/" + key + "?sig=1", nil
}

func fixture() (*Service, *fakeRepo, *fakeMail, *fakeStore, Pending) {
	repo := &fakeRepo{
		merchant: &Merchant{ID: "212", Name: "Chez Camille", Email: "camille@example.com"},
		done:     map[string]Done{}, failures: map[string]string{},
		listed: map[string][]Listed{}, subs: map[string]string{"sub_1": "212"},
	}
	stripe := fakeStripe{
		info: &stripeclient.InvoiceInfo{ID: "in_1", Number: "E4CB-0019", PDFURL: "https://pay.stripe.com/pdf", HostedURL: "https://invoice.stripe.com/i/1"},
		pdf:  []byte("%PDF-1.7 facture"),
	}
	mail, store := &fakeMail{}, &fakeStore{}
	p := Pending{Ref: Ref{InvoiceID: "in_1", MerchantID: "212", Number: "E4CB-0019", AmountPaid: 7900, Currency: "eur",
		PeriodStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), PaidAt: paidAt}}
	repo.pending = []Pending{p}
	return NewService(repo, stripe, store, mail, nil), repo, mail, store, p
}

func TestProcess_SendsStripePDFToTestRecipient(t *testing.T) {
	svc, repo, mail, store, _ := fixture()

	res, err := svc.ProcessPending(context.Background())
	if err != nil || res.Done != 1 {
		t.Fatalf("ProcessPending = %+v, %v", res, err)
	}
	if mail.to != docsmode.TestRecipient {
		t.Errorf("destinataire = %q, want %q (phase de test)", mail.to, docsmode.TestRecipient)
	}
	if mail.attachment.Name != "facture-abonnement-E4CB-0019.pdf" || string(mail.attachment.Content) != "%PDF-1.7 facture" {
		t.Errorf("pièce jointe = %q (%d octets) : doit être le PDF Stripe tel quel", mail.attachment.Name, len(mail.attachment.Content))
	}
	if mail.data.Amount != "79,00 €" || mail.data.InvoiceNumber != "E4CB-0019" || mail.data.Period != "du 01/10/2026 au 31/10/2026" || mail.data.TestNotice == "" {
		t.Errorf("mail = %+v", mail.data)
	}
	if len(store.uploaded) != 1 || !strings.HasPrefix(store.uploaded[0], "subscription-invoices/212/in_1/") {
		t.Errorf("archivage = %v", store.uploaded)
	}
	if d := repo.done["in_1"]; d.Number != "E4CB-0019" || d.Recipient != docsmode.TestRecipient || d.PDFKey == "" {
		t.Errorf("done = %+v", d)
	}
}

func TestProcess_PDFNotReadyRetriesAndSendsNothing(t *testing.T) {
	svc, repo, mail, _, _ := fixture()
	svc.stripe = fakeStripe{info: &stripeclient.InvoiceInfo{ID: "in_1"}, pdfErr: errors.New("la facture n'a pas (encore) de PDF")}

	res, _ := svc.ProcessPending(context.Background())
	if res.Retry != 1 || res.Done != 0 || mail.calls != 0 {
		t.Fatalf("res = %+v, mails = %d", res, mail.calls)
	}
	if repo.failures["in_1"] == "" {
		t.Error("l'échec doit être enregistré pour le prochain passage")
	}
}

func TestProcess_ArchiveFailureStillSends(t *testing.T) {
	svc, repo, mail, store, _ := fixture()
	store.err = errors.New("R2 indisponible")

	res, _ := svc.ProcessPending(context.Background())
	if res.Done != 1 || mail.calls != 1 {
		t.Fatalf("res = %+v, mails = %d : l'archivage ne doit pas bloquer l'envoi", res, mail.calls)
	}
	if repo.done["in_1"].PDFKey != "" {
		t.Errorf("clé enregistrée malgré l'échec : %q", repo.done["in_1"].PDFKey)
	}
}

func TestProcess_UnknownMerchantStillSent(t *testing.T) {
	svc, repo, mail, _, _ := fixture()
	repo.merchant = nil

	res, _ := svc.ProcessPending(context.Background())
	if res.Done != 1 || mail.data.MerchantName != "établissement 212" {
		t.Errorf("res = %+v, restaurateur = %q", res, mail.data.MerchantName)
	}
}

func TestRecordPaidInvoice_SkipsZeroAndUnknown(t *testing.T) {
	svc, repo, _, _, _ := fixture()
	ctx := context.Background()

	_ = svc.RecordPaidInvoice(ctx, Ref{InvoiceID: "in_0", MerchantID: "212", AmountPaid: 0}) // essai gratuit
	_ = svc.RecordPaidInvoice(ctx, Ref{InvoiceID: "in_x", MerchantID: "", AmountPaid: 7900}) // abonnement inconnu
	_ = svc.RecordPaidInvoice(ctx, Ref{InvoiceID: "in_1", MerchantID: "212", AmountPaid: 7900})

	if len(repo.inserted) != 1 || repo.inserted[0].InvoiceID != "in_1" {
		t.Errorf("insérées = %+v, want la seule facture payée d'un établissement connu", repo.inserted)
	}
}

func TestEnqueueSince(t *testing.T) {
	svc, repo, _, _, _ := fixture()
	svc.stripe = fakeStripe{invoices: []stripeclient.InvoiceInfo{
		{ID: "in_ok", SubscriptionID: "sub_1", AmountPaid: 7900},
		{ID: "in_zero", SubscriptionID: "sub_1", AmountPaid: 0},
		{ID: "in_nosub", SubscriptionID: "", AmountPaid: 7900},
		{ID: "in_unknown", SubscriptionID: "sub_inconnu", AmountPaid: 7900},
	}}

	n, err := svc.EnqueueSince(context.Background(), paidAt)
	if err != nil || n != 1 || len(repo.inserted) != 1 || repo.inserted[0].InvoiceID != "in_ok" || repo.inserted[0].MerchantID != "212" {
		t.Errorf("n = %d, err = %v, insérées = %+v", n, err, repo.inserted)
	}
}

func TestDownloadLink(t *testing.T) {
	svc, repo, _, _, _ := fixture()
	repo.listed["212"] = []Listed{
		{InvoiceID: "in_1", PDFKey: "subscription-invoices/212/in_1/facture-abonnement-E4CB-0019.pdf"},
		{InvoiceID: "in_nopdf"},
	}

	link, err := svc.DownloadLink(context.Background(), "212", "in_1")
	if err != nil || link.Filename != "facture-abonnement-E4CB-0019.pdf" || !strings.Contains(link.DownloadURL, "in_1") {
		t.Fatalf("lien = %+v, %v", link, err)
	}
	for name, args := range map[string][2]string{"non archivée": {"212", "in_nopdf"}, "inconnue": {"212", "in_x"}, "autre établissement": {"999", "in_1"}} {
		if _, err := svc.DownloadLink(context.Background(), args[0], args[1]); !errors.Is(err, ErrDocumentUnavailable) {
			t.Errorf("%s : err = %v, want ErrDocumentUnavailable", name, err)
		}
	}
}

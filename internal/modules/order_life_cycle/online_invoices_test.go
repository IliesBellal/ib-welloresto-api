package order_life_cycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"welloresto-api/internal/infrastructure/mailer"

	"github.com/DATA-DOG/go-sqlmock"
)

// trackedMailer : mailer qui sait envoyer la facture en renvoyant le
// messageId Brevo (BrevoMailer.SendOrderInvoice).
type trackedMailer struct {
	fakeMailerService
	sent      []mailer.InvoiceEmailData
	sentTo    []string
	messageID string
	err       error
}

func (m *trackedMailer) SendOrderInvoice(to string, data mailer.InvoiceEmailData, attachment mailer.Attachment) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.sent = append(m.sent, data)
	m.sentTo = append(m.sentTo, to)
	return m.messageID, nil
}

type journalEntry struct{ messageID, domain, refID, to string }

type fakeInvoiceJournal struct {
	entries []journalEntry
	err     error
}

func (j *fakeInvoiceJournal) RecordOutboundMessageWithContext(_ context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error {
	if j.err != nil {
		return j.err
	}
	j.entries = append(j.entries, journalEntry{providerMessageID, domain, domainRefID, recipient})
	return nil
}

type fixedBrand struct{}

func (fixedBrand) Resolve(_ context.Context, _, name string) mailer.MerchantBrand {
	return mailer.NewMerchantBrand(name, "", "#8C1C2B", "")
}

// L'envoi depuis la caisse est inscrit au journal : la tâche automatique ne
// renverra pas la facture.
func TestSendInvoiceByEmail_JournalsTheSend(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mail := &trackedMailer{messageID: "<brevo-1>"}
	journal := &fakeInvoiceJournal{}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, nil)
	svc.mailerService = mail
	svc.SetOnlineInvoices(journal, fixedBrand{})

	mock.ExpectBegin()
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "client@example.fr"))
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "client@example.fr"))
	mock.ExpectExec("UPDATE orders").WithArgs("cus_1", testOrderID, testMerchantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	customerID := "cus_1"
	if _, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "client@example.fr", CustomerID: &customerID}); err != nil {
		t.Fatalf("SendInvoiceByEmail() error = %v", err)
	}
	if len(mail.sent) != 1 || mail.sent[0].Brand.Color != "#8C1C2B" || mail.sent[0].OrderNumber != "ORD-42" {
		t.Fatalf("facture envoyée = %+v, attendu aux couleurs de l'établissement avec le numéro de commande", mail.sent)
	}
	want := journalEntry{"<brevo-1>", OnlineInvoiceDomain, testOrderID, "client@example.fr"}
	if len(journal.entries) != 1 || journal.entries[0] != want {
		t.Fatalf("journal = %+v, want [%+v]", journal.entries, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendOnlineOrderInvoices(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mail := &trackedMailer{messageID: "<brevo-2>"}
	journal := &fakeInvoiceJournal{}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, nil)
	svc.mailerService = mail
	svc.SetOnlineInvoices(journal, fixedBrand{})

	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	mock.ExpectQuery("FROM orders o").
		WithArgs(now.Add(-onlineInvoiceWindow), "WELLO_RESTO", "WEBHOOK_STRIPE", OnlineInvoiceMinTTC, OnlineInvoiceDomain, onlineInvoiceBatch).
		WillReturnRows(sqlmock.NewRows([]string{"order_id", "merchant_id", "customer_id", "name", "email"}).
			AddRow(testOrderID, testMerchantID, "cus_1", "Jean Dupont", "client@example.fr"))

	res, err := svc.SendOnlineOrderInvoices(context.Background(), now)
	if err != nil {
		t.Fatalf("SendOnlineOrderInvoices() error = %v", err)
	}
	if res.Sent != 1 || res.Failed != 0 {
		t.Fatalf("résultat = %+v, want 1 envoyée", res)
	}
	if len(mail.sentTo) != 1 || mail.sentTo[0] != "client@example.fr" {
		t.Fatalf("destinataires = %v", mail.sentTo)
	}
	if len(journal.entries) != 1 || journal.entries[0].refID != testOrderID || journal.entries[0].domain != OnlineInvoiceDomain {
		t.Fatalf("journal = %+v", journal.entries)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// Un envoi en échec n'est pas inscrit : la commande sera retentée.
func TestSendOnlineOrderInvoices_FailureIsNotJournaled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mail := &trackedMailer{err: errors.New("brevo down")}
	journal := &fakeInvoiceJournal{}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, nil)
	svc.mailerService = mail
	svc.SetOnlineInvoices(journal, nil)

	mock.ExpectQuery("FROM orders o").
		WillReturnRows(sqlmock.NewRows([]string{"order_id", "merchant_id", "customer_id", "name", "email"}).
			AddRow(testOrderID, testMerchantID, "cus_1", "", "client@example.fr"))

	res, err := svc.SendOnlineOrderInvoices(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("SendOnlineOrderInvoices() error = %v", err)
	}
	if res.Sent != 0 || res.Failed != 1 || len(journal.entries) != 0 {
		t.Fatalf("résultat = %+v, journal = %+v : l'échec ne doit pas être inscrit", res, journal.entries)
	}
}

// Sans mailer capable de renvoyer le messageId, la tâche refuse de tourner
// plutôt que d'envoyer sans pouvoir éviter les doublons.
func TestSendOnlineOrderInvoices_RequiresTrackedMailer(t *testing.T) {
	svc := newTestService(nil, &fakeOrderFetcher{}, &fakeAuditService{}, &fakeMailerService{})
	svc.SetOnlineInvoices(&fakeInvoiceJournal{}, nil)
	if _, err := svc.SendOnlineOrderInvoices(context.Background(), time.Now()); err == nil {
		t.Fatal("SendOnlineOrderInvoices() sans mailer tracé : erreur attendue")
	}
}

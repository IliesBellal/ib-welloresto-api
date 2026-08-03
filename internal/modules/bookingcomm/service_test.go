package bookingcomm

import (
	"context"
	"net/http"
	"testing"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/modules/outbound"

	"github.com/DATA-DOG/go-sqlmock"
)

// mockMailer capture les appels SendAsync sans effectuer d'envoi réel.
type mockMailer struct {
	sendAsyncCalls int
	lastTemplate   string
	lastTo         string
	nextMessageID  string
}

func (m *mockMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	m.sendAsyncCalls++
	m.lastTemplate = templateName
	m.lastTo = to
}
func (m *mockMailer) SendAsyncWithMessageID(fromName, fromEmail, to, subject, templateName string, data interface{}, onSent func(messageID string)) {
	m.SendAsync(fromName, fromEmail, to, subject, templateName, data)
	if onSent != nil {
		onSent(m.nextMessageID)
	}
}
func (m *mockMailer) SendOrderConfirmationToCustomer(to string, data mailer.ScanNOrderConfirmationData) {
}
func (m *mockMailer) SendRefundNotification(s string, data mailer.RefundData) {}
func (m *mockMailer) SendPayoutPaidNotification(email string, name string, payout mailer.PayoutData) {
}
func (m *mockMailer) SendOTP(data mailer.MfaOTPData)                     {}
func (m *mockMailer) SendPasswordReset(data mailer.PasswordResetData)    {}
func (m *mockMailer) SendInvoiceEmailToCustomer(to, customerName string, pdfBytes []byte, fileName string) error {
	return nil
}
func (m *mockMailer) TriggerTestEmail(writer http.ResponseWriter, request *http.Request) {}

// mockSMS capture les appels SendSMSAsync sans effectuer d'envoi réel.
type mockSMS struct {
	sendSMSCalls  int
	lastPhone     string
	lastMessage   string
	nextMessageID string
}

func (m *mockSMS) SendSMSAsync(senderID, phoneNumber, message string) {
	m.sendSMSCalls++
	m.lastPhone = phoneNumber
	m.lastMessage = message
}
func (m *mockSMS) SendSMSAsyncWithMessageID(senderID, phoneNumber, message string, onSent func(messageID string)) {
	m.SendSMSAsync(senderID, phoneNumber, message)
	if onSent != nil {
		onSent(m.nextMessageID)
	}
}
func (m *mockSMS) SendOrderConfirmationSMS(senderID, phoneNumber string, data sms.OrderConfirmationSMSData) {
}
func (m *mockSMS) SendOTP(tel, otp string)                                          {}
func (m *mockSMS) TriggerTestSMS(writer http.ResponseWriter, request *http.Request) {}

type mockOutbound struct {
	calls []struct {
		channel           string
		provider          string
		providerMessageID string
		domain            string
		domainRefID       string
		recipient         string
	}
}

func (m *mockOutbound) RecordOutboundMessageWithContext(ctx context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error {
	m.calls = append(m.calls, struct {
		channel           string
		provider          string
		providerMessageID string
		domain            string
		domainRefID       string
		recipient         string
	}{
		channel:           channel,
		provider:          provider,
		providerMessageID: providerMessageID,
		domain:            domain,
		domainRefID:       domainRefID,
		recipient:         recipient,
	})
	return nil
}

func baseMessage(smsEnabled bool) BookingMessage {
	return BookingMessage{
		BookingID:     "book-123",
		MerchantSlug:  "le-bistrot",
		MerchantName:  "Le Bistrot",
		CustomerName:  "Jean Dupont",
		CustomerEmail: "jean@example.com",
		CustomerPhone: "+33612345678",
		BookingNumber: "AB12CD",
		DateLabel:     "vendredi 12 juillet 2026",
		TimeLabel:     "20:00",
		PartySize:     4,
		SMSEnabled:    smsEnabled,
	}
}

func TestSendConfirmation_SMSEnabledTrue_SendsEmailAndSMS(t *testing.T) {
	mail := &mockMailer{}
	txt := &mockSMS{}
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil, nil)

	svc.SendConfirmation(context.Background(), baseMessage(true))

	if mail.sendAsyncCalls != 1 {
		t.Fatalf("expected 1 email sent, got %d", mail.sendAsyncCalls)
	}
	if mail.lastTemplate != "booking_confirmation.html" {
		t.Fatalf("unexpected template: %s", mail.lastTemplate)
	}
	if txt.sendSMSCalls != 1 {
		t.Fatalf("expected 1 SMS sent when sms_enabled=true, got %d", txt.sendSMSCalls)
	}
}

func TestSendConfirmation_SMSEnabledFalse_EmailOnly(t *testing.T) {
	mail := &mockMailer{}
	txt := &mockSMS{}
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil, nil)

	svc.SendConfirmation(context.Background(), baseMessage(false))

	if mail.sendAsyncCalls != 1 {
		t.Fatalf("expected 1 email sent regardless of sms_enabled, got %d", mail.sendAsyncCalls)
	}
	if txt.sendSMSCalls != 0 {
		t.Fatalf("expected no SMS sent when sms_enabled=false, got %d", txt.sendSMSCalls)
	}
}

func TestSendConfirmation_NoEmailAddress_SkipsEmail(t *testing.T) {
	mail := &mockMailer{}
	txt := &mockSMS{}
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil, nil)

	msg := baseMessage(true)
	msg.CustomerEmail = ""
	svc.SendConfirmation(context.Background(), msg)

	if mail.sendAsyncCalls != 0 {
		t.Fatalf("expected no email sent without an address, got %d", mail.sendAsyncCalls)
	}
	if txt.sendSMSCalls != 1 {
		t.Fatalf("expected SMS still sent independently of email, got %d", txt.sendSMSCalls)
	}
}

func TestSendCancellation_SMSEnabledTrue_SendsEmailAndSMS(t *testing.T) {
	mail := &mockMailer{}
	txt := &mockSMS{}
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil, nil)

	svc.SendCancellation(context.Background(), baseMessage(true))

	if mail.sendAsyncCalls != 1 || mail.lastTemplate != "booking_cancellation.html" {
		t.Fatalf("unexpected mailer state: calls=%d template=%s", mail.sendAsyncCalls, mail.lastTemplate)
	}
	if txt.sendSMSCalls != 1 {
		t.Fatalf("expected 1 SMS sent, got %d", txt.sendSMSCalls)
	}
}

func TestSendWaitlistAvailable_RespectsSMSEnabled(t *testing.T) {
	mail := &mockMailer{}
	txt := &mockSMS{}
	svc := New(mail, txt, "", nil, nil)

	msg := WaitlistMessage{
		MerchantName:  "Le Bistrot",
		CustomerName:  "Jean",
		CustomerEmail: "jean@example.com",
		CustomerPhone: "+33612345678",
		PartySize:     2,
		ExpiryMinutes: 15,
		SMSEnabled:    false,
	}
	svc.SendWaitlistAvailable(context.Background(), msg)

	if mail.sendAsyncCalls != 1 || mail.lastTemplate != "waitlist_available.html" {
		t.Fatalf("unexpected mailer state: calls=%d template=%s", mail.sendAsyncCalls, mail.lastTemplate)
	}
	if txt.sendSMSCalls != 0 {
		t.Fatalf("expected no SMS when sms_enabled=false, got %d", txt.sendSMSCalls)
	}

	msg.SMSEnabled = true
	svc.SendWaitlistAvailable(context.Background(), msg)
	if txt.sendSMSCalls != 1 {
		t.Fatalf("expected 1 SMS after enabling sms_enabled, got %d", txt.sendSMSCalls)
	}
}

func TestManagementLink_BuiltFromBaseURL(t *testing.T) {
	mail := &mockMailer{}
	svc := New(mail, nil, "https://rsv.welloresto.fr/", nil, nil)

	msg := baseMessage(false)
	data := svc.emailData(msg)
	want := "https://rsv.welloresto.fr/restaurant/le-bistrot/booking/AB12CD"
	if data.ManagementLink != want {
		t.Fatalf("ManagementLink = %q, want %q", data.ManagementLink, want)
	}
}

func TestManagementLink_EmptyWithoutBaseURL(t *testing.T) {
	mail := &mockMailer{}
	svc := New(mail, nil, "", nil, nil)

	data := svc.emailData(baseMessage(false))
	if data.ManagementLink != "" {
		t.Fatalf("expected empty ManagementLink without a configured base URL, got %q", data.ManagementLink)
	}
}

func TestSendConfirmation_RecordsOutboundWithBookingDomain(t *testing.T) {
	mail := &mockMailer{nextMessageID: "mail-msg-1"}
	txt := &mockSMS{nextMessageID: "sms-msg-1"}
	out := &mockOutbound{}
	svc := New(mail, txt, "https://rsv.welloresto.fr", out, nil)

	svc.SendConfirmation(context.Background(), baseMessage(true))

	if len(out.calls) != 2 {
		t.Fatalf("expected 2 outbound records (email + sms), got %d", len(out.calls))
	}
	if out.calls[0].domain != "booking" || out.calls[0].domainRefID != "book-123" {
		t.Fatalf("unexpected outbound domain payload: %+v", out.calls[0])
	}
}

func TestSendConfirmation_PersistsOutboundMessageWithBookingDomain(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mail := &mockMailer{nextMessageID: "mail-msg-1"}
	txt := &mockSMS{}
	outboundService := outbound.NewService(outbound.NewRepository(db), nil)
	svc := New(mail, txt, "https://rsv.welloresto.fr", outboundService, nil)

	mock.ExpectExec("INSERT INTO outbound_messages").
		WithArgs(
			sqlmock.AnyArg(),
			"email",
			"brevo",
			"mail-msg-1",
			"booking",
			"book-123",
			"jean@example.com",
			"sent",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	svc.SendConfirmation(context.Background(), baseMessage(false))

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
	if mail.sendAsyncCalls != 1 {
		t.Fatalf("expected email send to still happen, got %d", mail.sendAsyncCalls)
	}
}

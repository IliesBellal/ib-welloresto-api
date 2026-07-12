package bookingcomm

import (
	"context"
	"net/http"
	"testing"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
)

// mockMailer capture les appels SendAsync sans effectuer d'envoi réel.
type mockMailer struct {
	sendAsyncCalls int
	lastTemplate   string
	lastTo         string
}

func (m *mockMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	m.sendAsyncCalls++
	m.lastTemplate = templateName
	m.lastTo = to
}
func (m *mockMailer) SendOrderConfirmationToCustomer(to string, data mailer.ScanNOrderConfirmationData) {
}
func (m *mockMailer) SendRefundNotification(s string, data mailer.RefundData)          {}
func (m *mockMailer) SendPayoutPaidNotification(email string, name string, payout mailer.PayoutData) {
}
func (m *mockMailer) SendOTP(data mailer.MfaOTPData) {}
func (m *mockMailer) SendInvoiceEmailToCustomer(to, customerName string, pdfBytes []byte, fileName string) error {
	return nil
}
func (m *mockMailer) TriggerTestEmail(writer http.ResponseWriter, request *http.Request) {}

// mockSMS capture les appels SendSMSAsync sans effectuer d'envoi réel.
type mockSMS struct {
	sendSMSCalls int
	lastPhone    string
	lastMessage  string
}

func (m *mockSMS) SendSMSAsync(senderID, phoneNumber, message string) {
	m.sendSMSCalls++
	m.lastPhone = phoneNumber
	m.lastMessage = message
}
func (m *mockSMS) SendOrderConfirmationSMS(senderID, phoneNumber string, data sms.OrderConfirmationSMSData) {
}
func (m *mockSMS) SendOTP(tel, otp string)                                             {}
func (m *mockSMS) TriggerTestSMS(writer http.ResponseWriter, request *http.Request)     {}

func baseMessage(smsEnabled bool) BookingMessage {
	return BookingMessage{
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
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil)

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
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil)

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
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil)

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
	svc := New(mail, txt, "https://rsv.welloresto.fr", nil)

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
	svc := New(mail, txt, "", nil)

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
	svc := New(mail, nil, "https://rsv.welloresto.fr/", nil)

	msg := baseMessage(false)
	data := svc.emailData(msg)
	want := "https://rsv.welloresto.fr/le-bistrot/booking/AB12CD"
	if data.ManagementLink != want {
		t.Fatalf("ManagementLink = %q, want %q", data.ManagementLink, want)
	}
}

func TestManagementLink_EmptyWithoutBaseURL(t *testing.T) {
	mail := &mockMailer{}
	svc := New(mail, nil, "", nil)

	data := svc.emailData(baseMessage(false))
	if data.ManagementLink != "" {
		t.Fatalf("expected empty ManagementLink without a configured base URL, got %q", data.ManagementLink)
	}
}

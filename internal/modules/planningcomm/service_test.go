package planningcomm

import (
	"context"
	"net/http"
	"testing"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/modules/outbound"

	"github.com/DATA-DOG/go-sqlmock"
)

type mockMailer struct {
	sendAsyncCalls int
	nextMessageID  string
}

func (m *mockMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	m.sendAsyncCalls++
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
func (m *mockMailer) SendOTP(data mailer.MfaOTPData) {}
func (m *mockMailer) SendInvoiceEmailToCustomer(to, customerName string, pdfBytes []byte, fileName string) error {
	return nil
}
func (m *mockMailer) TriggerTestEmail(writer http.ResponseWriter, request *http.Request) {}

type mockSMS struct {
	sendSMSCalls  int
	nextMessageID string
}

func (m *mockSMS) SendSMSAsync(senderID, phoneNumber, message string) { m.sendSMSCalls++ }
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

func TestSendPublishedWeek_DisabledSMSSkipsSMS(t *testing.T) {
	mail := &mockMailer{nextMessageID: "mail-msg-1"}
	txt := &mockSMS{nextMessageID: "sms-msg-1"}
	svc := New(mail, txt, "", nil, nil)

	svc.SendPublishedWeek(context.Background(), PublishedWeekMessage{
		WeekID:        "week-1",
		MerchantName:  "Le Bistrot",
		EmployeeName:  "Jean",
		EmployeeEmail: "jean@example.com",
		EmployeePhone: "+33612345678",
		WeekLabel:     "Semaine du 01/06/2026 au 07/06/2026",
		AllowSMS:      false,
		SendInlineSMS: true,
	})

	if mail.sendAsyncCalls != 1 {
		t.Fatalf("expected email to be sent, got %d", mail.sendAsyncCalls)
	}
	if txt.sendSMSCalls != 0 {
		t.Fatalf("expected no SMS when toggle disabled, got %d", txt.sendSMSCalls)
	}
}

func TestSendPublishedWeek_RecordsOutboundPlanningDomain(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mail := &mockMailer{nextMessageID: "mail-msg-1"}
	txt := &mockSMS{}
	svc := New(mail, txt, "", outbound.NewService(outbound.NewRepository(db), nil), nil)

	mock.ExpectExec("INSERT INTO outbound_messages").
		WithArgs(sqlmock.AnyArg(), "email", "brevo", "mail-msg-1", "planning", "week-1", "jean@example.com", "sent").
		WillReturnResult(sqlmock.NewResult(1, 1))

	svc.SendPublishedWeek(context.Background(), PublishedWeekMessage{
		WeekID:        "week-1",
		MerchantName:  "Le Bistrot",
		EmployeeName:  "Jean",
		EmployeeEmail: "jean@example.com",
		WeekLabel:     "Semaine du 01/06/2026 au 07/06/2026",
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

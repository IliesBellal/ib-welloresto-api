package order_life_cycle

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/pos/accounting"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// ---- Fakes des dépendances externes au flux testé ----

type fakeOrderFetcher struct {
	resp *models.PendingOrdersResponse
	err  error
}

func (f *fakeOrderFetcher) ComputeGetOrder(ctx context.Context, merchantID, orderID string) (*models.PendingOrdersResponse, error) {
	return f.resp, f.err
}

type fakeMerchantHeaderProvider struct {
	header *accounting.MerchantHeader
	err    error
}

func (f *fakeMerchantHeaderProvider) GetMerchantHeader(ctx context.Context, merchantID string) (*accounting.MerchantHeader, error) {
	return f.header, f.err
}

type fakeReceiptService struct {
	receipt *models.Receipt
	err     error
}

func (f *fakeReceiptService) GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error {
	return nil
}

func (f *fakeReceiptService) GenerateRefundReceipt(ctx context.Context, merchantID string, orderID string, originalReceipt *models.Receipt, refundAmountNegative int, mop string) error {
	return nil
}

func (f *fakeReceiptService) GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error) {
	return f.receipt, f.err
}

type fakeAuditService struct {
	called bool
}

func (f *fakeAuditService) LogChange(ctx context.Context, MerchantID, UserID, action, resourceType, resourceID string, oldState, newState interface{}) error {
	f.called = true
	return nil
}

type fakeMailerService struct {
	sendErr      error
	sentTo       string
	sentName     string
	sentFileName string
}

func (f *fakeMailerService) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
}
func (f *fakeMailerService) SendOrderConfirmationToCustomer(to string, data mailer.ScanNOrderConfirmationData) {
}
func (f *fakeMailerService) SendRefundNotification(s string, data mailer.RefundData) {}
func (f *fakeMailerService) SendPayoutPaidNotification(email, name string, payout mailer.PayoutData) {
}
func (f *fakeMailerService) SendOTP(data mailer.MfaOTPData)                          {}
func (f *fakeMailerService) SendPasswordReset(data mailer.PasswordResetData)         {}
func (f *fakeMailerService) TriggerTestEmail(w http.ResponseWriter, r *http.Request) {}
func (f *fakeMailerService) SendInvoiceEmailToCustomer(to, customerName string, pdfBytes []byte, fileName string) error {
	f.sentTo = to
	f.sentName = customerName
	f.sentFileName = fileName
	return f.sendErr
}

const (
	testMerchantID = "merchant_1"
	testUserID     = "user_1"
	testOrderID    = "order_1"
)

func testContext() context.Context {
	return middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: testUserID, MerchantID: testMerchantID})
}

func validOrderResponse() *models.PendingOrdersResponse {
	orderNum := "ORD-42"
	merchantID := testMerchantID
	return &models.PendingOrdersResponse{
		Orders: []models.Order{
			{OrderID: testOrderID, MerchantID: &merchantID, OrderNum: &orderNum},
		},
	}
}

func testReceipt() *models.Receipt {
	return &models.Receipt{
		ReceiptID:        "receipt-1",
		MerchantID:       testMerchantID,
		OrderID:          testOrderID,
		ReceiptNumber:    "F-2026-000046",
		TotalTTC:         1500,
		TotalHT:          1364,
		ItemsSnapshot:    []byte("[]"),
		PaymentsSnapshot: []byte("[]"),
		CreatedAt:        time.Now(),
	}
}

func testMerchantHeader() *accounting.MerchantHeader {
	return &accounting.MerchantHeader{MerchantName: "Brasserie Du Midi", Currency: "EUR"}
}

// newTestService construit un OrdersLifeCycleService dont seules les dépendances utilisées par
// SendInvoiceByEmail sont câblées (les autres champs restent nil, car non sollicités ici).
func newTestService(db *sql.DB, orderFetcher *fakeOrderFetcher, audit *fakeAuditService, mailerSvc *fakeMailerService) *OrdersLifeCycleService {
	customersRepo := customers.NewCustomerRepository(db)
	// nil audit.AuditService: this test only exercises SendInvoiceByEmail,
	// which never reaches customersService's audit-logged paths (pre-existing
	// build break fixed incidentally — NewCustomersService gained this param
	// after this file was last touched, unrelated to this lot).
	customersService := customers.NewCustomersService(customersRepo, nil)
	ordersLifeCycleRepo := NewOrdersLifeCycleRepository(db, customersRepo)

	return &OrdersLifeCycleService{
		db:                  db,
		auditService:        audit,
		ordersLifeCycleRepo: ordersLifeCycleRepo,
		ordersService:       orderFetcher,
		customersService:    customersService,
		receiptService:      &fakeReceiptService{receipt: testReceipt()},
		accountingRepo:      &fakeMerchantHeaderProvider{header: testMerchantHeader()},
		mailerService:       mailerSvc,
	}
}

const customerSelectColumns = "SELECT customer_id, customer_first_name, customer_last_name, customer_email"

func TestSendInvoiceByEmail_InvalidEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, &fakeMailerService{})

	resp, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "not-an-email"})
	if !errors.Is(err, models.ErrInvoiceInvalidEmail) {
		t.Fatalf("SendInvoiceByEmail() error = %v, want ErrInvoiceInvalidEmail", err)
	}
	if resp != nil {
		t.Fatalf("SendInvoiceByEmail() response should be nil on validation failure, got %+v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_OrderNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := newTestService(db, &fakeOrderFetcher{resp: &models.PendingOrdersResponse{Orders: []models.Order{}}}, &fakeAuditService{}, &fakeMailerService{})

	_, err = svc.SendInvoiceByEmail(testContext(), "missing_order", &SendInvoiceEmailRequest{Email: "client@example.fr"})
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("SendInvoiceByEmail() error = %v, want ErrNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_CustomerIDProvidedButNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, &fakeMailerService{})

	mock.ExpectBegin()
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_missing", testMerchantID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	customerID := "cus_missing"
	_, err = svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "client@example.fr", CustomerID: &customerID})
	if !errors.Is(err, models.ErrInvoiceCustomerNotFound) {
		t.Fatalf("SendInvoiceByEmail() error = %v, want ErrInvoiceCustomerNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_CustomerIDProvided_ExistingCustomer_LinksAndSendsEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := &fakeAuditService{}
	mailerSvc := &fakeMailerService{}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, audit, mailerSvc)

	mock.ExpectBegin()
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "old@example.fr"))
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "client@example.fr"))
	mock.ExpectExec("UPDATE orders").
		WithArgs("cus_1", testOrderID, testMerchantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	customerID := "cus_1"
	resp, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "client@example.fr", CustomerID: &customerID})
	if err != nil {
		t.Fatalf("SendInvoiceByEmail() error = %v", err)
	}
	if resp.CustomerID != "cus_1" {
		t.Fatalf("SendInvoiceByEmail() CustomerID = %q, want cus_1", resp.CustomerID)
	}
	if resp.EmailSentTo != "client@example.fr" {
		t.Fatalf("SendInvoiceByEmail() EmailSentTo = %q, want client@example.fr", resp.EmailSentTo)
	}
	if mailerSvc.sentTo != "client@example.fr" {
		t.Fatalf("mailer was called with to = %q, want client@example.fr", mailerSvc.sentTo)
	}
	if mailerSvc.sentName != "Jean Dupont" {
		t.Fatalf("mailer was called with customerName = %q, want Jean Dupont", mailerSvc.sentName)
	}
	if !audit.called {
		t.Fatalf("expected audit.LogChange to be called")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_EmailMatchesExistingCustomer_ReusesItWithoutCreatingDuplicate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, &fakeMailerService{})

	mock.ExpectBegin()
	mock.ExpectQuery("LOWER\\(customer_email\\)").
		WithArgs("client@example.fr", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_2", "Marie", "Curie", "client@example.fr"))
	// Seul un UPDATE est attendu : aucun INSERT (pas de doublon créé) puisque le client existe déjà.
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_2", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_2", "Marie", "Curie", "client@example.fr"))
	mock.ExpectExec("UPDATE orders").
		WithArgs("cus_2", testOrderID, testMerchantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resp, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "client@example.fr"})
	if err != nil {
		t.Fatalf("SendInvoiceByEmail() error = %v", err)
	}
	if resp.CustomerID != "cus_2" {
		t.Fatalf("SendInvoiceByEmail() CustomerID = %q, want cus_2 (existing customer reused)", resp.CustomerID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_NewEmail_CreatesCustomer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, &fakeMailerService{})

	mock.ExpectBegin()
	mock.ExpectQuery("LOWER\\(customer_email\\)").
		WithArgs("new@example.fr", testMerchantID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO customer").WillReturnResult(sqlmock.NewResult(99, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("99", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("99", "Paul", "Martin", "new@example.fr"))
	mock.ExpectExec("UPDATE orders").
		WithArgs("99", testOrderID, testMerchantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resp, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "new@example.fr", FirstName: "Paul", LastName: "Martin"})
	if err != nil {
		t.Fatalf("SendInvoiceByEmail() error = %v", err)
	}
	if resp.CustomerID != "99" {
		t.Fatalf("SendInvoiceByEmail() CustomerID = %q, want 99 (newly created customer)", resp.CustomerID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSendInvoiceByEmail_BrevoFailure_CustomerStillLinkedAndCommitted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mailerSvc := &fakeMailerService{sendErr: errors.New("brevo timeout")}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, mailerSvc)

	mock.ExpectBegin()
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "old@example.fr"))
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "client@example.fr"))
	mock.ExpectExec("UPDATE orders").
		WithArgs("cus_1", testOrderID, testMerchantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Le COMMIT doit avoir lieu MÊME SI l'envoi Brevo échoue ensuite (hors transaction).
	mock.ExpectCommit()

	customerID := "cus_1"
	resp, err := svc.SendInvoiceByEmail(testContext(), testOrderID, &SendInvoiceEmailRequest{Email: "client@example.fr", CustomerID: &customerID})

	var emailErr *EmailDeliveryError
	if !errors.As(err, &emailErr) {
		t.Fatalf("SendInvoiceByEmail() error = %v, want *EmailDeliveryError", err)
	}
	if resp == nil || resp.CustomerID != "cus_1" {
		t.Fatalf("SendInvoiceByEmail() response = %+v, want non-nil with CustomerID=cus_1 (customer must remain linked despite email failure)", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations (transaction must commit before attempting the email): %v", err)
	}
}

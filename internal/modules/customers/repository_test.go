package customers

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

// captureQueryMatcher accepte n'importe quelle requête mais conserve le dernier SQL exécuté,
// ce qui permet de vérifier la PRÉSENCE/ABSENCE de colonnes dans le SET dynamique
// de UpdateOrCreateCustomer (l'ordre des colonnes est non déterministe, basé sur l'itération
// d'une map Go).
type captureQueryMatcher struct {
	lastSQL string
}

func (c *captureQueryMatcher) Match(expectedSQL, actualSQL string) error {
	c.lastSQL = actualSQL
	return nil
}

func newCapturingMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *captureQueryMatcher) {
	t.Helper()
	capture := &captureQueryMatcher{}
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(capture.Match)))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	return db, mock, capture
}

func strPtr(s string) *string { return &s }

func TestUpdateOrCreateCustomer_PartialUpdate_PreservesEmptyFields(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewCustomerRepository(db)

	// On ne fournit QUE l'email (cas "prenom"/"nom" absents du payload) : le first/last name
	// existants en base ne doivent pas être écrasés par des valeurs vides.
	customer := &models.Customer{
		CustomerID:    strPtr("cus_1"),
		MerchantID:    "merchant_1",
		CustomerEmail: strPtr("client@example.fr"),
	}

	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))

	if _, err := repo.UpdateOrCreateCustomer(context.Background(), customer); err != nil {
		t.Fatalf("UpdateOrCreateCustomer() error = %v", err)
	}

	if !strings.Contains(capture.lastSQL, "customer_email = ?") {
		t.Fatalf("expected SET to include customer_email, got SQL: %s", capture.lastSQL)
	}
	if strings.Contains(capture.lastSQL, "customer_first_name") {
		t.Fatalf("expected SET to NOT include customer_first_name when not provided, got SQL: %s", capture.lastSQL)
	}
	if strings.Contains(capture.lastSQL, "customer_last_name") {
		t.Fatalf("expected SET to NOT include customer_last_name when not provided, got SQL: %s", capture.lastSQL)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpdateOrCreateCustomer_PartialUpdate_IncludesProvidedFields(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewCustomerRepository(db)

	customer := &models.Customer{
		CustomerID:        strPtr("cus_1"),
		MerchantID:        "merchant_1",
		CustomerEmail:     strPtr("client@example.fr"),
		CustomerFirstName: strPtr("Jean"),
	}

	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))

	if _, err := repo.UpdateOrCreateCustomer(context.Background(), customer); err != nil {
		t.Fatalf("UpdateOrCreateCustomer() error = %v", err)
	}

	if !strings.Contains(capture.lastSQL, "customer_first_name = ?") {
		t.Fatalf("expected SET to include customer_first_name when provided, got SQL: %s", capture.lastSQL)
	}
	if strings.Contains(capture.lastSQL, "customer_last_name") {
		t.Fatalf("expected SET to NOT include customer_last_name when not provided, got SQL: %s", capture.lastSQL)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGetCustomerByID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	mock.ExpectQuery("SELECT customer_id, customer_first_name, customer_last_name, customer_email").
		WithArgs("cus_missing", "merchant_1").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.GetCustomerByID(context.Background(), "cus_missing", "merchant_1")
	if err != sql.ErrNoRows {
		t.Fatalf("GetCustomerByID() error = %v, want sql.ErrNoRows", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestFindCustomerByEmail_CaseInsensitiveLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	rows := sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
		AddRow("cus_2", "Jean", "Dupont", "Client@Example.fr")

	mock.ExpectQuery("LOWER\\(customer_email\\) = LOWER\\(\\?\\)").
		WithArgs("client@example.fr", "merchant_1").
		WillReturnRows(rows)

	c, err := repo.FindCustomerByEmail(context.Background(), "client@example.fr", "merchant_1")
	if err != nil {
		t.Fatalf("FindCustomerByEmail() error = %v", err)
	}
	if c.CustomerID == nil || *c.CustomerID != "cus_2" {
		t.Fatalf("FindCustomerByEmail() got customer_id = %v, want cus_2", c.CustomerID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

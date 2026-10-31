package customers

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

func TestFindCustomerByPhone_NormalizesPhoneBeforeLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	rows := sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email", "customer_tel"}).
		AddRow("cus_9", "Jean", "Dupont", "client@example.fr", "+33612345678")

	mock.ExpectQuery("WHERE customer_tel = \\? AND enabled = true AND merchant_id = \\?").
		WithArgs("+33612345678", "merchant_1").
		WillReturnRows(rows)

	c, err := repo.FindCustomerByPhone(context.Background(), "06 12 34 56 78", "merchant_1")
	if err != nil {
		t.Fatalf("FindCustomerByPhone() error = %v", err)
	}
	if c.CustomerID == nil || *c.CustomerID != "cus_9" {
		t.Fatalf("FindCustomerByPhone() got customer_id = %v, want cus_9", c.CustomerID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

var searchResultColumns = []string{
	"customer_id", "customer_name", "customer_last_name", "customer_first_name", "customer_tel", "customer_address", "customer_email", "customer_nb_orders", "customer_total_spent", "creation_date", "last_order_date", "customer_code", "advertising_consent", "customer_brand", "blocked_at", "blocked_reason_id", "blocked_comment", "match_score", "total_count",
}

func searchResultRow(id, name string, score, total int) []driver.Value {
	return []driver.Value{
		id, name, "Dupont", "Jean", "+33612345678", "1 rue de Paris", "client@example.fr", 2, 1500, nil, nil, "C001", true, "WELLORESTO", nil, nil, nil, score, total,
	}
}

func TestSearchCustomers_ReturnsScoreAndTotalFromSameQuery(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()
	repo := NewCustomerRepository(db)

	// Une seule requête : le total vient de COUNT(*) OVER (), pas d'un second parcours.
	mock.ExpectQuery("anything").WillReturnRows(
		sqlmock.NewRows(searchResultColumns).
			AddRow(searchResultRow("cus_1", "Jean Dupont", 1000, 7)...).
			AddRow(searchResultRow("cus_2", "Jeanne Dupond", 300, 7)...),
	)

	results, total, err := repo.SearchCustomers(context.Background(), "merchant_1", "06 12 34 56 78", "", "", 1, 10)
	if err != nil {
		t.Fatalf("SearchCustomers() error = %v", err)
	}
	if total != 7 || len(results) != 2 {
		t.Fatalf("SearchCustomers() = (total=%d, len=%d), want (7, 2)", total, len(results))
	}
	if results[0].MatchScore != 1000 || results[1].MatchScore != 300 {
		t.Fatalf("match_score lu depuis SQL : %d, %d", results[0].MatchScore, results[1].MatchScore)
	}
	if results[0].Blocked || results[0].BlockedAt != nil {
		t.Fatalf("un client sans blocked_at ne doit pas apparaître bloqué : %+v", results[0])
	}
	if strings.Contains(capture.lastSQL, "SELECT COUNT(*) FROM") {
		t.Fatalf("aucun comptage séparé attendu : %s", capture.lastSQL)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSearchCustomers_WithoutSortField_OrdersByRelevanceThenLoyalty(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()
	repo := NewCustomerRepository(db)

	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows(searchResultColumns))

	// Cas du POS : ni sort_field ni sort_dir.
	if _, _, err := repo.SearchCustomers(context.Background(), "merchant_1", "dupont", "", "", 1, 10); err != nil {
		t.Fatalf("SearchCustomers() error = %v", err)
	}
	want := "ORDER BY match_score DESC, c.customer_nb_orders DESC, c.last_order_date DESC NULLS LAST, c.customer_id ASC"
	if !strings.Contains(capture.lastSQL, want) {
		t.Fatalf("tri par pertinence attendu (%s), got SQL: %s", want, capture.lastSQL)
	}
	if !strings.Contains(capture.lastSQL, "c.customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')") ||
		!strings.Contains(capture.lastSQL, "(c.enabled = true OR c.blocked_at IS NOT NULL)") {
		t.Fatalf("le prédicat des index partiels (migration 198) doit rester dans la requête : %s", capture.lastSQL)
	}
}

func TestSearchCustomers_WithSortField_KeepsRequestedOrder(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()
	repo := NewCustomerRepository(db)

	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows(searchResultColumns))

	// Cas du back-office : tri explicite de l'utilisateur.
	if _, _, err := repo.SearchCustomers(context.Background(), "merchant_1", "dupont", "customer_nb_orders", "DESC", 1, 10); err != nil {
		t.Fatalf("SearchCustomers() error = %v", err)
	}
	if strings.Contains(capture.lastSQL, "ORDER BY match_score") {
		t.Fatalf("un tri explicite ne doit pas être remplacé par la pertinence : %s", capture.lastSQL)
	}
	if !strings.Contains(capture.lastSQL, "c.customer_nb_orders DESC") {
		t.Fatalf("tri demandé absent : %s", capture.lastSQL)
	}
}

func TestSearchCustomers_TermTooShort_DoesNotQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	repo := NewCustomerRepository(db)

	// Aucune expectation : toute requête serait une erreur.
	for _, term := range []string{"a", "06", "- ."} {
		results, total, err := repo.SearchCustomers(context.Background(), "merchant_1", term, "", "", 1, 10)
		if err != nil || total != 0 || results == nil || len(results) != 0 {
			t.Fatalf("SearchCustomers(%q) = (%v, %d, %v), want empty non-nil slice", term, results, total, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSearchCustomers_PageBeyondEnd_FallsBackToCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	repo := NewCustomerRepository(db)

	mock.ExpectQuery(`SELECT\s+c\.customer_id`).WillReturnRows(sqlmock.NewRows(searchResultColumns))
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))

	results, total, err := repo.SearchCustomers(context.Background(), "merchant_1", "dupont", "", "", 3, 10)
	if err != nil || len(results) != 0 || total != 12 {
		t.Fatalf("SearchCustomers() = (%d rows, total=%d, %v), want (0, 12, nil)", len(results), total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSearchCustomers_EmptyTerm_ListsWithoutScore(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()
	repo := NewCustomerRepository(db)

	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows(searchResultColumns))

	if _, _, err := repo.SearchCustomers(context.Background(), "merchant_1", "  ", "", "", 1, 10); err != nil {
		t.Fatalf("SearchCustomers() error = %v", err)
	}
	if strings.Contains(capture.lastSQL, "customer_search_") {
		t.Fatalf("sans terme, aucune fonction de recherche : %s", capture.lastSQL)
	}
	if !strings.Contains(capture.lastSQL, "c.creation_date") {
		t.Fatalf("sans terme ni tri, ordre historique (creation_date) attendu : %s", capture.lastSQL)
	}
}

func TestGetFrequentCustomers_RanksByLoyaltyAndReturnsEligibleTotal(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()
	repo := NewCustomerRepository(db)

	mock.ExpectQuery("anything").
		WithArgs("merchant_1", FrequentCustomersRecencyMonths, 50).
		WillReturnRows(sqlmock.NewRows(searchResultColumns).
			AddRow(searchResultRow("cus_1", "Jean Dupont", 0, 120)...).
			AddRow(searchResultRow("cus_2", "Marie Martin", 0, 120)...))

	results, total, err := repo.GetFrequentCustomers(context.Background(), "merchant_1", 50)
	if err != nil || len(results) != 2 || total != 120 {
		t.Fatalf("GetFrequentCustomers() = (%d rows, total=%d, %v), want (2, 120, nil)", len(results), total, err)
	}
	for _, want := range []string{
		"c.customer_nb_orders > 0",
		"c.blocked_at IS NULL",
		"c.customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')",
		"ORDER BY c.customer_nb_orders DESC, c.last_order_date DESC NULLS LAST",
	} {
		if !strings.Contains(capture.lastSQL, want) {
			t.Errorf("clause attendue absente (%s) : %s", want, capture.lastSQL)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

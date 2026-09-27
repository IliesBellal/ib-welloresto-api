package scannorder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/database/dbx"
)

// TestGetDiscounts_AllOrderTypesAndLocalClock vérifie la requête de la liste
// des promotions ScanNOrder (bandeau) : promotions « tous modes »
// (discount_order_type NULL/vide) incluses comme le pricing les applique,
// min_order_unit nullable, créneaux comparés à l'heure locale reçue.
func TestGetDiscounts_AllOrderTypesAndLocalClock(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		sql := strings.Join(strings.Fields(actual), " ")
		for _, fragment := range []string{
			"COALESCE(d.discount_order_type, '')",
			"(COALESCE(d.discount_order_type, '') = '' OR d.discount_order_type LIKE ?)",
			"COALESCE(d.min_order_unit, '')",
			"ds.available_from <= ? AND (ds.available_to > ? OR ds.available_to = '00:00:00')",
			dbx.UTCDate("d.valid_from") + " <= ?",
			"(d.valid_to IS NULL OR " + dbx.UTCDate("d.valid_to") + " >= ?)",
			"d.enabled = true",
		} {
			if !strings.Contains(sql, fragment) {
				return errors.New("SQL fragment missing: " + fragment)
			}
		}
		if strings.Contains(sql, "AT TIME ZONE 'UTC' AS time") || strings.Contains(sql, "UTC_TIMESTAMP()") || strings.Contains(sql, "now()") {
			return errors.New("promotions must not be compared to the database clock")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}

	// Lundi 21/09 00:30 heure locale : date locale 2026-09-21 (en UTC il
	// serait encore dimanche 20/09 22:30).
	mock.ExpectQuery("").
		WithArgs("42", "%IN%", "2026-09-21", "2026-09-21", "00:30:00", "00:30:00", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"discount_id", "discount_order_type", "discount_code", "discount_desc", "discount_name",
			"discount_value", "discount_unit", "min_order_value", "min_order_unit", "max_discount_value",
			"max_discount_unit", "discounted_quantity", "is_cumulative", "available",
		}).AddRow("d1", "", nil, "desc", "Tous modes", 10, "PERCENTAGE", 0, "", nil, nil, 1, 0, 1))

	discounts, err := NewRepository(db).GetDiscounts(context.Background(), "42", "IN", time.Date(2026, 9, 21, 0, 30, 0, 0, paris))
	if err != nil {
		t.Fatalf("GetDiscounts: %v", err)
	}
	if len(discounts) != 1 || discounts[0].DiscountID != "d1" || discounts[0].DiscountOrderType != "" {
		t.Fatalf("unexpected discounts: %+v", discounts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

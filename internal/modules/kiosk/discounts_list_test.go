package kiosk

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
// des promotions borne (bandeau + badge promo) : promotions « tous modes »
// incluses comme le pricing les applique, min_order_unit nullable, créneaux
// comparés à l'heure locale reçue, ordre de priorité du pricing.
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
			"ORDER BY d.prefered_order ASC, d.discount_id ASC",
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
		WithArgs("42", "%%", "2026-09-21", "2026-09-21", "00:30:00", "00:30:00", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"discount_id", "discount_order_type", "discount_code", "discount_desc", "discount_name",
			"discount_value", "discount_unit", "min_order_value", "min_order_unit", "max_discount_value",
			"max_discount_unit", "discounted_quantity", "is_cumulative", "available", "prefered_order",
		}).AddRow("d1", "", nil, "desc", "Tous modes", 10, "PERCENTAGE", 0, "", nil, nil, 1, 0, 1, 0))

	discounts, err := NewRepository(db).GetDiscounts(context.Background(), "42", "", time.Date(2026, 9, 21, 0, 30, 0, 0, paris))
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

// TestGetDiscountProductIDs_SkipsNewPriceWithoutPrice : le badge promo ne
// doit pas signaler un produit d'une promotion NEWPRICE sans prix saisi, que
// le pricing ne remise pas.
func TestGetDiscountProductIDs_SkipsNewPriceWithoutPrice(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		sql := strings.Join(strings.Fields(actual), " ")
		if !strings.Contains(sql, "NOT (d.discount_unit = 'NEWPRICE' AND dp.new_price IS NULL)") {
			return errors.New("NEWPRICE without price must be filtered out")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("").
		WithArgs("d1", "d2").
		WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id"}).
			AddRow("d1", "12").AddRow("d1", "34").AddRow("d2", "56"))

	got, err := NewRepository(db).GetDiscountProductIDs(context.Background(), []string{"d1", "d2"})
	if err != nil {
		t.Fatalf("GetDiscountProductIDs: %v", err)
	}
	if len(got["d1"]) != 2 || len(got["d2"]) != 1 {
		t.Fatalf("unexpected products by discount: %+v", got)
	}
}

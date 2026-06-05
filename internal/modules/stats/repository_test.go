package stats

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestStatsRepositorySharedRevenuePredicatesAreIsoBehaviorAndReusableForHT(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewStatsRepository(db)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	baseWhere := `FROM orders o\s+WHERE o\.merchant_id = \?\s+AND o\.creation_date >= \?\s+AND o\.creation_date < \?\s+AND o\.state IN \('CLOSED', 'DONE'\)\s+AND o\.brand_status NOT IN \('DELETED', 'CANCELED'\)`

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SUM(o.price), 0) as total")+`\s+`+baseWhere).
		WithArgs("m-1", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(12345))

	ttc, err := repo.getRevenueForPeriod(ctx, "m-1", start, end)
	if err != nil {
		t.Fatalf("getRevenueForPeriod() error = %v", err)
	}
	if ttc != 12345 {
		t.Fatalf("getRevenueForPeriod() = %d, want 12345", ttc)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) as count")+`\s+`+baseWhere).
		WithArgs("m-1", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	count, err := repo.getOrderCountForPeriod(ctx, "m-1", start, end)
	if err != nil {
		t.Fatalf("getOrderCountForPeriod() error = %v", err)
	}
	if count != 7 {
		t.Fatalf("getOrderCountForPeriod() = %d, want 7", count)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ROUND(COALESCE(AVG(o.price), 0),0) as avg_basket")+`\s+`+baseWhere).
		WithArgs("m-1", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"avg_basket"}).AddRow(1764))

	avg, err := repo.getAverageBasketForPeriod(ctx, "m-1", start, end)
	if err != nil {
		t.Fatalf("getAverageBasketForPeriod() error = %v", err)
	}
	if avg != 1764 {
		t.Fatalf("getAverageBasketForPeriod() = %d, want 1764", avg)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SUM(o.HT), 0) as total")+`\s+`+baseWhere).
		WithArgs("m-1", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(9876))

	ht, err := repo.getRevenueHTForPeriod(ctx, "m-1", start, end)
	if err != nil {
		t.Fatalf("getRevenueHTForPeriod() error = %v", err)
	}
	if ht != 9876 {
		t.Fatalf("getRevenueHTForPeriod() = %d, want 9876", ht)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

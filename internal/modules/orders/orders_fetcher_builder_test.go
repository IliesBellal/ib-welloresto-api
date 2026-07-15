package orders

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newFetcherWithMock builds an OrdersFetcher backed by a sqlmock DB, in
// unordered mode so tests don't need to enumerate every sub-query in the exact
// order FetchAndBuildOrders happens to issue them.
func newFetcherWithMock(t *testing.T) (*OrdersFetcher, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.MatchExpectationsInOrder(false)
	return NewOrdersFetcher(db), mock
}

// expectSupportingQueries wires up empty-result expectations for every
// FetchAndBuildOrders sub-query except the "header" one, matched on a short
// unique fragment of each SELECT list. Args aren't constrained here — the
// tests only assert args on the header query, which carries whereFilters
// through to the very end of the function (WHERE ... + ORDER BY + LIMIT).
func expectSupportingQueries(mock sqlmock.Sqlmock) {
	fragments := []string{
		"ol.location_id, l.location_name",                     // locations
		"c.component_id, c.name, c.component_price",           // components
		"e.order_item_id, e.id, e.order_id, e.product_id, ce", // extras
		"w.order_item_id, w.id, w.order_id, w.product_id, cw", // withouts
		"cao.id, cao.title, cao.extra_price",                  // configurable options
		"ca.id, ca.title, ca.max_options, ca.attribute_type",  // configurable attributes
		"oc.id, oc.user_id, oc.content",                       // order comments
		"p.order_id, p.payment_id, p.mop",                     // payments
		"oi.quantity, oi.paid_quantity, oi.price",             // products
		"ds.id AS delivery_session_id",                        // delivery sessions (temp)
	}
	for _, f := range fragments {
		mock.ExpectQuery(f).WillReturnRows(sqlmock.NewRows([]string{"x"}))
	}
}

// TestFetchAndBuildOrders_SingleOrderIDFilter covers the case behind
// OrdersRepository.GetOrder: a single order_id filter must reach the DB as a
// `?` placeholder with the raw order ID passed as an argument, never
// interpolated into the SQL string.
func TestFetchAndBuildOrders_SingleOrderIDFilter(t *testing.T) {
	fetcher, mock := newFetcherWithMock(t)
	expectSupportingQueries(mock)

	filter := NewFilter(" AND o.order_id = ? ", "order_123")

	mock.ExpectQuery("o.order_id, o.order_num, o.order_type").
		WithArgs("merchant_1", "order_123").
		WillReturnRows(sqlmock.NewRows([]string{"x"}))

	if _, err := fetcher.FetchAndBuildOrders(context.Background(), "merchant_1", filter, "", ""); err != nil {
		t.Fatalf("FetchAndBuildOrders() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestFetchAndBuildOrders_InFilterMultipleOrderIDs covers the case behind
// OrdersRepository.GetOrdersByIDs / GetOrders / GetHistory: an IN (...) filter
// over several order IDs must produce as many `?` placeholders as IDs, with
// the args slice matching that order exactly (merchantID first, then the IDs
// in the order they were provided).
func TestFetchAndBuildOrders_InFilterMultipleOrderIDs(t *testing.T) {
	fetcher, mock := newFetcherWithMock(t)
	expectSupportingQueries(mock)

	filter := InFilter("o.order_id", []string{"order_1", "order_2", "order_3"})
	if filter.SQL != " AND o.order_id IN (?,?,?) " {
		t.Fatalf("unexpected filter SQL: %q", filter.SQL)
	}

	mock.ExpectQuery(`o\.order_id, o\.order_num, o\.order_type`).
		WithArgs("merchant_1", "order_1", "order_2", "order_3").
		WillReturnRows(sqlmock.NewRows([]string{"x"}))

	if _, err := fetcher.FetchAndBuildOrders(context.Background(), "merchant_1", filter, "", ""); err != nil {
		t.Fatalf("FetchAndBuildOrders() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

package orders

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestUnitBasePrice(t *testing.T) {
	cases := []struct {
		name      string
		basePrice sql.NullInt64
		price     sql.NullInt64
		want      int64
	}{
		{"ligne remisée : prix avant remise", sql.NullInt64{Int64: 1000, Valid: true}, sql.NullInt64{Int64: 800, Valid: true}, 1000},
		{"ligne offerte : prix avant remise conservé", sql.NullInt64{Int64: 1000, Valid: true}, sql.NullInt64{Int64: 0, Valid: true}, 1000},
		{"ligne sans remise", sql.NullInt64{Int64: 1000, Valid: true}, sql.NullInt64{Int64: 1000, Valid: true}, 1000},
		{"ancienne ligne base_price = 0 : repli sur price", sql.NullInt64{Int64: 0, Valid: true}, sql.NullInt64{Int64: 800, Valid: true}, 800},
		{"base_price NULL : repli sur price", sql.NullInt64{}, sql.NullInt64{Int64: 800, Valid: true}, 800},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unitBasePrice(c.basePrice, c.price); got != c.want {
				t.Fatalf("unitBasePrice = %d, want %d", got, c.want)
			}
		})
	}
}

// Colonnes scannées par la requête « products » et par la requête « header » de
// FetchAndBuildOrders (cf. rows.Scan dans orders_fetcher_builder.go). Seules les
// positions utiles au test sont renseignées, le reste reste NULL.
const (
	productColumns = 36
	headerColumns  = 64

	productColOrderID     = 0
	productColQuantity    = 1
	productColPrice       = 3
	productColProductID   = 4
	productColName        = 5
	productColOrderItemID = 8
	productColOrderedOn   = 11
	productColBasePrice   = 12
	productColDiscountID  = 13
	productColDiscountNm  = 15
)

func mockRows(columns int, values []driver.Value) *sqlmock.Rows {
	names := make([]string, columns)
	for i := range names {
		names[i] = fmt.Sprintf("c%d", i)
	}
	return sqlmock.NewRows(names).AddRow(values...)
}

// productRow décrit une ligne orderitems telle que stockée en base : `price`
// est le prix final remisé, `base_price` le prix avant remise.
func productRow(orderItemID string, price, basePrice int64, discountID *int64, discountName string) []driver.Value {
	row := make([]driver.Value, productColumns)
	row[productColOrderID] = "order_1"
	row[productColQuantity] = int64(1)
	row[productColPrice] = price
	row[productColProductID] = "product_1"
	row[productColName] = "Burger"
	row[productColOrderItemID] = orderItemID
	row[productColOrderedOn] = time.Unix(1_700_000_000, 0).UTC()
	row[productColBasePrice] = basePrice
	if discountID != nil {
		row[productColDiscountID] = *discountID
		row[productColDiscountNm] = discountName
	}
	return row
}

// TestFetchAndBuildOrders_ProductPriceContract verrouille le contrat JSON de
// l'API commandes : `price` est le prix unitaire AVANT remise (base_price) et
// `discounted_price` le prix final remisé, présent uniquement si une remise
// s'applique. Régression : `price` renvoyait le prix remisé, si bien que la
// caisse barrait un prix identique au prix remisé dans l'historique.
func TestFetchAndBuildOrders_ProductPriceContract(t *testing.T) {
	fetcher, mock := newFetcherWithMock(t)

	discountID := int64(12)

	// Enregistrée avant les attentes génériques : en mode non ordonné, sqlmock
	// retient la première attente non consommée qui correspond à la requête.
	names := make([]string, productColumns)
	for i := range names {
		names[i] = fmt.Sprintf("c%d", i)
	}
	productsMock := sqlmock.NewRows(names).
		AddRow(productRow("item_promo", 800, 1000, &discountID, "Happy hour")...).
		AddRow(productRow("item_plain", 1000, 1000, nil, "")...).
		AddRow(productRow("item_legacy", 1000, 0, nil, "")...). // ancienne ligne : base_price = 0 (DEFAULT 0 NOT NULL)
		AddRow(productRow("item_free", 0, 1000, &discountID, "Offert")...)
	mock.ExpectQuery("oi.quantity, oi.paid_quantity, oi.price").WillReturnRows(productsMock)

	header := make([]driver.Value, headerColumns)
	header[0] = "order_1"
	header[6] = "merchant_1"
	header[24] = time.Unix(1_700_000_000, 0).UTC() // last_update (toujours renseigné en base)
	header[27] = time.Unix(1_700_000_000, 0).UTC() // creation_date (idem)
	mock.ExpectQuery(`o\.order_id, o\.order_num, o\.order_type`).WillReturnRows(mockRows(headerColumns, header))

	expectSupportingQueries(mock)

	orders, err := fetcher.FetchAndBuildOrders(context.Background(), "merchant_1", NewFilter(" AND o.order_id = ? ", "order_1"), "", "")
	if err != nil {
		t.Fatalf("FetchAndBuildOrders() error = %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}

	type line struct {
		Price           float64  `json:"price"`
		DiscountedPrice *float64 `json:"discounted_price"`
	}
	got := map[string]line{}
	for _, p := range orders[0].Products {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal(product) error = %v", err)
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatalf("json.Unmarshal(product) error = %v", err)
		}
		got[p.OrderItemID] = l
	}

	ptr := func(v float64) *float64 { return &v }
	want := map[string]line{
		"item_promo":  {Price: 1000, DiscountedPrice: ptr(800)},
		"item_plain":  {Price: 1000, DiscountedPrice: nil},
		"item_legacy": {Price: 1000, DiscountedPrice: nil}, // repli sur price, jamais 0
		"item_free":   {Price: 1000, DiscountedPrice: ptr(0)},
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Errorf("%s: ligne absente de la réponse", id)
			continue
		}
		if g.Price != w.Price {
			t.Errorf("%s: price = %v, want %v", id, g.Price, w.Price)
		}
		switch {
		case w.DiscountedPrice == nil && g.DiscountedPrice != nil:
			t.Errorf("%s: discounted_price = %v, want absent", id, *g.DiscountedPrice)
		case w.DiscountedPrice != nil && g.DiscountedPrice == nil:
			t.Errorf("%s: discounted_price absent, want %v", id, *w.DiscountedPrice)
		case w.DiscountedPrice != nil && *g.DiscountedPrice != *w.DiscountedPrice:
			t.Errorf("%s: discounted_price = %v, want %v", id, *g.DiscountedPrice, *w.DiscountedPrice)
		}
	}
}

package order_life_cycle

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"
)

func testInvoiceReceipt() *models.Receipt {
	items, _ := json.Marshal([]models.SnapshotItem{
		{Name: "Burger", Quantity: 2, PriceTTC: 1500},
	})
	payments, _ := json.Marshal([]models.SnapshotPayment{
		{Amount: 1500, MOP: "CB"},
	})

	return &models.Receipt{
		ReceiptID:        "receipt-1",
		MerchantID:       "merchant_1",
		OrderID:          "order_1",
		ReceiptNumber:    "F-2026-000046",
		TotalTTC:         1500,
		TotalHT:          1364,
		ItemsSnapshot:    items,
		PaymentsSnapshot: payments,
		CreatedAt:        time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestBuildInvoicePDF_ContainsReceiptNumberAndTotalsWithoutRecomputing(t *testing.T) {
	vatNumber := "FR12345678901"
	header := &accounting.MerchantHeader{
		MerchantName: "Brasserie Du Midi",
		SIRET:        "12345678900012",
		VATNumber:    &vatNumber,
		Address:      "1 rue du Test",
		Phone:        "0102030405",
		Currency:     "EUR",
	}
	customer := invoiceCustomer{Name: "Jean Dupont", BusinessName: "Dupont SARL", Address: "2 avenue du Client"}
	vatLines := []invoiceVATLine{{Rate: 10, HT: 1364, TVA: 136, TTC: 1500}}

	pdfBytes, err := buildInvoicePDF(testInvoiceReceipt(), header, "ORD-42", customer, vatLines)
	if err != nil {
		t.Fatalf("buildInvoicePDF() error = %v", err)
	}

	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("buildInvoicePDF() did not produce a valid PDF header")
	}

	mustContain := []string{
		"F-2026-000046", // numéro de facture (pas recalculé, vient du Receipt)
		"Burger",
		"Brasserie Du Midi",
		"SIRET : 12345678900012",
		"FR12345678901",
		"Jean Dupont",
		"Dupont SARL",
		"2 avenue du Client",
		"10 %",
		"1.36 EUR", // TVA totale = TTC - HT figés
	}
	for _, marker := range mustContain {
		if !bytes.Contains(pdfBytes, []byte(marker)) {
			t.Errorf("buildInvoicePDF() output does not contain expected marker %q", marker)
		}
	}

	// Total TTC = 1500 -> "15.00 EUR" attendu littéralement, sans recalcul.
	if !bytes.Contains(pdfBytes, []byte("15.00 EUR")) {
		t.Errorf("buildInvoicePDF() output does not contain the un-recomputed total 15.00 EUR")
	}
}

func TestBuildInvoicePDF_OmitsMissingMentions(t *testing.T) {
	header := &accounting.MerchantHeader{MerchantName: "Brasserie Du Midi", Currency: "EUR"}

	pdfBytes, err := buildInvoicePDF(testInvoiceReceipt(), header, "ORD-42", invoiceCustomer{}, nil)
	if err != nil {
		t.Fatalf("buildInvoicePDF() error = %v", err)
	}

	for _, marker := range []string{"SIRET", "intracommunautaire", "Adresse", "Client", "Base HT"} {
		if bytes.Contains(pdfBytes, []byte(marker)) {
			t.Errorf("buildInvoicePDF() output contains %q although the information is missing", marker)
		}
	}
	if !bytes.Contains(pdfBytes, []byte("F-2026-000046")) {
		t.Errorf("buildInvoicePDF() output must still contain the receipt number")
	}
}

// strPtr est déclaré dans cancellation_test.go.
func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }
func int64Ptr(v int64) *int64     { return &v }

func TestComputeInvoiceVATBreakdown_SingleRateUsesFrozenTotals(t *testing.T) {
	order := &models.Order{
		OrderType: strPtr(models.OrderTypeIn),
		Products: []models.ProductEntry{
			{Price: 750, Quantity: intPtr(2), TVAIn: floatPtr(10)},
		},
	}

	lines := computeInvoiceVATBreakdown(order, nil, 1500, 1364)
	if len(lines) != 1 {
		t.Fatalf("computeInvoiceVATBreakdown() = %+v, want 1 line", lines)
	}
	if got := lines[0]; got.Rate != 10 || got.TTC != 1500 || got.HT != 1364 || got.TVA != 136 {
		t.Fatalf("computeInvoiceVATBreakdown() = %+v, want 10%% / TTC 1500 / HT 1364 / TVA 136", got)
	}
}

func TestComputeInvoiceVATBreakdown_MultipleRatesAndDeliveryFeesSumToFrozenTotals(t *testing.T) {
	// Livraison : plat à 10 % (20,00), boisson à 5,5 % (5,00), frais à 20 % (3,00).
	order := &models.Order{
		OrderType:    strPtr(models.OrderTypeDelivery),
		DeliveryFees: int64Ptr(300),
		Products: []models.ProductEntry{
			{Price: 2000, Quantity: intPtr(1), TVADelivery: floatPtr(10), TVAIn: floatPtr(20)},
			{Price: 250, Quantity: intPtr(2), TVADelivery: floatPtr(5.5)},
		},
	}
	// HT attendu : 1818 + 474 + 250 = 2542.
	lines := computeInvoiceVATBreakdown(order, floatPtr(20), 2800, 2542)
	if len(lines) != 3 {
		t.Fatalf("computeInvoiceVATBreakdown() = %+v, want 3 lines", lines)
	}

	want := []invoiceVATLine{
		{Rate: 5.5, TTC: 500, HT: 474, TVA: 26},
		{Rate: 10, TTC: 2000, HT: 1818, TVA: 182},
		{Rate: 20, TTC: 300, HT: 250, TVA: 50},
	}
	var sumTTC, sumHT int64
	for i, line := range lines {
		if line != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, line, want[i])
		}
		sumTTC += line.TTC
		sumHT += line.HT
	}
	if sumTTC != 2800 || sumHT != 2542 {
		t.Fatalf("lines sum to TTC %d / HT %d, want the frozen totals 2800 / 2542", sumTTC, sumHT)
	}
}

func TestComputeInvoiceVATBreakdown_OmittedWhenDataIsInconsistent(t *testing.T) {
	tests := []struct {
		name             string
		order            *models.Order
		deliveryFeesRate *float64
		totalHT          int64
	}{
		{
			name: "taux du produit inconnu",
			order: &models.Order{OrderType: strPtr(models.OrderTypeIn), Products: []models.ProductEntry{
				{Price: 1500, Quantity: intPtr(1)},
			}},
			totalHT: 1364,
		},
		{
			name: "taux des frais de livraison inconnu",
			order: &models.Order{OrderType: strPtr(models.OrderTypeDelivery), DeliveryFees: int64Ptr(300), Products: []models.ProductEntry{
				{Price: 1200, Quantity: intPtr(1), TVADelivery: floatPtr(10)},
			}},
			totalHT: 1364,
		},
		{
			// HT figé calculé à 20 % alors que la commande ne porte que du 10 %
			// (commande de plateforme, par exemple) : on n'imprime pas un détail faux.
			name: "HT figé incohérent avec le taux",
			order: &models.Order{OrderType: strPtr(models.OrderTypeIn), Products: []models.ProductEntry{
				{Price: 1500, Quantity: intPtr(1), TVAIn: floatPtr(10)},
			}},
			totalHT: 1250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if lines := computeInvoiceVATBreakdown(tt.order, tt.deliveryFeesRate, 1500, tt.totalHT); lines != nil {
				t.Fatalf("computeInvoiceVATBreakdown() = %+v, want nil", lines)
			}
		})
	}
}

func TestInvoiceCustomerFromOrder_OnlyUsesAddressOfTheSameCustomer(t *testing.T) {
	order := &models.Order{Customer: &models.Customer{
		CustomerID:                strPtr("cus_1"),
		CustomerBusinessName:      strPtr("Dupont SARL"),
		CustomerAddress:           strPtr("2 avenue du Client"),
		CustomerAdditionalAddress: strPtr("Bât. B"),
	}}

	same := invoiceCustomerFromOrder(order, "cus_1", "Jean Dupont")
	if same.Name != "Jean Dupont" || same.BusinessName != "Dupont SARL" || same.Address != "2 avenue du Client, Bât. B" {
		t.Fatalf("invoiceCustomerFromOrder() = %+v", same)
	}

	other := invoiceCustomerFromOrder(order, "cus_2", "Marie Curie")
	if other.Name != "Marie Curie" || other.BusinessName != "" || other.Address != "" {
		t.Fatalf("invoiceCustomerFromOrder() must not reuse another customer's details, got %+v", other)
	}
}

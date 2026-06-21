package order_life_cycle

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"
)

func TestBuildInvoicePDF_ContainsReceiptNumberAndTotalsWithoutRecomputing(t *testing.T) {
	items, _ := json.Marshal([]models.SnapshotItem{
		{Name: "Burger", Quantity: 2, PriceTTC: 1500},
	})
	payments, _ := json.Marshal([]models.SnapshotPayment{
		{Amount: 1500, MOP: "CB"},
	})

	rcpt := &models.Receipt{
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

	header := &accounting.MerchantHeader{
		MerchantName: "Brasserie Du Midi",
		SIRET:        "12345678900012",
		VATNumber:    nil,
		Address:      "1 rue du Test",
		Phone:        "0102030405",
		Currency:     "EUR",
	}

	pdfBytes, err := buildInvoicePDF(rcpt, header, "ORD-42")
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

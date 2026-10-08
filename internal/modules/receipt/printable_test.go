package receipt

import (
	"testing"
	"time"
)

// Ticket imprimable (lot E, phase 2) : vente en vigueur, avoirs, ticket
// complet ou antérieur.
func TestBuildOrderReceipts(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	complete := storedReceipt{
		number: "F-2026-000010", ttc: 1250, ht: 1136, createdAt: at, hash: "abc",
		tax:   []byte(`{"lines":[{"rate":10,"ttc":1250,"ht":1136,"tva":114}],"discount":0}`),
		items: []byte(`[{"name":"Pizza","quantity":1,"price_ttc":1100,"tax_rate":1000,"tax_amount":100,"kind":"article","total_ttc":1100,"total_ht":1000,"total_tva":100},
			{"name":"Burrata","quantity":1,"price_ttc":150,"tax_rate":1000,"tax_amount":14,"kind":"option","total_ttc":150,"total_ht":136,"total_tva":14,"parent":0}]`),
		pays: []byte(`[{"amount":1250,"mop":"CB"}]`),
	}
	refund := storedReceipt{number: "F-2026-000011", ttc: -1250, ht: -1136, createdAt: at.Add(time.Hour),
		tax: []byte(`{"lines":[{"rate":10,"ttc":-1250,"ht":-1136,"tva":-114}],"discount":0}`), items: []byte(`[]`), pays: []byte(`[]`)}
	legacy := storedReceipt{number: "F-2026-000003", ttc: 900, ht: 818, createdAt: at,
		items: []byte(`[{"name":"Café","quantity":3,"price_ttc":300,"tax_rate":27,"tax_amount":0}]`)}

	// Vente simple : en vigueur, lignes complètes, TVA, paiements.
	r := buildOrderReceipts("42", "CLOSED", []storedReceipt{complete})
	if !r.Closed || r.CurrentReceiptNumber == nil || *r.CurrentReceiptNumber != "F-2026-000010" || r.Software == "" {
		t.Fatalf("sale: %+v", r)
	}
	p := r.Receipts[0]
	if p.Type != PrintableSale || !p.Complete || p.TotalTVA != 114 || len(p.Lines) != 2 || p.Lines[1].Kind != "option" ||
		p.Lines[1].TaxRate != 10 || p.Lines[1].Parent == nil || *p.Lines[1].Parent != 0 || len(p.VAT) != 1 ||
		len(p.Payments) != 1 || p.Payments[0].MOP != "CB" {
		t.Fatalf("printable sale: %+v", p)
	}

	// Annulée : la vente et son avoir, plus de vente en vigueur.
	r = buildOrderReceipts("42", "CLOSED", []storedReceipt{complete, refund})
	if r.CurrentReceiptNumber != nil || r.Receipts[1].Type != PrintableRefund || r.Receipts[1].TotalTVA != -114 {
		t.Fatalf("cancelled: %+v", r)
	}

	// Rouverte et modifiée : avoir puis nouvelle vente, en vigueur.
	resale := complete
	resale.number = "F-2026-000012"
	r = buildOrderReceipts("42", "CLOSED", []storedReceipt{complete, refund, resale})
	if r.CurrentReceiptNumber == nil || *r.CurrentReceiptNumber != "F-2026-000012" {
		t.Fatalf("resale: %+v", r)
	}

	// Remboursement partiel : la vente reste en vigueur.
	partial := refund
	partial.ttc = -300
	r = buildOrderReceipts("42", "CLOSED", []storedReceipt{complete, partial})
	if r.CurrentReceiptNumber == nil || *r.CurrentReceiptNumber != "F-2026-000010" {
		t.Fatalf("partial refund: %+v", r)
	}

	// Ticket antérieur au lot D : prix unitaires, sans taux ni TVA ventilée.
	r = buildOrderReceipts("42", "CLOSED", []storedReceipt{legacy})
	if l := r.Receipts[0].Lines[0]; r.Receipts[0].Complete || l.TaxRate != 0 || l.TotalTTC != 900 || l.Kind != "article" ||
		len(r.Receipts[0].VAT) != 0 || r.Receipts[0].Payments == nil {
		t.Fatalf("legacy: %+v", r.Receipts[0])
	}

	// Commande ouverte : pas de ticket.
	r = buildOrderReceipts("42", "OPEN", nil)
	if r.Closed || r.CurrentReceiptNumber != nil || r.Receipts == nil || len(r.Receipts) != 0 {
		t.Fatalf("open: %+v", r)
	}
}

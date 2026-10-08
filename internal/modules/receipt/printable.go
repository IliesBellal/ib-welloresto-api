package receipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/version"
)

// Ticket fiscal exposé à la caisse (conformité caisse, lot E, constat C8 ;
// docs/attestation-conformite-06-lot-E-brief.md, phase 2) : la caisse imprime
// le ticket figé, avec son numéro et sa TVA ventilée, au lieu de le
// reconstruire depuis la commande.

// Types d'un ticket imprimable.
const (
	PrintableSale   = "SALE"
	PrintableRefund = "REFUND"
)

// OrderReceipts répond à GET /orders/{order_id}/receipt.
type OrderReceipts struct {
	Status  string `json:"status"`
	OrderID string `json:"order_id"`
	// Closed : commande close. Ouverte, elle n'a pas encore de ticket : la
	// caisse imprime une note qui ne vaut pas ticket.
	Closed bool `json:"closed"`
	// CurrentReceiptNumber : ticket de vente en vigueur (le dernier, s'il
	// n'a pas été annulé par un avoir), à imprimer comme justificatif ; nil
	// sans vente en vigueur (commande ouverte, annulée ou refusée).
	CurrentReceiptNumber *string `json:"current_receipt_number"`
	// Software : nom et version du logiciel, à imprimer en pied de ticket.
	Software string             `json:"software"`
	Receipts []PrintableReceipt `json:"receipts"` // tickets et avoirs, du plus ancien au plus récent
}

// PrintableReceipt est un ticket ou un avoir, tel que figé à son émission.
type PrintableReceipt struct {
	ReceiptNumber string    `json:"receipt_number"`
	Type          string    `json:"type"` // SALE | REFUND
	CreatedAt     time.Time `json:"created_at"`
	TotalTTC      int64     `json:"total_ttc"` // centimes, négatif pour un avoir
	TotalHT       int64     `json:"total_ht"`
	TotalTVA      int64     `json:"total_tva"`
	// Discount : remise de caisse du ticket (centimes), déjà déduite des
	// lignes de TVA.
	Discount int64 `json:"discount"`
	// Complete : lignes au format complet (lot D) : nature, taux et total de
	// chaque ligne. Sinon (ticket antérieur) : libellé, quantité et prix
	// unitaire seulement.
	Complete bool                     `json:"complete"`
	Lines    []PrintableLine          `json:"lines"`
	VAT      []PrintableVAT           `json:"vat"`
	Payments []models.SnapshotPayment `json:"payments"`
	Hash     string                   `json:"hash"` // empreinte du ticket, imprimable abrégée
}

// PrintableLine est une ligne de ticket.
type PrintableLine struct {
	Kind         string  `json:"kind"` // article, option, supplement, livraison, remise
	Name         string  `json:"name"`
	Quantity     int     `json:"quantity"`
	UnitPriceTTC int64   `json:"unit_price_ttc"`
	TaxRate      float64 `json:"tax_rate"`  // pourcentage (10 pour 10 %), 0 si inconnu
	TotalTTC     int64   `json:"total_ttc"` // quantité × prix unitaire pour un ticket antérieur
	Parent       *int    `json:"parent"`    // rang (base 0) de l'article d'une option ou d'un supplément
}

// PrintableVAT est une ligne de TVA ventilée.
type PrintableVAT struct {
	Rate float64 `json:"rate"`
	TTC  int64   `json:"ttc"`
	HT   int64   `json:"ht"`
	TVA  int64   `json:"tva"`
}

// storedReceipt : une ligne de receipts relue pour l'impression.
type storedReceipt struct {
	number           string
	ttc, ht          int64
	tax, items, pays []byte
	createdAt        time.Time
	hash             string
}

// listOrderReceipts lit l'état de la commande et ses tickets, dans
// l'établissement. found vaut false si la commande n'existe pas (ou est d'un
// autre établissement).
func (r *receiptRepository) ListOrderReceipts(ctx context.Context, merchantID, orderID string) (state string, list []storedReceipt, found bool, err error) {
	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, `
		SELECT COALESCE(o.state, ''), r.receipt_number, r.total_ttc, r.total_ht, r.tax_details::text,
		       r.items_snapshot::text, r.payments_snapshot::text, r.created_at, r.hash
		FROM orders o
		LEFT JOIN receipts r ON r.order_id = o.order_id
		WHERE o.order_id = ? AND o.merchant_id = ?
		ORDER BY r.created_at, r.receipt_number`, orderID, merchantID)
	if err != nil {
		return "", nil, false, fmt.Errorf("list order receipts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		found = true
		var number, tax, items, pays, hash sql.NullString
		var ttc, ht sql.NullInt64
		var created sql.NullTime
		if err := rows.Scan(&state, &number, &ttc, &ht, &tax, &items, &pays, &created, &hash); err != nil {
			return "", nil, false, fmt.Errorf("list order receipts: %w", err)
		}
		if !number.Valid {
			continue // commande sans ticket
		}
		list = append(list, storedReceipt{number: number.String, ttc: ttc.Int64, ht: ht.Int64,
			tax: bytesOrNil(tax), items: bytesOrNil(items), pays: bytesOrNil(pays), createdAt: created.Time, hash: hash.String})
	}
	return state, list, found, rows.Err()
}

func bytesOrNil(s sql.NullString) []byte {
	if !s.Valid {
		return nil
	}
	return []byte(s.String)
}

// GetOrderReceipts renvoie les tickets imprimables d'une commande de
// l'établissement (models.ErrNotFound sinon).
func (s *receiptService) GetOrderReceipts(ctx context.Context, merchantID, orderID string) (*OrderReceipts, error) {
	state, list, found, err := s.repo.ListOrderReceipts(ctx, merchantID, orderID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, models.ErrNotFound
	}
	return buildOrderReceipts(orderID, state, list), nil
}

func buildOrderReceipts(orderID, state string, list []storedReceipt) *OrderReceipts {
	out := &OrderReceipts{
		Status: "1", OrderID: orderID, Closed: state == "CLOSED",
		Software: version.Product + " " + version.Version, Receipts: make([]PrintableReceipt, 0, len(list)),
	}
	lastSale, refundsAfter := -1, int64(0)
	for i, sr := range list {
		p := printable(sr)
		out.Receipts = append(out.Receipts, p)
		if p.Type == PrintableSale {
			lastSale, refundsAfter = i, 0
		} else {
			refundsAfter += sr.ttc
		}
	}
	// Vente en vigueur : la dernière, sauf si ses avoirs l'annulent en
	// totalité (commande annulée, ou rouverte puis modifiée sans reclôture).
	if lastSale >= 0 {
		sale := list[lastSale]
		if !(refundsAfter < 0 && sale.ttc+refundsAfter <= 0) {
			n := sale.number
			out.CurrentReceiptNumber = &n
		}
	}
	return out
}

func printable(sr storedReceipt) PrintableReceipt {
	p := PrintableReceipt{
		ReceiptNumber: sr.number, Type: PrintableSale, CreatedAt: sr.createdAt.UTC(),
		TotalTTC: sr.ttc, TotalHT: sr.ht, TotalTVA: sr.ttc - sr.ht, Hash: sr.hash,
		Lines: []PrintableLine{}, VAT: []PrintableVAT{}, Payments: []models.SnapshotPayment{},
	}
	if sr.ttc < 0 {
		p.Type = PrintableRefund
	}
	var items []models.SnapshotItem
	_ = json.Unmarshal(sr.items, &items)
	for _, it := range items {
		if it.Kind != "" {
			p.Complete = true
			break
		}
	}
	for _, it := range items {
		l := PrintableLine{Kind: models.SnapshotKindArticle, Name: it.Name, Quantity: it.Quantity,
			UnitPriceTTC: it.PriceTTC, TotalTTC: it.PriceTTC * int64(it.Quantity)}
		if p.Complete {
			l.Kind, l.TotalTTC, l.Parent = it.Kind, it.TotalTTC, it.Parent
			l.TaxRate = float64(it.TaxRate) / 100
		}
		p.Lines = append(p.Lines, l)
	}
	if d, ok := fiscal.ParseTaxDetails(sr.tax); ok {
		p.Discount = d.Discount
		for _, l := range d.Lines {
			p.VAT = append(p.VAT, PrintableVAT{Rate: l.Rate, TTC: l.TTC, HT: l.HT, TVA: l.TVA})
		}
	}
	_ = json.Unmarshal(sr.pays, &p.Payments)
	if p.Payments == nil {
		p.Payments = []models.SnapshotPayment{}
	}
	return p
}

package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// Charges utiles v2, une par chaîne. Elles contiennent les données que le
// BOI-TVA-DECLA-30-10-30 §50 exige d'inaltérer, dans la représentation que la
// base restitue : identifiants numériques en entiers, horodatages FormatTime,
// jsonb passés par Canonical. Les colonnes qui changent légitimement après le
// scellement en sont exclues (voir le brief du lot A, phase 3) :
//   - orders : state, brand_status et les colonnes de service (mises à jour
//     par les plateformes après la clôture) ;
//   - payments : enabled (état dérivé), cash_register_id (rattaché au registre
//     à sa fermeture), fee, net_amount, status_check (écrits par Stripe) ;
//   - cash_registers : enclosed, closed_by, closure_comment et les relevés
//     cash_registers_custom_items (saisis après la fermeture).

// PaymentPayload scelle un paiement à son insertion.
type PaymentPayload struct {
	MerchantID    string  `json:"merchant_id"`
	OrderID       int64   `json:"order_id"`
	Amount        int64   `json:"amount"`
	MOP           string  `json:"mop"`
	OperationType string  `json:"operation_type"`
	PaymentDate   string  `json:"payment_date"`
	UserID        string  `json:"user_id"`
	Comment       *string `json:"comment"`
}

func NewPaymentPayload(merchantID, orderID string, amount int, mop, operationType string, paymentDate time.Time, userID string, comment *string) (PaymentPayload, error) {
	oid, err := parseID("order_id", orderID)
	if err != nil {
		return PaymentPayload{}, err
	}
	return PaymentPayload{
		MerchantID: merchantID, OrderID: oid, Amount: int64(amount), MOP: mop,
		OperationType: operationType, PaymentDate: FormatTime(paymentDate), UserID: userID, Comment: comment,
	}, nil
}

// ReceiptPayload scelle un ticket ou un avoir, détail compris.
type ReceiptPayload struct {
	MerchantID    string          `json:"merchant_id"`
	ReceiptNumber string          `json:"receipt_number"`
	OrderID       int64           `json:"order_id"`
	CreatedAt     string          `json:"created_at"`
	TotalTTC      int64           `json:"total_ttc"`
	TotalHT       int64           `json:"total_ht"`
	TaxDetails    json.RawMessage `json:"tax_details"`
	Items         json.RawMessage `json:"items"`
	Payments      json.RawMessage `json:"payments"`
}

func NewReceiptPayload(merchantID, receiptNumber, orderID string, createdAt time.Time, totalTTC, totalHT int, taxDetails, items, payments []byte) (ReceiptPayload, error) {
	oid, err := parseID("order_id", orderID)
	if err != nil {
		return ReceiptPayload{}, err
	}
	p := ReceiptPayload{
		MerchantID: merchantID, ReceiptNumber: receiptNumber, OrderID: oid,
		CreatedAt: FormatTime(createdAt), TotalTTC: int64(totalTTC), TotalHT: int64(totalHT),
	}
	if p.TaxDetails, err = Canonical(taxDetails); err != nil {
		return ReceiptPayload{}, err
	}
	if p.Items, err = Canonical(items); err != nil {
		return ReceiptPayload{}, err
	}
	if p.Payments, err = Canonical(payments); err != nil {
		return ReceiptPayload{}, err
	}
	return p, nil
}

// AuditLogPayload scelle une entrée du journal d'audit, état avant compris.
type AuditLogPayload struct {
	ID           string          `json:"id"`
	MerchantID   string          `json:"merchant_id"`
	UserID       string          `json:"user_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	CreatedAt    string          `json:"created_at"`
	OldValues    json.RawMessage `json:"old_values"`
	NewValues    json.RawMessage `json:"new_values"`
}

func NewAuditLogPayload(id, merchantID, userID, action, resourceType, resourceID string, createdAt time.Time, oldValues, newValues []byte) (AuditLogPayload, error) {
	p := AuditLogPayload{
		ID: id, MerchantID: merchantID, UserID: userID, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, CreatedAt: FormatTime(createdAt),
	}
	var err error
	if p.OldValues, err = Canonical(oldValues); err != nil {
		return AuditLogPayload{}, err
	}
	if p.NewValues, err = Canonical(newValues); err != nil {
		return AuditLogPayload{}, err
	}
	return p, nil
}

// OrderLinePayload est une ligne de commande à sa clôture.
type OrderLinePayload struct {
	OrderItemID int64    `json:"order_item_id"`
	ProductID   int64    `json:"product_id"`
	Quantity    int64    `json:"quantity"`
	BasePrice   *int64   `json:"base_price"`
	Price       int64    `json:"price"`
	TVARate     *float64 `json:"tva_rate"`
	DiscountID  *int64   `json:"discount_id"`
}

// OrderClosurePayload scelle une commande à sa clôture (vente, annulation ou
// refus).
type OrderClosurePayload struct {
	MerchantID         string             `json:"merchant_id"`
	OrderID            int64              `json:"order_id"`
	OrderType          *string            `json:"order_type"`
	Price              int64              `json:"price"`
	HT                 int64              `json:"ht"`
	TVA                int64              `json:"tva"`
	DeliveryFees       int64              `json:"delivery_fees"`
	CartDiscountAmount int64              `json:"cart_discount_amount"`
	DeliveredOn        string             `json:"delivered_on"`
	Lines              []OrderLinePayload `json:"lines"`
}

// LoadOrderClosure lit l'en-tête et les lignes d'une commande. deliveredOn est
// la date de clôture écrite avec l'empreinte.
func LoadOrderClosure(ctx context.Context, db *dbx.DB, orderID string, deliveredOn time.Time) (OrderClosurePayload, error) {
	p, err := loadOrderClosure(ctx, db, orderID, false)
	if err != nil {
		return OrderClosurePayload{}, err
	}
	p.DeliveredOn = FormatTime(deliveredOn)
	return p, nil
}

// LoadOrderClosureForUpdate lit la commande à sceller en verrouillant sa
// ligne jusqu'à la fin de la transaction : une modification concurrente de la
// commande ne peut plus s'intercaler entre la lecture et l'écriture de
// l'empreinte. DeliveredOn reste vide : l'appelant le fixe une fois le verrou
// de la chaîne obtenu, pour que les dates de clôture suivent l'ordre de la
// chaîne.
func LoadOrderClosureForUpdate(ctx context.Context, db *dbx.DB, orderID string) (OrderClosurePayload, error) {
	return loadOrderClosure(ctx, db, orderID, true)
}

// loadOrderClosure lit en-tête et lignes en une seule requête (un seul aller-
// retour : la clôture est sur le chemin de chaque encaissement final).
func loadOrderClosure(ctx context.Context, db *dbx.DB, orderID string, forUpdate bool) (OrderClosurePayload, error) {
	query := `
		SELECT o.merchant_id, o.order_id, o.order_type, o.price, o.ht, o.tva, o.delivery_fees, o.cart_discount_amount,
		       oi.order_item_id, oi.product_id, oi.quantity, oi.base_price, oi.price, oi.tva_rate, oi.discount_id
		FROM orders o
		LEFT JOIN orderitems oi ON oi.order_id = o.order_id
		WHERE o.order_id = ?
		ORDER BY oi.order_item_id`
	if forUpdate {
		query += ` FOR UPDATE OF o`
	}
	rows, err := db.QueryContext(ctx, query, orderID)
	if err != nil {
		return OrderClosurePayload{}, fmt.Errorf("fiscal: load order %s: %w", orderID, err)
	}
	defer rows.Close()

	p := OrderClosurePayload{Lines: []OrderLinePayload{}}
	found := false
	for rows.Next() {
		var (
			orderType                              sql.NullString
			itemID, productID, quantity, linePrice sql.NullInt64
			basePrice, discountID                  sql.NullInt64
			tvaRate                                sql.NullFloat64
		)
		if err := rows.Scan(&p.MerchantID, &p.OrderID, &orderType, &p.Price, &p.HT, &p.TVA, &p.DeliveryFees, &p.CartDiscountAmount,
			&itemID, &productID, &quantity, &basePrice, &linePrice, &tvaRate, &discountID); err != nil {
			return OrderClosurePayload{}, fmt.Errorf("fiscal: scan order %s: %w", orderID, err)
		}
		found = true
		p.OrderType = nil
		if orderType.Valid {
			p.OrderType = &orderType.String
		}
		if !itemID.Valid {
			continue // commande sans ligne
		}
		l := OrderLinePayload{OrderItemID: itemID.Int64, ProductID: productID.Int64, Quantity: quantity.Int64, Price: linePrice.Int64}
		if basePrice.Valid {
			l.BasePrice = &basePrice.Int64
		}
		if tvaRate.Valid {
			l.TVARate = &tvaRate.Float64
		}
		if discountID.Valid {
			l.DiscountID = &discountID.Int64
		}
		p.Lines = append(p.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return OrderClosurePayload{}, fmt.Errorf("fiscal: load order %s: %w", orderID, err)
	}
	if !found {
		return OrderClosurePayload{}, fmt.Errorf("fiscal: load order %s: %w", orderID, sql.ErrNoRows)
	}
	return p, nil
}

// CashRegisterItemPayload est une ligne du Z (moyen de paiement, montant).
type CashRegisterItemPayload struct {
	MOP    string `json:"mop"`
	Amount int64  `json:"amount"`
}

// CashRegisterClosurePayload scelle la fermeture d'un registre de caisse.
type CashRegisterClosurePayload struct {
	CashRegisterID int64                     `json:"cash_register_id"`
	MerchantID     string                    `json:"merchant_id"`
	StartDate      string                    `json:"start_date"`
	EndDate        string                    `json:"end_date"`
	CashFund       int64                     `json:"cash_fund"`
	FinalCashFund  int64                     `json:"final_cash_fund"`
	Items          []CashRegisterItemPayload `json:"items"`
}

// LoadCashRegisterClosure lit un registre et les lignes de son Z (déjà
// insérées dans la transaction). endDate et finalCashFund sont les valeurs que
// l'appelant s'apprête à écrire avec l'empreinte.
func LoadCashRegisterClosure(ctx context.Context, db *dbx.DB, cashRegisterID string, endDate time.Time, finalCashFund int) (CashRegisterClosurePayload, error) {
	p := CashRegisterClosurePayload{EndDate: FormatTime(endDate), FinalCashFund: int64(finalCashFund), Items: []CashRegisterItemPayload{}}
	var startDate time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT cash_register_id, merchant_id, start_date, cash_fund
		FROM cash_registers WHERE cash_register_id = ?`, cashRegisterID).
		Scan(&p.CashRegisterID, &p.MerchantID, &startDate, &p.CashFund); err != nil {
		return CashRegisterClosurePayload{}, fmt.Errorf("fiscal: load cash register %s: %w", cashRegisterID, err)
	}
	p.StartDate = FormatTime(startDate)

	rows, err := db.QueryContext(ctx, `
		SELECT mop, amount FROM cash_registers_items
		WHERE cash_register_id = ? ORDER BY mop, amount`, cashRegisterID)
	if err != nil {
		return CashRegisterClosurePayload{}, fmt.Errorf("fiscal: load cash register items %s: %w", cashRegisterID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var it CashRegisterItemPayload
		if err := rows.Scan(&it.MOP, &it.Amount); err != nil {
			return CashRegisterClosurePayload{}, fmt.Errorf("fiscal: scan cash register item %s: %w", cashRegisterID, err)
		}
		p.Items = append(p.Items, it)
	}
	return p, rows.Err()
}

func parseID(field, s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("fiscal: invalid %s %q: %w", field, s, err)
	}
	return id, nil
}

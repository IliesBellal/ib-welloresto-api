package order_life_cycle

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/diagnostics"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// Contrôle de cohérence d'une commande (docs/DIAGNOSTIC_EVENTS.md) : le serveur
// enregistre le total envoyé par la caisse sans le comparer aux lignes. Une
// tablette qui envoie un total périmé produit une commande « payée » dont les
// lignes ne couvrent pas les encaissements, scellée telle quelle à la clôture
// (commande 38912 du registre 934 : 20,70 € payés pour une pizza à 8,70 €).
//
// Ce contrôle ne bloque rien : il consigne l'écart dans diagnostic_events pour
// en trouver l'origine, après chaque création, modification et paiement.

const (
	// DiagnosticOrderPriceMismatch : orders.price ≠ somme des lignes (prix de la
	// ligne, suppléments, options payantes, × quantité) + frais de livraison.
	DiagnosticOrderPriceMismatch = "order_price_mismatch"
	// DiagnosticOrderOverpaid : paiements actifs (hors remises) > orders.price.
	DiagnosticOrderOverpaid = "order_overpaid"
)

// Étapes qui déclenchent le contrôle.
const (
	consistencyStageCreate  = "create"
	consistencyStageUpdate  = "update"
	consistencyStagePayment = "payment"
)

// orderConsistency est l'état chiffré (centimes) d'une commande.
type orderConsistency struct {
	Price         int64
	Lines         int64
	DeliveryFees  int64
	CartDiscount  int64
	Paid          int64
	DiscountsPaid int64
	Brand         string
	State         string
	BrandStatus   string
	OrderType     string
	CreatedBy     string
}

// expected est le total attendu d'après les lignes.
func (c orderConsistency) expected() int64 { return c.Lines + c.DeliveryFees }

// platformPriced : Uber Eats et Deliveroo envoient leur propre prix, dont on ne
// sait pas s'il inclut les options (même règle que le ticket de caisse).
func (c orderConsistency) platformPriced() bool {
	return c.Brand == models.BrandUberEats || c.Brand == models.BrandDeliveroo
}

func (c orderConsistency) priceMismatch() bool {
	return !c.platformPriced() && c.Price != c.expected()
}

func (c orderConsistency) overpaid() bool {
	return !c.platformPriced() && c.Paid > c.Price
}

// loadOrderConsistency lit le total, les lignes et les paiements d'une commande.
func loadOrderConsistency(ctx context.Context, db *dbx.DB, orderID string) (*orderConsistency, error) {
	var c orderConsistency
	var createdBy sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(o.price, 0), COALESCE(o.delivery_fees, 0), COALESCE(o.cart_discount_amount, 0),
		       COALESCE(o.brand, ''), COALESCE(o.state, ''), COALESCE(o.brand_status, ''),
		       COALESCE(o.order_type, ''), o.created_by,
		       COALESCE((
		           SELECT SUM((oi.price + COALESCE(e.extra_price, 0) + `+models.OrderItemPaidOptionsTTCSQL("opt", "o")+`) * oi.quantity)
		           FROM orderitems oi
		           LEFT JOIN (SELECT order_item_id, SUM(extra.price) AS extra_price FROM extra GROUP BY order_item_id) e
		                  ON e.order_item_id = oi.order_item_id
		           `+models.OrderItemPaidOptionsJoinSQL("oi", "opt")+`
		           WHERE oi.order_id = o.order_id
		       ), 0),
		       COALESCE((SELECT SUM(p.amount) FROM payments p
		                 WHERE p.order_id = o.order_id AND p.enabled IS TRUE AND upper(p.mop) NOT IN `+models.DiscountMOPsSQL+`), 0),
		       COALESCE((SELECT SUM(p.amount) FROM payments p
		                 WHERE p.order_id = o.order_id AND p.enabled IS TRUE AND upper(p.mop) IN `+models.DiscountMOPsSQL+`), 0)
		FROM orders o
		WHERE o.order_id = ?`, orderID).
		Scan(&c.Price, &c.DeliveryFees, &c.CartDiscount, &c.Brand, &c.State, &c.BrandStatus,
			&c.OrderType, &createdBy, &c.Lines, &c.Paid, &c.DiscountsPaid)
	if err != nil {
		return nil, err
	}
	c.CreatedBy = createdBy.String
	return &c, nil
}

// checkOrderConsistency consigne les incohérences d'une commande. Ne renvoie
// rien et ne panique jamais : appelé après le succès de l'opération métier, sa
// propre panne ne doit rien changer au flux.
func (s *OrdersLifeCycleService) checkOrderConsistency(ctx context.Context, merchantID, userID, orderID, stage string) {
	if orderID == "" || s.db == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.FromContext(ctx).Error("order consistency check panicked", zap.String("order_id", orderID), zap.Any("panic", r))
		}
	}()

	// Lecture hors de la transaction de l'appelant (ctx peut en porter une) : une
	// requête en échec y rendrait toutes les suivantes invalides.
	qctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := loadOrderConsistency(qctx, dbx.GetDB(qctx, s.db), orderID)
	if err != nil {
		logger.FromContext(ctx).Warn("order consistency check failed", zap.String("order_id", orderID), zap.Error(err))
		return
	}

	data := func() map[string]any {
		return map[string]any{
			"stage":            stage,
			"stored_price":     c.Price,
			"lines_sum":        c.Lines,
			"delivery_fees":    c.DeliveryFees,
			"expected_price":   c.expected(),
			"delta":            c.Price - c.expected(),
			"paid":             c.Paid,
			"discounts_paid":   c.DiscountsPaid,
			"cart_discount":    c.CartDiscount,
			"brand":            c.Brand,
			"state":            c.State,
			"brand_status":     c.BrandStatus,
			"order_type":       c.OrderType,
			"order_created_by": c.CreatedBy,
		}
	}

	if c.priceMismatch() {
		diagnostics.Record(ctx, diagnostics.Event{
			Kind: DiagnosticOrderPriceMismatch, Severity: diagnostics.SeverityWarn,
			MerchantID: merchantID, UserID: userID,
			SubjectType: "order", SubjectID: orderID, Data: data(),
		})
	}
	// Un excédent n'a de sens qu'à l'encaissement.
	if stage == consistencyStagePayment && c.overpaid() {
		diagnostics.Record(ctx, diagnostics.Event{
			Kind: DiagnosticOrderOverpaid, Severity: diagnostics.SeverityWarn,
			MerchantID: merchantID, UserID: userID,
			SubjectType: "order", SubjectID: orderID, Data: data(),
		})
	}
}

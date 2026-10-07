package fiscal

import (
	"context"
	"fmt"

	"welloresto-api/internal/database/dbx"
)

// Les commandes sont scellées par leur clôture journalière (CloseDueDays,
// décision S1 du lot B conformité caisse), plus par leur propre ligne : une
// clôture de commande n'écrit que son état et sa date de clôture, qui la
// rattache à son jour.

// ClosureColumns est le fragment SET commun aux clôtures de commande
// plateformes ; son paramètre est la date de clôture (ClosureArgs).
const ClosureColumns = `state = 'CLOSED', delivered_on = ?`

// ClosureArgs renvoie le paramètre de ClosureColumns : maintenant, dans la
// précision de timestamptz.
func ClosureArgs() []any {
	return []any{Now()}
}

// OpenPlatformOrder est une commande plateforme encore ouverte, verrouillée.
type OpenPlatformOrder struct {
	OrderID     string
	MerchantID  string
	BrandStatus string
}

// LockOpenOrdersByBrandOrderID verrouille, jusqu'à la fin de la transaction,
// les commandes encore ouvertes d'une commande plateforme (Uber Eats,
// Deliveroo) : ce sont les seules qu'une clôture plateforme clôt. Une
// commande déjà close ne l'est jamais une seconde fois.
func LockOpenOrdersByBrandOrderID(ctx context.Context, db *dbx.DB, brandOrderID string) ([]OpenPlatformOrder, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT order_id::text, merchant_id, brand_status FROM orders
		WHERE brand_order_id = ? AND state = 'OPEN'
		ORDER BY order_id
		FOR UPDATE`, brandOrderID)
	if err != nil {
		return nil, fmt.Errorf("fiscal: lock open orders of %s: %w", brandOrderID, err)
	}
	defer rows.Close()
	var out []OpenPlatformOrder
	for rows.Next() {
		var o OpenPlatformOrder
		if err := rows.Scan(&o.OrderID, &o.MerchantID, &o.BrandStatus); err != nil {
			return nil, fmt.Errorf("fiscal: scan open order of %s: %w", brandOrderID, err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OrderFullyPaid indique si les paiements actifs d'une commande couvrent
// exactement son prix : condition d'une clôture de vente (comme
// SetDeliveredLocal).
func OrderFullyPaid(ctx context.Context, db *dbx.DB, orderID string) (bool, error) {
	var price, paid int64
	if err := db.QueryRowContext(ctx, `
		SELECT o.price, COALESCE((SELECT SUM(p.amount) FROM payments p WHERE p.order_id = o.order_id AND p.enabled = TRUE), 0)
		FROM orders o WHERE o.order_id = ?`, orderID).Scan(&price, &paid); err != nil {
		return false, fmt.Errorf("fiscal: read order %s balance: %w", orderID, err)
	}
	return price == paid, nil
}

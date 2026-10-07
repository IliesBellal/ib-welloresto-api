package fiscal

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
)

// OrderClosureSeal est le scellement d'une clôture de commande, à écrire avec
// elle : voir SealColumns et Args.
type OrderClosureSeal struct {
	MerchantID string
	ClosedAt   time.Time
	Prev       string
	Hash       string
	Signature  string
}

// SealColumns est le fragment SET d'une clôture scellée ; ses paramètres sont
// Args, dans cet ordre.
const SealColumns = `state = 'CLOSED', delivered_on = ?, previous_hash = ?, hash = ?, signature = ?, hash_version = ?`

// Args renvoie les paramètres de SealColumns.
func (s *OrderClosureSeal) Args() []any {
	return []any{s.ClosedAt, s.Prev, s.Hash, s.Signature, HashVersion}
}

// SealOrderClosure scelle la clôture de orderID (vente, annulation, refus ou
// échec) : lecture verrouillée de la commande et de ses lignes, verrou de la
// chaîne orders de l'établissement, dernier maillon chaîné, puis empreinte v2.
// Doit être appelée dans la transaction qui écrit la clôture.
//
// La ligne de la commande est verrouillée avant la chaîne : un encaissement
// tient la chaîne payments (pas orders) quand il attend cette ligne, donc
// aucun cycle.
//
// Le dernier maillon est la dernière commande chaînée, close ou rouverte :
// exclure une commande rouverte créerait une fourche au maillon suivant, et
// inclure les commandes sans empreinte (antérieures à la chaîne, ou clôturées
// sans delivered_on — triées en tête par DESC) faisait redémarrer la chaîne.
func SealOrderClosure(ctx context.Context, db *dbx.DB, orderID string) (*OrderClosureSeal, error) {
	payload, err := LoadOrderClosureForUpdate(ctx, db, orderID)
	if err != nil {
		return nil, err
	}
	if err := LockChain(ctx, ChainOrders, payload.MerchantID); err != nil {
		return nil, err
	}
	var prevHash sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT hash FROM orders
		WHERE merchant_id = ? AND hash IS NOT NULL
		ORDER BY delivered_on DESC, order_id DESC LIMIT 1
	`, payload.MerchantID).Scan(&prevHash); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("fiscal: read previous order hash: %w", err)
	}

	// Date de clôture prise sous le verrou de la chaîne : elle suit l'ordre
	// des maillons.
	seal := &OrderClosureSeal{MerchantID: payload.MerchantID, ClosedAt: Now(), Prev: PrevOrGenesis(prevHash.String)}
	payload.DeliveredOn = FormatTime(seal.ClosedAt)
	if seal.Hash, seal.Signature, err = Seal(ChainOrders, seal.Prev, payload); err != nil {
		return nil, err
	}
	return seal, nil
}

// OpenPlatformOrder est une commande plateforme encore ouverte, verrouillée.
type OpenPlatformOrder struct {
	OrderID     string
	BrandStatus string
}

// LockOpenOrdersByBrandOrderID verrouille, jusqu'à la fin de la transaction,
// les commandes encore ouvertes d'une commande plateforme (Uber Eats,
// Deliveroo) : ce sont les seules qu'une clôture plateforme scelle. Une
// commande déjà close n'est jamais rechaînée.
func LockOpenOrdersByBrandOrderID(ctx context.Context, db *dbx.DB, brandOrderID string) ([]OpenPlatformOrder, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT order_id::text, brand_status FROM orders
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
		if err := rows.Scan(&o.OrderID, &o.BrandStatus); err != nil {
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

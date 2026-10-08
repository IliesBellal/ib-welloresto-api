package fiscalverify

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/fiscal"
)

// numbering contrôle la numérotation des tickets (F-AAAA-NNNNNN) de chaque
// année touchée par la période : chaque année commence à 000001, sans trou ni
// doublon.
func (v *verifier) numbering(ctx context.Context) error {
	c := v.r.check("numerotation", "Numérotation des tickets continue, sans doublon")
	first := v.from
	if v.fromDay == "" {
		var min sql.NullTime
		if err := v.db.QueryRowContext(ctx, `SELECT min(created_at) FROM receipts WHERE merchant_id = ?`, v.merchant).Scan(&min); err != nil {
			return fmt.Errorf("fiscalverify: numbering: %w", err)
		}
		if !min.Valid {
			return nil
		}
		first = min.Time
	}
	last := v.to.Add(-time.Microsecond)
	for year := first.UTC().Year(); year <= last.UTC().Year(); year++ {
		if err := v.numberingYear(ctx, c, year); err != nil {
			return err
		}
	}
	return nil
}

type numbered struct {
	seq     int
	number  string
	version int
}

func (v *verifier) numberingYear(ctx context.Context, c *CheckSummary, year int) error {
	prefix := fmt.Sprintf("F-%d-", year)
	rows, err := v.db.QueryContext(ctx, `
		SELECT receipt_number, hash_version FROM receipts WHERE merchant_id = ? AND receipt_number LIKE ?`,
		v.merchant, prefix+"%")
	if err != nil {
		return fmt.Errorf("fiscalverify: numbering %d: %w", year, err)
	}
	var list []numbered
	for rows.Next() {
		var n numbered
		if err := rows.Scan(&n.number, &n.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: numbering %d: %w", year, err)
		}
		seq, err := strconv.Atoi(strings.TrimPrefix(n.number, prefix))
		if err != nil || len(n.number) != len(prefix)+6 {
			v.r.add(severity(n.version >= fiscal.HashVersion), c.Check, "ticket %q : numéro mal formé", n.number)
			continue
		}
		n.seq = seq
		list = append(list, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: numbering %d: %w", year, err)
	}
	c.Checked += len(list)
	sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })
	expected := 1
	for i, n := range list {
		attested := n.version >= fiscal.HashVersion
		switch {
		case i > 0 && n.seq == list[i-1].seq:
			v.r.add(severity(attested || list[i-1].version >= fiscal.HashVersion), c.Check, "numéro %s en double", n.number)
		case n.seq > expected:
			v.r.add(severity(attested), c.Check, "numéros manquants de %s%06d à %s%06d", prefix, expected, prefix, n.seq-1)
		}
		expected = n.seq + 1
	}
	return nil
}

// cancelledStatuses : commandes closes sans vente (annulées, refusées).
var cancelledStatuses = map[string]bool{
	"CANCELED": true, "CANCELLED": true, "DENIED": true, "REJECTED": true, "DELETED": true, "FAILED": true,
}

// ordersVsReceipts recoupe chaque commande close de la période avec ses
// tickets :
//   - vente : au moins un ticket de vente, le dernier égal au prix de la
//     commande, et la somme de ses tickets entre 0 et ce prix (avoirs de
//     remboursement déduits) ;
//   - commande annulée ou refusée : tickets compensés par leurs avoirs
//     (somme nulle).
func (v *verifier) ordersVsReceipts(ctx context.Context) error {
	c := v.r.check("commandes_tickets", "Commandes closes recoupées avec leurs tickets")
	rows, err := v.db.QueryContext(ctx, `
		SELECT o.order_id::text, o.price, upper(COALESCE(o.brand_status, '')), o.delivered_on,
		       count(r.receipt_id) FILTER (WHERE r.total_ttc >= 0), COALESCE(sum(r.total_ttc), 0),
		       (SELECT r2.total_ttc FROM receipts r2 WHERE r2.order_id = o.order_id AND r2.total_ttc >= 0
		        ORDER BY r2.created_at DESC, r2.receipt_number DESC LIMIT 1)
		FROM orders o
		LEFT JOIN receipts r ON r.order_id = o.order_id
		WHERE o.merchant_id = ? AND o.state = 'CLOSED' AND o.delivered_on >= ? AND o.delivered_on < ?
		GROUP BY o.order_id, o.price, o.brand_status, o.delivered_on
		ORDER BY o.delivered_on, o.order_id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: orders vs receipts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		var price, sales, sum int64
		var closed time.Time
		var lastSale sql.NullInt64
		if err := rows.Scan(&id, &price, &status, &closed, &sales, &sum, &lastSale); err != nil {
			return fmt.Errorf("fiscalverify: orders vs receipts: %w", err)
		}
		c.Checked++
		sev := severity(v.attestedAt(closed))
		label := fmt.Sprintf("commande %s (close le %s)", id, closed.In(v.loc).Format("2006-01-02 15:04"))
		if cancelledStatuses[status] {
			if sum != 0 {
				v.r.add(sev, c.Check, "%s, %s : tickets non compensés par un avoir (somme %d)", label, strings.ToLower(status), sum)
			}
			continue
		}
		switch {
		case sales == 0:
			v.r.add(sev, c.Check, "%s : vente sans ticket", label)
		case lastSale.Int64 != price:
			v.r.add(sev, c.Check, "%s : dernier ticket de vente %d, prix de la commande %d", label, lastSale.Int64, price)
		case sum < 0 || sum > price:
			v.r.add(sev, c.Check, "%s : somme des tickets %d hors de [0, %d]", label, sum, price)
		}
	}
	return rows.Err()
}

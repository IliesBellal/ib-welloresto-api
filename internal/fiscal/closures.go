package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // fuseaux des établissements, quel que soit l'hôte

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

// Clôtures fiscales (lot B conformité caisse, constat C6 ; BOI-TVA-DECLA-30-10-30
// §170) : journalière, mensuelle et annuelle, produites par le logiciel sans
// action du restaurateur, scellées et chaînées (table fiscal_closures,
// migration 170). La clôture journalière scelle aussi l'empreinte de chaque
// commande clôturée ce jour-là (décision S1).
//
// Jour = jour calendaire dans le fuseau de l'établissement. Contenu :
//   - chiffre d'affaires : tickets et avoirs créés ce jour-là, nets des
//     remises de caisse (receipts.tax_details) ; un ticket antérieur au lot B
//     (sans ventilation) compte pour son total, sans ventilation de TVA ;
//   - paiements créés ce jour-là, par moyen, type et état ;
//   - commandes closes (delivered_on) ce jour-là : statut et empreinte.
// Mois et année : somme de leurs journées. Grand total de la période :
// cumul du net TTC depuis le 1er janvier ; total perpétuel : cumul depuis la
// première clôture, valeur d'ouverture comprise (somme des tickets
// antérieurs, décision d'Ilies du 2026-10-07).

// Types de période.
const (
	PeriodDay   = "DAY"
	PeriodMonth = "MONTH"
	PeriodYear  = "YEAR"
)

// closingHour : heure locale à partir de laquelle la veille est clôturée
// (aucun établissement ne sert après 23 h).
const closingHour = 3

const dateLayout = "2006-01-02"

// VATTotal est le total d'une période à un taux de TVA (centimes, net).
type VATTotal struct {
	Rate float64 `json:"rate"`
	TTC  int64   `json:"ttc"`
	HT   int64   `json:"ht"`
	TVA  int64   `json:"tva"`
}

// PaymentTotal est le total des paiements d'une période par moyen, type et
// état (centimes).
type PaymentTotal struct {
	MOP           string `json:"mop"`
	OperationType string `json:"operation_type"`
	Enabled       bool   `json:"enabled"`
	Amount        int64  `json:"amount"`
	Count         int64  `json:"count"`
}

// ChannelTotal est le chiffre d'affaires net TTC d'une période par canal
// (orders.order_source de la commande du ticket).
type ChannelTotal struct {
	Source string `json:"source"`
	NetTTC int64  `json:"net_ttc"`
}

// OrderFingerprint est une commande scellée par sa clôture journalière.
type OrderFingerprint struct {
	OrderID int64  `json:"order_id"`
	Status  string `json:"status"` // brand_status au moment du scellement
	Hash    string `json:"hash"`   // Fingerprint("order_closure", OrderClosurePayload)
}

// ClosureOpening est la valeur d'ouverture portée par la première clôture
// d'un établissement : tickets antérieurs à son premier jour clôturé.
type ClosureOpening struct {
	Method        string `json:"method"` // "receipts_before"
	Until         string `json:"until"`  // borne exclue (UTC)
	ReceiptsCount int64  `json:"receipts_count"`
	NetTTC        int64  `json:"net_ttc"`
	YearNetTTC    int64  `json:"year_net_ttc"` // depuis le 1er janvier de l'année du premier jour
}

// ClosurePayload est une clôture fiscale, telle que scellée et stockée.
type ClosurePayload struct {
	MerchantID         string             `json:"merchant_id"`
	PeriodType         string             `json:"period_type"`
	PeriodStart        string             `json:"period_start"`
	PeriodEnd          string             `json:"period_end"`
	Timezone           string             `json:"timezone"`
	ClosedAt           string             `json:"closed_at"`
	SalesTTC           int64              `json:"sales_ttc"`
	SalesHT            int64              `json:"sales_ht"`
	RefundsTTC         int64              `json:"refunds_ttc"`
	RefundsHT          int64              `json:"refunds_ht"`
	NetTTC             int64              `json:"net_ttc"`
	NetHT              int64              `json:"net_ht"`
	VATByRate          []VATTotal         `json:"vat_by_rate"`
	PaymentsByMOP      []PaymentTotal     `json:"payments_by_mop"`
	ByChannel          []ChannelTotal     `json:"by_channel"`
	ReceiptsCount      int64              `json:"receipts_count"`
	FirstReceiptNumber *string            `json:"first_receipt_number"`
	LastReceiptNumber  *string            `json:"last_receipt_number"`
	OrdersCount        int64              `json:"orders_count"`
	Orders             []OrderFingerprint `json:"orders"`  // DAY seulement
	Opening            *ClosureOpening    `json:"opening"` // première clôture seulement
	GrandTotalPeriod   int64              `json:"grand_total_period"`
	PerpetualTotal     int64              `json:"perpetual_total"`
}

// ErrInvalidTimezone : fuseau d'établissement illisible ; ses clôtures sont
// suspendues (journalisé par l'appelant) plutôt que calculées sur un mauvais
// calendrier.
var ErrInvalidTimezone = errors.New("fiscal: invalid merchant timezone")

// DueRange renvoie les jours de l'établissement à clôturer, dans l'ordre :
//   - jusqu'à la veille — ou l'avant-veille avant 3 h, heure locale ;
//   - depuis le lendemain de sa dernière clôture journalière ;
//   - s'il n'en a aucune : depuis from (rattrapage, cmd/backfill_fiscal_closures)
//     ou, sans from, depuis le dernier jour échu seulement — tout ce qui précède
//     est compté dans la valeur d'ouverture de cette première clôture ;
//   - jamais avant le jour de création de l'établissement.
// ok vaut false s'il n'y a rien à clôturer.
func DueRange(ctx context.Context, db *sql.DB, merchantID, timezone string, from *time.Time, merchantCreated, now time.Time) (first, last time.Time, ok bool, err error) {
	loc, lerr := time.LoadLocation(timezone)
	if lerr != nil || timezone == "" {
		return first, last, false, fmt.Errorf("%w %q: %v", ErrInvalidTimezone, timezone, lerr)
	}
	nowLocal := now.In(loc)
	last = civilDate(nowLocal).AddDate(0, 0, -1)
	if nowLocal.Hour() < closingHour {
		last = last.AddDate(0, 0, -1)
	}

	var lastClosed sql.NullString
	if err := dbx.GetDB(ctx, db).QueryRowContext(ctx, `
		SELECT max(period_start)::text FROM fiscal_closures
		WHERE merchant_id = ? AND period_type = 'DAY'`, merchantID).Scan(&lastClosed); err != nil {
		return first, last, false, fmt.Errorf("fiscal: read last closure: %w", err)
	}
	switch {
	case lastClosed.Valid:
		d, err := time.Parse(dateLayout, lastClosed.String)
		if err != nil {
			return first, last, false, fmt.Errorf("fiscal: parse last closure date: %w", err)
		}
		first = d.AddDate(0, 0, 1)
	case from != nil:
		first = civilDateOf(*from)
	default:
		first = last
	}
	if created := civilDate(merchantCreated.In(loc)); created.After(first) {
		first = created
	}
	return first, last, !first.After(last), nil
}

// CloseDueDays clôture les jours de DueRange, dans l'ordre. Une transaction
// par jour (et ses clôtures de mois et d'année) ; idempotent entre instances
// (verrou de la chaîne et contrôle « déjà clos »). Renvoie le nombre de jours
// clos. from : premier jour d'un rattrapage (nil pour la tâche horaire).
func CloseDueDays(ctx context.Context, db *sql.DB, merchantID, timezone string, from *time.Time, merchantCreated, now time.Time) (int, error) {
	first, last, ok, err := DueRange(ctx, db, merchantID, timezone, from, merchantCreated, now)
	if err != nil || !ok {
		return 0, err
	}
	loc, _ := time.LoadLocation(timezone)

	closed := 0
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		day := d
		err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			done, err := closeDay(txCtx, db, merchantID, timezone, loc, day)
			if done {
				closed++
			}
			return err
		})
		if err != nil {
			return closed, fmt.Errorf("fiscal: close %s for merchant %s: %w", day.Format(dateLayout), merchantID, err)
		}
	}
	return closed, nil
}

// closeDay clôt un jour (et son mois / son année s'il en est le dernier).
// false si une autre instance l'a déjà clos.
func closeDay(ctx context.Context, db *sql.DB, merchantID, timezone string, loc *time.Location, day time.Time) (bool, error) {
	if err := LockChain(ctx, ChainFiscalClosures, merchantID); err != nil {
		return false, err
	}
	d := dbx.GetDB(ctx, db)
	var exists bool
	if err := d.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM fiscal_closures WHERE merchant_id = ? AND period_type = 'DAY' AND period_start = CAST(? AS date))`,
		merchantID, day.Format(dateLayout)).Scan(&exists); err != nil {
		return false, fmt.Errorf("read closure: %w", err)
	}
	if exists {
		return false, nil
	}

	p, err := ComputeDayClosure(ctx, d, merchantID, timezone, loc, day)
	if err != nil {
		return false, err
	}

	// Cumuls : depuis la clôture journalière précédente, ou depuis la valeur
	// d'ouverture pour la première.
	var prevStart sql.NullString
	var prevGrand, prevPerpetual int64
	err = d.QueryRowContext(ctx, `
		SELECT period_start::text, grand_total_period, perpetual_total FROM fiscal_closures
		WHERE merchant_id = ? AND period_type = 'DAY'
		ORDER BY period_start DESC LIMIT 1`, merchantID).Scan(&prevStart, &prevGrand, &prevPerpetual)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		opening, err := computeOpening(ctx, d, merchantID, loc, day)
		if err != nil {
			return false, err
		}
		p.Opening = opening
		p.PerpetualTotal = opening.NetTTC + p.NetTTC
		p.GrandTotalPeriod = opening.YearNetTTC + p.NetTTC
	case err != nil:
		return false, fmt.Errorf("read previous closure: %w", err)
	default:
		p.PerpetualTotal = prevPerpetual + p.NetTTC
		p.GrandTotalPeriod = p.NetTTC
		if strings.HasPrefix(prevStart.String, day.Format("2006")) {
			p.GrandTotalPeriod += prevGrand
		}
	}
	if err := insertClosure(ctx, d, p); err != nil {
		return false, err
	}

	// Dernier jour du mois / de l'année : clôtures mensuelle et annuelle.
	if day.AddDate(0, 0, 1).Day() == 1 {
		monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
		if err := closeAggregate(ctx, d, merchantID, timezone, PeriodMonth, monthStart, day); err != nil {
			return false, err
		}
		if day.Month() == time.December {
			if err := closeAggregate(ctx, d, merchantID, timezone, PeriodYear, time.Date(day.Year(), 1, 1, 0, 0, 0, 0, time.UTC), day); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// ComputeDayClosure calcule le contenu d'une clôture journalière à partir des
// données (sans cumuls ni ouverture). Sert aussi à vérifier une clôture
// existante : une commande modifiée depuis change d'empreinte.
func ComputeDayClosure(ctx context.Context, d *dbx.DB, merchantID, timezone string, loc *time.Location, day time.Time) (ClosurePayload, error) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	p := ClosurePayload{
		MerchantID: merchantID, PeriodType: PeriodDay, Timezone: timezone,
		PeriodStart: day.Format(dateLayout), PeriodEnd: day.Format(dateLayout),
		VATByRate: []VATTotal{}, PaymentsByMOP: []PaymentTotal{}, ByChannel: []ChannelTotal{}, Orders: []OrderFingerprint{},
	}

	// Tickets et avoirs.
	rows, err := d.QueryContext(ctx, `
		SELECT r.receipt_number, r.total_ttc, r.total_ht, r.tax_details::text, COALESCE(o.order_source, '')
		FROM receipts r
		LEFT JOIN orders o ON o.order_id = r.order_id
		WHERE r.merchant_id = ? AND r.created_at >= ? AND r.created_at < ?
		ORDER BY r.created_at, r.receipt_number`, merchantID, from.UTC(), to.UTC())
	if err != nil {
		return p, fmt.Errorf("load day receipts: %w", err)
	}
	vat := map[float64]*VATTotal{}
	channels := map[string]int64{}
	for rows.Next() {
		var number, taxRaw, source string
		var ttc, ht int64
		if err := rows.Scan(&number, &ttc, &ht, &taxRaw, &source); err != nil {
			rows.Close()
			return p, fmt.Errorf("scan day receipt: %w", err)
		}
		if td, ok := ParseTaxDetails([]byte(taxRaw)); ok {
			for _, l := range td.Lines {
				v := vat[l.Rate]
				if v == nil {
					v = &VATTotal{Rate: l.Rate}
					vat[l.Rate] = v
				}
				v.TTC += l.TTC
				v.HT += l.HT
				v.TVA += l.TVA
			}
			ttc, ht = td.TotalTTC(), td.TotalHT()
		}
		if ttc >= 0 {
			p.SalesTTC += ttc
			p.SalesHT += ht
		} else {
			p.RefundsTTC += ttc
			p.RefundsHT += ht
		}
		channels[source] += ttc
		p.ReceiptsCount++
		n := number
		if p.FirstReceiptNumber == nil {
			p.FirstReceiptNumber = &n
		}
		p.LastReceiptNumber = &n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, fmt.Errorf("load day receipts: %w", err)
	}
	p.NetTTC = p.SalesTTC + p.RefundsTTC
	p.NetHT = p.SalesHT + p.RefundsHT
	p.VATByRate = sortedVAT(vat)
	p.ByChannel = sortedChannels(channels)

	// Paiements.
	rows, err = d.QueryContext(ctx, `
		SELECT mop, operation_type, enabled, SUM(amount), COUNT(*)
		FROM payments
		WHERE merchant_id = ? AND payment_date >= ? AND payment_date < ?
		GROUP BY mop, operation_type, enabled
		ORDER BY mop, operation_type, enabled`, merchantID, from.UTC(), to.UTC())
	if err != nil {
		return p, fmt.Errorf("load day payments: %w", err)
	}
	for rows.Next() {
		var t PaymentTotal
		if err := rows.Scan(&t.MOP, &t.OperationType, &t.Enabled, &t.Amount, &t.Count); err != nil {
			rows.Close()
			return p, fmt.Errorf("scan day payment: %w", err)
		}
		p.PaymentsByMOP = append(p.PaymentsByMOP, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, fmt.Errorf("load day payments: %w", err)
	}

	// Commandes closes ce jour-là : empreinte de chacune.
	orders, err := loadDayOrderClosures(ctx, d, merchantID, from, to)
	if err != nil {
		return p, err
	}
	for _, o := range orders {
		h, err := Fingerprint("order_closure", o.payload)
		if err != nil {
			return p, err
		}
		p.Orders = append(p.Orders, OrderFingerprint{OrderID: o.payload.OrderID, Status: o.status, Hash: h})
	}
	p.OrdersCount = int64(len(p.Orders))
	return p, nil
}

type dayOrder struct {
	status  string
	payload OrderClosurePayload
}

// loadDayOrderClosures charge en une requête les commandes closes sur
// [from, to[ et leurs lignes, sous la forme exacte de LoadOrderClosure (même
// empreinte, vérifié par les tests).
func loadDayOrderClosures(ctx context.Context, d *dbx.DB, merchantID string, from, to time.Time) ([]dayOrder, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT o.merchant_id, o.order_id, o.order_type, o.price, o.ht, o.tva, o.delivery_fees, o.cart_discount_amount,
		       o.delivered_on, upper(o.brand_status),
		       oi.order_item_id, oi.product_id, oi.quantity, oi.base_price, oi.price, oi.tva_rate, oi.discount_id
		FROM orders o
		LEFT JOIN orderitems oi ON oi.order_id = o.order_id
		WHERE o.merchant_id = ? AND o.state = 'CLOSED' AND o.delivered_on >= ? AND o.delivered_on < ?
		ORDER BY o.order_id, oi.order_item_id`, merchantID, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("load day orders: %w", err)
	}
	defer rows.Close()
	var out []dayOrder
	for rows.Next() {
		var (
			p                                      OrderClosurePayload
			orderType                              sql.NullString
			deliveredOn                            time.Time
			status                                 string
			itemID, productID, quantity, linePrice sql.NullInt64
			basePrice, discountID                  sql.NullInt64
			tvaRate                                sql.NullFloat64
		)
		if err := rows.Scan(&p.MerchantID, &p.OrderID, &orderType, &p.Price, &p.HT, &p.TVA, &p.DeliveryFees, &p.CartDiscountAmount,
			&deliveredOn, &status, &itemID, &productID, &quantity, &basePrice, &linePrice, &tvaRate, &discountID); err != nil {
			return nil, fmt.Errorf("scan day order: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].payload.OrderID != p.OrderID {
			if orderType.Valid {
				p.OrderType = &orderType.String
			}
			p.DeliveredOn = FormatTime(deliveredOn)
			p.Lines = []OrderLinePayload{}
			out = append(out, dayOrder{status: status, payload: p})
		}
		if !itemID.Valid {
			continue
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
		cur := &out[len(out)-1].payload
		cur.Lines = append(cur.Lines, l)
	}
	return out, rows.Err()
}

// computeOpening : tickets de l'établissement antérieurs à son premier jour
// clôturé (décision d'Ilies : source « tickets »). Un ticket sans ventilation
// (antérieur au lot B) compte pour son total.
func computeOpening(ctx context.Context, d *dbx.DB, merchantID string, loc *time.Location, day time.Time) (*ClosureOpening, error) {
	until := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	yearStart := time.Date(day.Year(), 1, 1, 0, 0, 0, 0, loc)
	rows, err := d.QueryContext(ctx, `
		SELECT created_at, total_ttc, tax_details::text FROM receipts
		WHERE merchant_id = ? AND created_at < ?`, merchantID, until.UTC())
	if err != nil {
		return nil, fmt.Errorf("load opening receipts: %w", err)
	}
	defer rows.Close()
	o := &ClosureOpening{Method: "receipts_before", Until: FormatTime(until)}
	for rows.Next() {
		var at time.Time
		var ttc int64
		var taxRaw string
		if err := rows.Scan(&at, &ttc, &taxRaw); err != nil {
			return nil, fmt.Errorf("scan opening receipt: %w", err)
		}
		if td, ok := ParseTaxDetails([]byte(taxRaw)); ok {
			ttc = td.TotalTTC()
		}
		o.ReceiptsCount++
		o.NetTTC += ttc
		if !at.Before(yearStart) {
			o.YearNetTTC += ttc
		}
	}
	return o, rows.Err()
}

// closeAggregate scelle une clôture de mois ou d'année : somme des journées
// closes de [start, end], cumuls de la dernière journée.
func closeAggregate(ctx context.Context, d *dbx.DB, merchantID, timezone, periodType string, start, end time.Time) error {
	rows, err := d.QueryContext(ctx, `
		SELECT sales_ttc, sales_ht, refunds_ttc, refunds_ht, net_ttc, net_ht,
		       vat_by_rate::text, payments_by_mop::text, by_channel::text,
		       receipts_count, first_receipt_number, last_receipt_number, orders_count,
		       grand_total_period, perpetual_total
		FROM fiscal_closures
		WHERE merchant_id = ? AND period_type = 'DAY'
		  AND period_start >= CAST(? AS date) AND period_start <= CAST(? AS date)
		ORDER BY period_start`, merchantID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return fmt.Errorf("load %s days: %w", periodType, err)
	}
	defer rows.Close()
	p := ClosurePayload{
		MerchantID: merchantID, PeriodType: periodType, Timezone: timezone,
		PeriodStart: start.Format(dateLayout), PeriodEnd: end.Format(dateLayout),
	}
	vat := map[float64]*VATTotal{}
	pays := map[string]*PaymentTotal{}
	channels := map[string]int64{}
	for rows.Next() {
		var vatRaw, payRaw, chRaw string
		var first, last sql.NullString
		var receipts, orders, grand, perpetual int64
		var s ClosurePayload
		if err := rows.Scan(&s.SalesTTC, &s.SalesHT, &s.RefundsTTC, &s.RefundsHT, &s.NetTTC, &s.NetHT,
			&vatRaw, &payRaw, &chRaw, &receipts, &first, &last, &orders, &grand, &perpetual); err != nil {
			return fmt.Errorf("scan %s day: %w", periodType, err)
		}
		p.SalesTTC += s.SalesTTC
		p.SalesHT += s.SalesHT
		p.RefundsTTC += s.RefundsTTC
		p.RefundsHT += s.RefundsHT
		p.NetTTC += s.NetTTC
		p.NetHT += s.NetHT
		p.ReceiptsCount += receipts
		p.OrdersCount += orders
		if first.Valid && p.FirstReceiptNumber == nil {
			f := first.String
			p.FirstReceiptNumber = &f
		}
		if last.Valid {
			l := last.String
			p.LastReceiptNumber = &l
		}
		p.GrandTotalPeriod, p.PerpetualTotal = grand, perpetual

		var dayVAT []VATTotal
		var dayPays []PaymentTotal
		var dayCh []ChannelTotal
		if err := json.Unmarshal([]byte(vatRaw), &dayVAT); err != nil {
			return fmt.Errorf("decode day vat: %w", err)
		}
		if err := json.Unmarshal([]byte(payRaw), &dayPays); err != nil {
			return fmt.Errorf("decode day payments: %w", err)
		}
		if err := json.Unmarshal([]byte(chRaw), &dayCh); err != nil {
			return fmt.Errorf("decode day channels: %w", err)
		}
		for _, v := range dayVAT {
			t := vat[v.Rate]
			if t == nil {
				t = &VATTotal{Rate: v.Rate}
				vat[v.Rate] = t
			}
			t.TTC += v.TTC
			t.HT += v.HT
			t.TVA += v.TVA
		}
		for _, pt := range dayPays {
			key := fmt.Sprintf("%s|%s|%t", pt.MOP, pt.OperationType, pt.Enabled)
			t := pays[key]
			if t == nil {
				t = &PaymentTotal{MOP: pt.MOP, OperationType: pt.OperationType, Enabled: pt.Enabled}
				pays[key] = t
			}
			t.Amount += pt.Amount
			t.Count += pt.Count
		}
		for _, c := range dayCh {
			channels[c.Source] += c.NetTTC
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load %s days: %w", periodType, err)
	}
	p.VATByRate = sortedVAT(vat)
	p.ByChannel = sortedChannels(channels)
	p.PaymentsByMOP = make([]PaymentTotal, 0, len(pays))
	for _, t := range pays {
		p.PaymentsByMOP = append(p.PaymentsByMOP, *t)
	}
	sort.Slice(p.PaymentsByMOP, func(i, j int) bool {
		a, b := p.PaymentsByMOP[i], p.PaymentsByMOP[j]
		if a.MOP != b.MOP {
			return a.MOP < b.MOP
		}
		if a.OperationType != b.OperationType {
			return a.OperationType < b.OperationType
		}
		return !a.Enabled && b.Enabled
	})
	return insertClosure(ctx, d, p)
}

// insertClosure scelle la clôture (chaînée sur la dernière de l'établissement)
// et l'écrit. L'appelant tient le verrou de la chaîne.
func insertClosure(ctx context.Context, d *dbx.DB, p ClosurePayload) error {
	var prevHash sql.NullString
	if err := d.QueryRowContext(ctx, `
		SELECT hash FROM fiscal_closures WHERE merchant_id = ?
		ORDER BY closed_at DESC, id DESC LIMIT 1`, p.MerchantID).Scan(&prevHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read previous closure hash: %w", err)
	}
	prev := PrevOrGenesis(prevHash.String)
	closedAt := Now()
	p.ClosedAt = FormatTime(closedAt)
	hash, signature, err := Seal(ChainFiscalClosures, prev, p)
	if err != nil {
		return err
	}

	vatJSON, _ := json.Marshal(p.VATByRate)
	payJSON, _ := json.Marshal(p.PaymentsByMOP)
	chJSON, _ := json.Marshal(p.ByChannel)
	var ordersJSON, openingJSON []byte
	if p.Orders != nil {
		ordersJSON, _ = json.Marshal(p.Orders)
	}
	if p.Opening != nil {
		openingJSON, _ = json.Marshal(p.Opening)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO fiscal_closures
		(merchant_id, period_type, period_start, period_end, timezone, closed_at,
		 sales_ttc, sales_ht, refunds_ttc, refunds_ht, net_ttc, net_ht,
		 vat_by_rate, payments_by_mop, by_channel,
		 receipts_count, first_receipt_number, last_receipt_number, orders_count, orders, opening,
		 grand_total_period, perpetual_total, previous_hash, hash, signature, hash_version)
		VALUES (?, ?, CAST(? AS date), CAST(? AS date), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.MerchantID, p.PeriodType, p.PeriodStart, p.PeriodEnd, p.Timezone, closedAt,
		p.SalesTTC, p.SalesHT, p.RefundsTTC, p.RefundsHT, p.NetTTC, p.NetHT,
		vatJSON, payJSON, chJSON,
		p.ReceiptsCount, p.FirstReceiptNumber, p.LastReceiptNumber, p.OrdersCount, nullJSON(ordersJSON), nullJSON(openingJSON),
		p.GrandTotalPeriod, p.PerpetualTotal, prev, hash, signature, HashVersion); err != nil {
		return fmt.Errorf("insert %s closure: %w", strings.ToLower(p.PeriodType), err)
	}
	return nil
}

// IsOrderSealed indique si la commande est scellée : une clôture journalière
// existe pour la date locale de sa clôture (lot C : réouverture refusée).
func IsOrderSealed(ctx context.Context, d *dbx.DB, orderID string) (bool, error) {
	var sealed bool
	err := d.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders o
			JOIN merchant m ON m.id::text = o.merchant_id
			JOIN fiscal_closures fc ON fc.merchant_id = o.merchant_id AND fc.period_type = 'DAY'
			 AND fc.period_start = (o.delivered_on AT TIME ZONE m.timezone)::date
			WHERE o.order_id = ? AND o.state = 'CLOSED' AND o.delivered_on IS NOT NULL)`, orderID).Scan(&sealed)
	if err != nil {
		return false, fmt.Errorf("fiscal: read order seal: %w", err)
	}
	return sealed, nil
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return b
}

func sortedVAT(m map[float64]*VATTotal) []VATTotal {
	out := make([]VATTotal, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rate < out[j].Rate })
	return out
}

func sortedChannels(m map[string]int64) []ChannelTotal {
	out := make([]ChannelTotal, 0, len(m))
	for s, v := range m {
		out = append(out, ChannelTotal{Source: s, NetTTC: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// civilDate ramène une date locale à minuit UTC de la même date calendaire
// (arithmétique de jours sans effet des changements d'heure).
func civilDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func civilDateOf(t time.Time) time.Time { return civilDate(t) }

// StoredClosure est une clôture relue en base : sa charge utile et son
// scellement.
type StoredClosure struct {
	ID        int64
	Payload   ClosurePayload
	Prev      string
	Hash      string
	Signature string
	Version   int
}

// LoadClosure relit une clôture sous la forme exacte où elle a été scellée
// (Seal(ChainFiscalClosures, Prev, Payload) doit redonner Hash) — pour les
// tests et la commande de vérification.
func LoadClosure(ctx context.Context, d *dbx.DB, id int64) (StoredClosure, error) {
	var (
		s                         StoredClosure
		p                         ClosurePayload
		closedAt                  time.Time
		vatRaw, payRaw, chRaw     string
		ordersRaw, openingRaw     sql.NullString
		firstReceipt, lastReceipt sql.NullString
	)
	err := d.QueryRowContext(ctx, `
		SELECT id, merchant_id, period_type, period_start::text, period_end::text, timezone, closed_at,
		       sales_ttc, sales_ht, refunds_ttc, refunds_ht, net_ttc, net_ht,
		       vat_by_rate::text, payments_by_mop::text, by_channel::text,
		       receipts_count, first_receipt_number, last_receipt_number, orders_count, orders::text, opening::text,
		       grand_total_period, perpetual_total, previous_hash, hash, signature, hash_version
		FROM fiscal_closures WHERE id = ?`, id).Scan(
		&s.ID, &p.MerchantID, &p.PeriodType, &p.PeriodStart, &p.PeriodEnd, &p.Timezone, &closedAt,
		&p.SalesTTC, &p.SalesHT, &p.RefundsTTC, &p.RefundsHT, &p.NetTTC, &p.NetHT,
		&vatRaw, &payRaw, &chRaw,
		&p.ReceiptsCount, &firstReceipt, &lastReceipt, &p.OrdersCount, &ordersRaw, &openingRaw,
		&p.GrandTotalPeriod, &p.PerpetualTotal, &s.Prev, &s.Hash, &s.Signature, &s.Version)
	if err != nil {
		return s, fmt.Errorf("fiscal: load closure %d: %w", id, err)
	}
	p.ClosedAt = FormatTime(closedAt)
	if firstReceipt.Valid {
		p.FirstReceiptNumber = &firstReceipt.String
	}
	if lastReceipt.Valid {
		p.LastReceiptNumber = &lastReceipt.String
	}
	for _, j := range []struct {
		raw string
		dst any
	}{{vatRaw, &p.VATByRate}, {payRaw, &p.PaymentsByMOP}, {chRaw, &p.ByChannel}} {
		if err := json.Unmarshal([]byte(j.raw), j.dst); err != nil {
			return s, fmt.Errorf("fiscal: decode closure %d: %w", id, err)
		}
	}
	if ordersRaw.Valid {
		if err := json.Unmarshal([]byte(ordersRaw.String), &p.Orders); err != nil {
			return s, fmt.Errorf("fiscal: decode closure %d orders: %w", id, err)
		}
	}
	if openingRaw.Valid {
		p.Opening = &ClosureOpening{}
		if err := json.Unmarshal([]byte(openingRaw.String), p.Opening); err != nil {
			return s, fmt.Errorf("fiscal: decode closure %d opening: %w", id, err)
		}
	}
	s.Payload = p
	return s, nil
}

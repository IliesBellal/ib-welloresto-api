package fiscalverify

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/fiscal"
)

type dayRef struct {
	id  int64
	day time.Time // date civile, minuit UTC
}

// dayClosures recalcule chaque clôture journalière de la période depuis les
// données et la compare à la clôture scellée :
//   - tickets et avoirs du jour (ventes, avoirs, TVA, canaux, nombre,
//     premier et dernier numéro) : erreur si différents ;
//   - commandes closes du jour et leur empreinte : erreur si différentes
//     (commande modifiée après son scellement) ;
//   - paiements du jour : avertissement si seul l'état actif / annulé a changé
//     (paiement d'une commande encore ouverte, annulé après la clôture de son
//     jour, tracé au journal), erreur sinon ;
//   - cumuls : total perpétuel et grand total de l'année, depuis la clôture
//     précédente ou la valeur d'ouverture recalculée ;
//
// et contrôle qu'aucun jour ne manque entre la première clôture et
// l'avant-veille.
func (v *verifier) dayClosures(ctx context.Context) error {
	c := v.r.check("clotures_jour", "Clôtures journalières recalculées depuis les données")
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, period_start::text FROM fiscal_closures
		WHERE merchant_id = ? AND period_type = 'DAY' ORDER BY period_start`, v.merchant)
	if err != nil {
		return fmt.Errorf("fiscalverify: day closures: %w", err)
	}
	var days []dayRef
	for rows.Next() {
		var d dayRef
		var start string
		if err := rows.Scan(&d.id, &start); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: day closures: %w", err)
		}
		if d.day, err = time.Parse("2006-01-02", start); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: day closures: %w", err)
		}
		days = append(days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: day closures: %w", err)
	}

	inPeriod := func(d time.Time) bool {
		s := d.Format("2006-01-02")
		return (v.fromDay == "" || s >= v.fromDay) && s <= v.toDay
	}
	// Jours manquants : entre deux clôtures, et après la dernière jusqu'à
	// l'avant-veille (la veille se clôt après 3 h, heure locale).
	now := v.r.GeneratedAt.In(v.loc)
	expectedLast := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2)
	if to, _ := time.Parse("2006-01-02", v.toDay); to.Before(expectedLast) {
		expectedLast = to
	}
	missing := func(from, to time.Time) {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if inPeriod(d) {
				end := to
				v.r.add(SeverityError, c.Check, "jours sans clôture journalière du %s au %s", d.Format("2006-01-02"), end.Format("2006-01-02"))
				return
			}
		}
	}
	if len(days) == 0 {
		if v.attested != nil {
			v.r.add(SeverityError, c.Check, "aucune clôture journalière pour cet établissement")
		}
		return nil
	}
	for i := 1; i < len(days); i++ {
		if next := days[i-1].day.AddDate(0, 0, 1); days[i].day.After(next) {
			missing(next, days[i].day.AddDate(0, 0, -1))
		}
	}
	if last := days[len(days)-1].day; last.Before(expectedLast) {
		missing(last.AddDate(0, 0, 1), expectedLast)
	}

	var prev *fiscal.ClosurePayload
	for i, d := range days {
		if !inPeriod(d.day) {
			continue
		}
		if prev == nil && i > 0 {
			sc, err := fiscal.LoadClosure(ctx, v.db, days[i-1].id)
			if err != nil {
				return err
			}
			prev = &sc.Payload
		}
		sc, err := fiscal.LoadClosure(ctx, v.db, d.id)
		if err != nil {
			return err
		}
		stored := sc.Payload
		label := "clôture du " + stored.PeriodStart
		loc, err := time.LoadLocation(stored.Timezone)
		if err != nil {
			v.r.add(SeverityError, c.Check, "%s : fuseau illisible %q", label, stored.Timezone)
			continue
		}
		got, err := fiscal.ComputeDayClosure(ctx, v.db, v.merchant, stored.Timezone, loc, d.day)
		if err != nil {
			return err
		}
		c.Checked++
		if a, b := receiptsPart(stored), receiptsPart(got); a != b {
			v.r.add(SeverityError, c.Check, "%s : tickets du jour différents de la clôture (scellé %s, recalculé %s)", label, a, b)
		}
		if msg := ordersDiff(stored.Orders, got.Orders); msg != "" {
			v.r.add(SeverityError, c.Check, "%s : %s", label, msg)
		}
		if a, b := jsonOf(stored.PaymentsByMOP), jsonOf(got.PaymentsByMOP); a != b {
			if paymentsIgnoringState(stored.PaymentsByMOP) == paymentsIgnoringState(got.PaymentsByMOP) {
				v.r.add(SeverityWarning, c.Check, "%s : paiement du jour annulé après la clôture (commande encore ouverte à la clôture ; annulation tracée au journal)", label)
			} else {
				v.r.add(SeverityError, c.Check, "%s : paiements du jour différents de la clôture (scellé %s, recalculé %s)", label, a, b)
			}
		}
		// Cumuls.
		wantPerpetual, wantGrand := stored.NetTTC, stored.NetTTC
		if prev == nil {
			opening, err := fiscal.ComputeOpening(ctx, v.db, v.merchant, loc, d.day)
			if err != nil {
				return err
			}
			if stored.Opening == nil || *stored.Opening != *opening {
				v.r.add(SeverityError, c.Check, "%s : valeur d'ouverture %s, recalculée %s", label, jsonOf(stored.Opening), jsonOf(opening))
			} else {
				wantPerpetual += opening.NetTTC
				wantGrand += opening.YearNetTTC
			}
		} else {
			wantPerpetual += prev.PerpetualTotal
			if strings.HasPrefix(prev.PeriodStart, stored.PeriodStart[:4]) {
				wantGrand += prev.GrandTotalPeriod
			}
		}
		if stored.PerpetualTotal != wantPerpetual || stored.GrandTotalPeriod != wantGrand {
			v.r.add(SeverityError, c.Check, "%s : cumuls %d / %d (perpétuel / année), attendus %d / %d",
				label, stored.PerpetualTotal, stored.GrandTotalPeriod, wantPerpetual, wantGrand)
		}
		prev = &stored
	}
	return nil
}

// aggregateClosures recalcule chaque clôture mensuelle et annuelle de la
// période depuis ses jours, et contrôle qu'un mois (une année) dont le
// dernier jour est clos a sa clôture.
func (v *verifier) aggregateClosures(ctx context.Context) error {
	c := v.r.check("clotures_mois_annee", "Clôtures mensuelles et annuelles égales à la somme de leurs jours")
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, period_type, period_start::text, period_end::text FROM fiscal_closures
		WHERE merchant_id = ? AND period_type IN ('MONTH', 'YEAR')
		  AND period_end >= CAST(? AS date) AND period_end <= CAST(? AS date)
		ORDER BY period_end, period_type`, v.merchant, firstNonEmpty(v.fromDay, "1970-01-01"), v.toDay)
	if err != nil {
		return fmt.Errorf("fiscalverify: aggregate closures: %w", err)
	}
	type agg struct {
		id              int64
		typ, start, end string
	}
	var aggs []agg
	have := map[string]bool{}
	for rows.Next() {
		var a agg
		if err := rows.Scan(&a.id, &a.typ, &a.start, &a.end); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: aggregate closures: %w", err)
		}
		aggs = append(aggs, a)
		have[a.typ+" "+a.end] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: aggregate closures: %w", err)
	}
	for _, a := range aggs {
		sc, err := fiscal.LoadClosure(ctx, v.db, a.id)
		if err != nil {
			return err
		}
		start, _ := time.Parse("2006-01-02", a.start)
		end, _ := time.Parse("2006-01-02", a.end)
		got, err := fiscal.ComputeAggregateClosure(ctx, v.db, v.merchant, sc.Payload.Timezone, a.typ, start, end)
		if err != nil {
			return err
		}
		got.ClosedAt = sc.Payload.ClosedAt
		c.Checked++
		if x, y := jsonOf(sc.Payload), jsonOf(got); x != y {
			v.r.add(SeverityError, c.Check, "clôture %s %s → %s : différente de la somme de ses jours", a.typ, a.start, a.end)
		}
	}

	// Mois et années échus sans clôture.
	rows, err = v.db.QueryContext(ctx, `
		SELECT period_start::text FROM fiscal_closures
		WHERE merchant_id = ? AND period_type = 'DAY'
		  AND period_start >= CAST(? AS date) AND period_start <= CAST(? AS date)
		  AND extract(day FROM period_start + 1) = 1
		ORDER BY period_start`, v.merchant, firstNonEmpty(v.fromDay, "1970-01-01"), v.toDay)
	if err != nil {
		return fmt.Errorf("fiscalverify: month ends: %w", err)
	}
	var ends []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: month ends: %w", err)
		}
		ends = append(ends, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: month ends: %w", err)
	}
	for _, e := range ends {
		if !have["MONTH "+e] {
			v.r.add(SeverityError, c.Check, "mois clos le %s sans clôture mensuelle", e)
		}
		if strings.HasSuffix(e, "-12-31") && !have["YEAR "+e] {
			v.r.add(SeverityError, c.Check, "année close le %s sans clôture annuelle", e)
		}
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func jsonOf(x any) string {
	b, err := json.Marshal(x)
	if err != nil {
		return fmt.Sprintf("%v", x)
	}
	return string(b)
}

// receiptsPart : la partie d'une clôture journalière calculée depuis les
// tickets.
func receiptsPart(p fiscal.ClosurePayload) string {
	return jsonOf(struct {
		SalesTTC, SalesHT, RefundsTTC, RefundsHT, NetTTC, NetHT int64
		VAT                                                     []fiscal.VATTotal
		Channels                                                []fiscal.ChannelTotal
		Count                                                   int64
		First, Last                                             *string
	}{p.SalesTTC, p.SalesHT, p.RefundsTTC, p.RefundsHT, p.NetTTC, p.NetHT, p.VATByRate, p.ByChannel,
		p.ReceiptsCount, p.FirstReceiptNumber, p.LastReceiptNumber})
}

// paymentsIgnoringState : totaux par moyen et type, actifs et annulés
// confondus.
func paymentsIgnoringState(ps []fiscal.PaymentTotal) string {
	m := map[string][2]int64{}
	for _, p := range ps {
		k := p.MOP + "|" + p.OperationType
		t := m[k]
		t[0] += p.Amount
		t[1] += p.Count
		m[k] = t
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%d/%d;", k, m[k][0], m[k][1])
	}
	return b.String()
}

// ordersDiff décrit l'écart entre les commandes scellées d'un jour et celles
// recalculées (au plus cinq commandes citées).
func ordersDiff(stored, got []fiscal.OrderFingerprint) string {
	byID := map[int64]fiscal.OrderFingerprint{}
	for _, o := range got {
		byID[o.OrderID] = o
	}
	var changed, missing []string
	for _, o := range stored {
		g, ok := byID[o.OrderID]
		delete(byID, o.OrderID)
		switch {
		case !ok:
			missing = append(missing, strconv.FormatInt(o.OrderID, 10))
		case g.Hash != o.Hash || g.Status != o.Status:
			changed = append(changed, strconv.FormatInt(o.OrderID, 10))
		}
	}
	var extra []string
	for id := range byID {
		extra = append(extra, strconv.FormatInt(id, 10))
	}
	sort.Strings(extra)
	var parts []string
	if len(changed) > 0 {
		parts = append(parts, "commande(s) modifiée(s) après scellement : "+cut(changed))
	}
	if len(missing) > 0 {
		parts = append(parts, "commande(s) scellée(s) qui ne sont plus closes ce jour : "+cut(missing))
	}
	if len(extra) > 0 {
		parts = append(parts, "commande(s) close(s) ce jour absentes de la clôture : "+cut(extra))
	}
	return strings.Join(parts, " ; ")
}

func cut(ids []string) string {
	if len(ids) > 5 {
		return strings.Join(ids[:5], ", ") + fmt.Sprintf(" (+%d)", len(ids)-5)
	}
	return strings.Join(ids, ", ")
}

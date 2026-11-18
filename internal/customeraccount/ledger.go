package customeraccount

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// Grand livre des comptes clients d'un établissement : tous les mouvements
// et toutes les commandes réglées sur compte, pour la balance âgée, les totaux
// mensuels et les relances. Calculs sans base, testés unitairement.

// Natures d'une ligne du grand livre.
const (
	KindDeposit     = "deposit"
	KindRefund      = "refund"
	KindWriteOff    = "write_off"
	KindConsumption = "consumption" // commande réglée sur compte (ou remboursée dessus)
)

// LedgerEntry : effet d'une ligne sur le solde d'un client.
type LedgerEntry struct {
	CustomerID int64
	Date       time.Time
	Delta      int64
	Kind       string
}

// LoadLedger lit le grand livre de l'établissement (customerID > 0 : un seul
// client), trié par date.
func LoadLedger(ctx context.Context, db *dbx.DB, merchantID string, customerID int64) ([]LedgerEntry, error) {
	filter, args := "", []any{merchantID}
	if customerID > 0 {
		filter = " AND customer_id = ?"
		args = append(args, customerID)
	}
	args2 := append([]any{}, args...)
	rows, err := db.QueryContext(ctx, `
		SELECT customer_id, operation_date, amount, operation_type FROM customer_account_operations
		WHERE merchant_id = ?`+filter+`
		UNION ALL
		SELECT customer_id, payment_date, -amount, 'CONSUMPTION' FROM payments
		WHERE merchant_id = ? AND mop = 'ACCOUNT' AND enabled = TRUE AND customer_id IS NOT NULL`+filter,
		append(args, args2...)...)
	if err != nil {
		return nil, fmt.Errorf("customeraccount: ledger: %w", err)
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		var kind string
		if err := rows.Scan(&e.CustomerID, &e.Date, &e.Delta, &kind); err != nil {
			return nil, fmt.Errorf("customeraccount: ledger: %w", err)
		}
		switch kind {
		case models.AccountOperationDeposit:
			e.Kind = KindDeposit
		case models.AccountOperationRefund:
			e.Kind = KindRefund
		case models.AccountOperationWriteOff:
			e.Kind = KindWriteOff
		default:
			e.Kind = KindConsumption
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortLedger(out)
	return out, nil
}

func sortLedger(entries []LedgerEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Date.Before(entries[j].Date) })
}

// openDebt : une dette encore due, datée de la commande qui l'a créée.
type openDebt struct {
	date   time.Time
	amount int64
}

// openDebts rejoue les lignes d'un client (triées) : chaque crédit solde
// d'abord les dettes les plus anciennes ; un crédit sans dette à solder
// devient une avance, qui couvre les dettes suivantes.
func openDebts(entries []LedgerEntry) []openDebt {
	var debts []openDebt
	var credit int64
	for _, e := range entries {
		if e.Delta < 0 {
			due := -e.Delta
			used := min(credit, due)
			credit -= used
			if due -= used; due > 0 {
				debts = append(debts, openDebt{date: e.Date, amount: due})
			}
			continue
		}
		c := e.Delta
		for c > 0 && len(debts) > 0 {
			paid := min(c, debts[0].amount)
			debts[0].amount -= paid
			c -= paid
			if debts[0].amount == 0 {
				debts = debts[1:]
			}
		}
		credit += c
	}
	return debts
}

// byCustomer regroupe les lignes par client, chacune triée.
func byCustomer(entries []LedgerEntry) map[int64][]LedgerEntry {
	out := map[int64][]LedgerEntry{}
	for _, e := range entries {
		out[e.CustomerID] = append(out[e.CustomerID], e)
	}
	for id := range out {
		sortLedger(out[id])
	}
	return out
}

// AgeDebts calcule l'ancienneté de l'ardoise de chaque client endetté,
// dettes les plus anciennes d'abord.
func AgeDebts(entries []LedgerEntry, now time.Time) []models.CustomerAccountAging {
	var out []models.CustomerAccountAging
	for id, list := range byCustomer(entries) {
		debts := openDebts(list)
		if len(debts) == 0 {
			continue
		}
		a := models.CustomerAccountAging{CustomerSummary: models.CustomerAccountSummary{CustomerID: id}}
		a.OldestUnpaid = debts[0].date.Unix()
		for _, d := range debts {
			a.Debt += d.amount
			switch days := int(now.Sub(d.date).Hours() / 24); {
			case days <= 30:
				a.Days0To30 += d.amount
			case days <= 60:
				a.Days31To60 += d.amount
			default:
				a.Days61Plus += d.amount
			}
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OldestUnpaid != out[j].OldestUnpaid {
			return out[i].OldestUnpaid < out[j].OldestUnpaid
		}
		return out[i].CustomerSummary.CustomerID < out[j].CustomerSummary.CustomerID
	})
	return out
}

// OldestUnpaid renvoie la date de la plus ancienne dette non soldée d'un
// client (ok=false sans dette).
func OldestUnpaid(entries []LedgerEntry) (time.Time, bool) {
	list := append([]LedgerEntry(nil), entries...)
	sortLedger(list)
	debts := openDebts(list)
	if len(debts) == 0 {
		return time.Time{}, false
	}
	return debts[0].date, true
}

// MonthlyTotals calcule, pour chaque mois de year (calendrier de
// l'établissement), les mouvements et les soldes de fin de mois.
func MonthlyTotals(entries []LedgerEntry, year int, loc *time.Location) []models.CustomerAccountMonth {
	sorted := append([]LedgerEntry(nil), entries...)
	sortLedger(sorted)
	balances := map[int64]int64{}
	months := make([]models.CustomerAccountMonth, 0, 12)
	i := 0
	for m := time.January; m <= time.December; m++ {
		start := time.Date(year, m, 1, 0, 0, 0, 0, loc)
		end := start.AddDate(0, 1, 0)
		month := models.CustomerAccountMonth{Month: start.Format("2006-01")}
		for ; i < len(sorted) && sorted[i].Date.Before(end); i++ {
			e := sorted[i]
			balances[e.CustomerID] += e.Delta
			if e.Date.Before(start) {
				continue // mouvement antérieur à l'année : solde seulement
			}
			month.NetChange += e.Delta
			switch e.Kind {
			case KindDeposit:
				month.Deposits += e.Delta
			case KindRefund:
				month.Refunds -= e.Delta
			case KindWriteOff:
				month.WriteOffs += e.Delta
			case KindConsumption:
				month.Consumed -= e.Delta
			}
		}
		for _, b := range balances {
			if b > 0 {
				month.EndCredit += b
			} else {
				month.EndDebt -= b
			}
		}
		months = append(months, month)
	}
	return months
}

// LimitAlert : client endetté proche (≥ 80 %) ou au-delà de son plafond.
type LimitAlert struct {
	CustomerID int64
	Debt       int64
	Limit      int64
	Over       bool
}

// nearLimitRatio : seuil d'alerte (80 % du plafond).
const nearLimitRatio = 0.8

// LimitAlerts liste les clients endettés proches de leur plafond ou au-delà.
func LimitAlerts(ctx context.Context, db *dbx.DB, merchantID string) ([]LimitAlert, error) {
	merchantLimit, err := CreditLimit(ctx, db, merchantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT b.customer_id, b.balance, s.credit_limit, COALESCE(s.credit_blocked, FALSE)
		FROM (`+BalancesSQL+`) b
		LEFT JOIN customer_account_settings s ON s.merchant_id = ? AND s.customer_id = b.customer_id
		WHERE b.balance < 0`, merchantID, merchantID, merchantID)
	if err != nil {
		return nil, fmt.Errorf("customeraccount: limit alerts: %w", err)
	}
	defer rows.Close()
	var out []LimitAlert
	for rows.Next() {
		var id, balance int64
		var limit sql.NullInt64
		var blocked bool
		if err := rows.Scan(&id, &balance, &limit, &blocked); err != nil {
			return nil, fmt.Errorf("customeraccount: limit alerts: %w", err)
		}
		cs := models.CustomerAccountCustomerSettings{CreditBlocked: blocked}
		if limit.Valid {
			v := limit.Int64
			cs.CreditLimit = &v
		}
		if a, ok := limitAlert(id, -balance, EffectiveCreditLimit(merchantLimit, cs)); ok {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// limitAlert : alerte pour une dette face à un plafond (aucune sans plafond
// et sans dette au-delà).
func limitAlert(customerID, debt, limit int64) (LimitAlert, bool) {
	a := LimitAlert{CustomerID: customerID, Debt: debt, Limit: limit}
	if debt > limit {
		a.Over = true
		return a, true
	}
	if limit > 0 && float64(debt) >= nearLimitRatio*float64(limit) {
		return a, true
	}
	return a, false
}

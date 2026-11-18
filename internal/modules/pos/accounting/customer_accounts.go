package accounting

import (
	"context"
	"fmt"
	"sort"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"

	"github.com/jung-kurt/gofpdf"
)

// Compte client dans le rapport comptable (docs/customer-accounts.md).
//
// Une commande réglée sur le compte est une vente : elle est dans la TVA (au
// jour de la commande), mais aucun argent n'entre. Un versement est de
// l'argent encaissé, hors chiffre d'affaires et hors TVA (bon à usages
// multiples). D'où un écart entre la TVA et les encaissements réels, égal à
// la variation des comptes clients sur la période — ce bloc le justifie.

// AccountSummary : mouvements des comptes clients d'une période.
type AccountSummary struct {
	// Consumed : commandes réglées sur le compte, nettes des remboursements de
	// commande sur le compte (centimes).
	Consumed int64
	// Deposits / Refunds : argent versé / rendu, par moyen (Refunds en valeur
	// absolue).
	Deposits []PaymentRow
	Refunds  []PaymentRow
	// WriteOffs : dettes abandonnées.
	WriteOffs int64
}

func (a AccountSummary) isEmpty() bool {
	return a.Consumed == 0 && len(a.Deposits) == 0 && len(a.Refunds) == 0 && a.WriteOffs == 0
}

func sumRows(rows []PaymentRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.Amount
	}
	return total
}

// NetChange : variation du total des soldes clients sur la période (> 0 : les
// clients ont plus d'avance ou moins de dette qu'au début).
func (a AccountSummary) NetChange() int64 {
	return sumRows(a.Deposits) - sumRows(a.Refunds) + a.WriteOffs - a.Consumed
}

// buildAccountSummary : Consumed vient des paiements des commandes du
// périmètre du rapport (mêmes commandes que la TVA), le reste des mouvements
// datés de la période.
func buildAccountSummary(orderPayments []orderPaymentLine, movements []accountMovement) AccountSummary {
	var s AccountSummary
	for _, p := range orderPayments {
		if p.MOP == models.AccountMOP {
			s.Consumed += p.Amount
		}
	}
	deposits, refunds := map[string]int64{}, map[string]int64{}
	for _, m := range movements {
		switch m.Type {
		case models.AccountOperationDeposit:
			deposits[m.MOP] += m.Amount
		case models.AccountOperationRefund:
			refunds[m.MOP] += -m.Amount
		case models.AccountOperationWriteOff:
			s.WriteOffs += m.Amount
		}
	}
	s.Deposits = mopRows(deposits)
	s.Refunds = mopRows(refunds)
	return s
}

func mopRows(byMOP map[string]int64) []PaymentRow {
	rows := make([]PaymentRow, 0, len(byMOP))
	for mop, amount := range byMOP {
		if amount != 0 {
			rows = append(rows, PaymentRow{Label: models.AccountMOPLabel(mop), Amount: amount})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })
	return rows
}

// accountMovement : total des mouvements de compte d'une période par type et
// moyen.
type accountMovement struct {
	Type   string
	MOP    string
	Amount int64
}

// GetAccountMovements totalise les mouvements de compte client de
// [from, to[ (date du mouvement) par type et moyen.
func (r *AccountingRepository) GetAccountMovements(ctx context.Context, merchantID string, from, toExclusive time.Time) ([]accountMovement, error) {
	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, `
		SELECT operation_type, COALESCE(mop, ''), SUM(amount)
		FROM customer_account_operations
		WHERE merchant_id = ? AND operation_date >= ? AND operation_date < ?
		GROUP BY operation_type, COALESCE(mop, '')`, merchantID, acctUTCParam(from), acctUTCParam(toExclusive))
	if err != nil {
		return nil, fmt.Errorf("accounting: account movements: %w", err)
	}
	defer rows.Close()
	var out []accountMovement
	for rows.Next() {
		var m accountMovement
		if err := rows.Scan(&m.Type, &m.MOP, &m.Amount); err != nil {
			return nil, fmt.Errorf("accounting: account movements: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// accountSummary calcule le bloc « Compte client » du rapport.
func (s *AccountingService) accountSummary(ctx context.Context, merchantID string, fromLocal, toExclusive time.Time, scope orderScope) (AccountSummary, error) {
	orderPayments, err := s.repo.GetOrderPayments(ctx, merchantID, fromLocal, toExclusive, scope)
	if err != nil {
		return AccountSummary{}, err
	}
	movements, err := s.repo.GetAccountMovements(ctx, merchantID, fromLocal, toExclusive)
	if err != nil {
		return AccountSummary{}, err
	}
	return buildAccountSummary(orderPayments, movements), nil
}

// drawAccountSection dessine le bloc « Comptes clients » sous les
// encaissements : rien si aucun mouvement sur la période.
func drawAccountSection(pdf *gofpdf.Fpdf, translate func(string) string, a AccountSummary, currency string) {
	if a.isEmpty() {
		return
	}
	money := func(v int64) string { return fmt.Sprintf("%.2f %s", float64(v)/100, currency) }
	row := func(label string, amount int64) {
		pdf.CellFormat(120, 7, translate(label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(70, 7, translate(money(amount)), "1", 1, "R", false, 0, "")
	}

	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Comptes clients (avances et ardoises)"), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 10)
	row("Commandes réglées sur le compte client (dans la TVA, sans encaissement)", a.Consumed)
	for _, d := range a.Deposits {
		row("Versements reçus · "+d.Label+" (encaissés, hors chiffre d'affaires)", d.Amount)
	}
	for _, r := range a.Refunds {
		row("Remboursements de solde · "+r.Label, -r.Amount)
	}
	if a.WriteOffs != 0 {
		row("Abandons de créance", a.WriteOffs)
	}
	pdf.SetFont("Arial", "B", 10)
	row("Variation des soldes clients", a.NetChange())

	pdf.Ln(2)
	pdf.SetFont("Arial", "I", 9)
	pdf.MultiCell(190, 5, translate(
		"Un versement sur un compte client est une avance utilisable sur tous les produits (bon à usages multiples, "+
			"CGI art. 256 ter) : ni chiffre d'affaires ni TVA à sa réception. La vente et sa TVA sont comptées à la "+
			"commande réglée sur le compte. L'écart entre la TVA et l'argent encaissé sur la période est égal à "+
			"la variation des soldes clients (compte 4191 pour les avances, 411 pour les ardoises)."), "", "L", false)
}

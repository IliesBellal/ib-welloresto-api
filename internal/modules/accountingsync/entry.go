package accountingsync

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
)

// DefaultKey est la clé de repli des tables de correspondance.
const DefaultKey = "default"

// Mapping fait correspondre les ventes d'un établissement à SES comptes
// comptables (numéros du plan comptable, indépendants de l'outil : chaque
// connecteur les traduit en identifiants chez l'outil, voir Provider.Prepare).
//
// Chaque table cherche d'abord la clé précise (taux de TVA, code du moyen de
// paiement) puis "default".
type Mapping struct {
	// JournalCode : code du journal de ventes chez l'outil (ex. "VE").
	JournalCode string `json:"journal_code"`
	// SalesAccounts : compte de produits (706, 707…) par taux de TVA ("20",
	// "10", "5.5", "0") ; l'écriture y porte le HT.
	SalesAccounts map[string]string `json:"sales_accounts"`
	// VATAccounts : compte de TVA collectée (44571x) par taux de TVA.
	VATAccounts map[string]string `json:"vat_accounts"`
	// PaymentAccounts : compte d'encaissement (531 caisse, 511 CB à encaisser,
	// 4xx plateformes…) par code du moyen de paiement (payments.mop : "ES",
	// "CB", "TR", "STRIPE", "UBER_EATS"…).
	PaymentAccounts map[string]string `json:"payment_accounts"`
	// DifferenceAccount : absorbe l'écart entre ventes et encaissements du jour
	// (commande non réglée, arrondi), pour que l'écriture soit toujours équilibrée.
	DifferenceAccount string `json:"difference_account"`
}

// Validate vérifie que la correspondance est complète.
func (m Mapping) Validate() error {
	var missing []string
	if strings.TrimSpace(m.JournalCode) == "" {
		missing = append(missing, "journal_code")
	}
	if strings.TrimSpace(m.SalesAccounts[DefaultKey]) == "" {
		missing = append(missing, "sales_accounts.default")
	}
	if strings.TrimSpace(m.VATAccounts[DefaultKey]) == "" {
		missing = append(missing, "vat_accounts.default")
	}
	if strings.TrimSpace(m.PaymentAccounts[DefaultKey]) == "" {
		missing = append(missing, "payment_accounts.default")
	}
	if strings.TrimSpace(m.DifferenceAccount) == "" {
		missing = append(missing, "difference_account")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w : %s", ErrInvalidMapping, strings.Join(missing, ", "))
	}
	for _, n := range m.AccountNumbers() {
		if !validAccountNumber(n) {
			return fmt.Errorf("%w : numéro de compte invalide %q (chiffres uniquement)", ErrInvalidMapping, n)
		}
	}
	return nil
}

// Normalize retire les espaces autour des valeurs.
func (m Mapping) Normalize() Mapping {
	trim := func(in map[string]string) map[string]string {
		out := make(map[string]string, len(in))
		for k, v := range in {
			if v = strings.TrimSpace(v); v != "" {
				out[strings.TrimSpace(k)] = v
			}
		}
		return out
	}
	return Mapping{
		JournalCode:       strings.TrimSpace(m.JournalCode),
		SalesAccounts:     trim(m.SalesAccounts),
		VATAccounts:       trim(m.VATAccounts),
		PaymentAccounts:   trim(m.PaymentAccounts),
		DifferenceAccount: strings.TrimSpace(m.DifferenceAccount),
	}
}

// AccountNumbers liste, sans doublon et triés, les comptes que la correspondance utilise.
func (m Mapping) AccountNumbers() []string {
	seen := map[string]bool{}
	add := func(n string) {
		if n = strings.TrimSpace(n); n != "" {
			seen[n] = true
		}
	}
	for _, t := range []map[string]string{m.SalesAccounts, m.VATAccounts, m.PaymentAccounts} {
		for _, n := range t {
			add(n)
		}
	}
	add(m.DifferenceAccount)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func validAccountNumber(n string) bool {
	if n == "" || len(n) > 20 {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func pick(table map[string]string, key string) string {
	if v := strings.TrimSpace(table[key]); v != "" {
		return v
	}
	return strings.TrimSpace(table[DefaultKey])
}

// DailySales est la journée de ventes d'un établissement telle que scellée par
// sa clôture fiscale journalière : la seule source des écritures.
type DailySales struct {
	MerchantID   string
	Date         string // AAAA-MM-JJ, jour local de l'établissement
	ClosureID    int64
	ClosureHash  string
	VATByRate    []fiscal.VATTotal
	PaymentsByOp []fiscal.PaymentTotal
}

// EntryLine est une ligne d'écriture ; montants en centimes, positifs, un seul
// des deux côtés renseigné.
type EntryLine struct {
	Account string `json:"account"`
	Label   string `json:"label"`
	Debit   int64  `json:"debit"`
	Credit  int64  `json:"credit"`
}

// Entry est l'écriture de ventes d'une journée, équilibrée par construction.
type Entry struct {
	Date  string `json:"date"`
	Label string `json:"label"`
	// Reference identifie l'écriture (et le jour) ; incluse dans le libellé pour
	// pouvoir la retrouver chez l'outil.
	Reference string      `json:"reference"`
	Lines     []EntryLine `json:"lines"`
	// Difference : ventes − encaissements du jour (centimes) porté par le compte
	// d'écart ; 0 si la journée est nette.
	Difference int64 `json:"difference"`
}

var (
	// ErrNothingToPost : journée sans vente ni encaissement.
	ErrNothingToPost = errors.New("accountingsync: rien à écrire pour ce jour")
	// ErrInvalidMapping : correspondance de comptes incomplète ou invalide.
	ErrInvalidMapping = errors.New("accountingsync: correspondance de comptes invalide")
)

// Reference renvoie la référence de l'écriture d'un établissement pour un jour.
func Reference(merchantID, date string) string {
	return "WR-" + merchantID + "-" + strings.ReplaceAll(date, "-", "")
}

func rateKey(rate float64) string {
	return strconv.FormatFloat(math.Round(rate*100)/100, 'f', -1, 64)
}

// BuildEntry construit l'écriture de la journée :
//
//	crédit  compte de produits   HT de chaque taux de TVA
//	crédit  compte de TVA        TVA de chaque taux
//	débit   compte d'encaissement  paiements du jour, par moyen
//	±       compte d'écart       ventes − encaissements, pour équilibrer
//
// Les ventes viennent des tickets (nettes des avoirs et des remises) ; les
// encaissements des paiements actifs du jour (remboursements retranchés,
// remises exclues : ce n'est pas de l'argent encaissé). Un solde négatif d'un
// compte (jour de remboursements) devient une ligne du côté opposé, ce qui
// laisse l'écriture équilibrée.
func BuildEntry(sales DailySales, m Mapping) (Entry, error) {
	if err := m.Validate(); err != nil {
		return Entry{}, err
	}

	// Montants signés par compte et par côté : positif = au crédit pour les
	// ventes, au débit pour les encaissements.
	type acc struct {
		label string
		cents int64
	}
	credits := map[string]*acc{} // ventes, TVA
	var creditOrder []string
	addCredit := func(account, label string, cents int64) {
		if cents == 0 {
			return
		}
		if a, ok := credits[account]; ok {
			a.cents += cents
			return
		}
		credits[account] = &acc{label: label, cents: cents}
		creditOrder = append(creditOrder, account)
	}

	vats := append([]fiscal.VATTotal(nil), sales.VATByRate...)
	sort.Slice(vats, func(i, j int) bool { return vats[i].Rate < vats[j].Rate })
	var totalSales int64
	for _, v := range vats {
		key := rateKey(v.Rate)
		addCredit(pick(m.SalesAccounts, key), "Ventes TVA "+key+" %", v.HT)
		addCredit(pick(m.VATAccounts, key), "TVA collectée "+key+" %", v.TVA)
		totalSales += v.HT + v.TVA
	}

	debits := map[string]*acc{}
	var debitOrder []string
	var totalPaid int64
	for _, p := range sales.PaymentsByOp {
		if !p.Enabled || models.IsDiscountMOP(p.MOP) {
			continue
		}
		amount := p.Amount
		if strings.EqualFold(p.OperationType, models.OperationTypeRefund) {
			// Un remboursement diminue l'encaissement, quel que soit le signe
			// sous lequel il est enregistré.
			if amount < 0 {
				amount = -amount
			}
			amount = -amount
		}
		if amount == 0 {
			continue
		}
		account := pick(m.PaymentAccounts, strings.ToUpper(strings.TrimSpace(p.MOP)))
		if a, ok := debits[account]; ok {
			a.cents += amount
		} else {
			debits[account] = &acc{label: "Encaissements " + strings.ToUpper(strings.TrimSpace(p.MOP)), cents: amount}
			debitOrder = append(debitOrder, account)
		}
		totalPaid += amount
	}

	var lines []EntryLine
	push := func(account, label string, debit, credit int64) {
		switch {
		case debit < 0:
			debit, credit = 0, -debit
		case credit < 0:
			debit, credit = -credit, 0
		}
		if debit == 0 && credit == 0 {
			return
		}
		lines = append(lines, EntryLine{Account: account, Label: label, Debit: debit, Credit: credit})
	}
	for _, account := range debitOrder {
		push(account, debits[account].label, debits[account].cents, 0)
	}
	for _, account := range creditOrder {
		push(account, credits[account].label, 0, credits[account].cents)
	}

	// Écart : ce qui a été vendu mais pas (ou en trop) encaissé ce jour.
	diff := totalSales - totalPaid
	if diff > 0 {
		push(m.DifferenceAccount, "Écart ventes / encaissements", diff, 0)
	} else if diff < 0 {
		push(m.DifferenceAccount, "Écart ventes / encaissements", 0, -diff)
	}

	if len(lines) == 0 {
		return Entry{}, ErrNothingToPost
	}
	var sumD, sumC int64
	for _, l := range lines {
		sumD += l.Debit
		sumC += l.Credit
	}
	if sumD != sumC {
		// Ne doit pas arriver (l'écart équilibre par construction) : on refuse
		// plutôt que d'envoyer une écriture que l'outil rejetterait.
		return Entry{}, fmt.Errorf("accountingsync: écriture déséquilibrée pour %s (débit %d, crédit %d)", sales.Date, sumD, sumC)
	}

	ref := Reference(sales.MerchantID, sales.Date)
	return Entry{
		Date:       sales.Date,
		Label:      fmt.Sprintf("Ventes Wello Resto du %s [%s]", formatDateFR(sales.Date), ref),
		Reference:  ref,
		Lines:      lines,
		Difference: diff,
	}, nil
}

func formatDateFR(iso string) string {
	if len(iso) != 10 {
		return iso
	}
	return iso[8:10] + "/" + iso[5:7] + "/" + iso[0:4]
}

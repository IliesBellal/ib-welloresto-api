package accountingsync

import (
	"errors"
	"strings"
	"testing"

	"welloresto-api/internal/fiscal"
)

func testMapping() Mapping {
	return Mapping{
		JournalCode:       "VE",
		SalesAccounts:     map[string]string{"default": "707000", "10": "706000"},
		VATAccounts:       map[string]string{"default": "445710", "10": "445712"},
		PaymentAccounts:   map[string]string{"default": "580000", "ES": "531000"},
		DifferenceAccount: "471000",
	}
}

func sums(e Entry) (debit, credit int64) {
	for _, l := range e.Lines {
		debit += l.Debit
		credit += l.Credit
	}
	return
}

func lineFor(e Entry, account string) (EntryLine, bool) {
	for _, l := range e.Lines {
		if l.Account == account {
			return l, true
		}
	}
	return EntryLine{}, false
}

// Journée nette : 110 € TTC à 10 % + 120 € TTC à 20 %, réglés 150 € en
// espèces et 80 € par carte. L'écriture est équilibrée sans compte d'écart.
func TestBuildEntry_BalancedDay(t *testing.T) {
	sales := DailySales{
		MerchantID: "212", Date: "2026-10-11",
		VATByRate: []fiscal.VATTotal{
			{Rate: 20, TTC: 12000, HT: 10000, TVA: 2000},
			{Rate: 10, TTC: 11000, HT: 10000, TVA: 1000},
		},
		PaymentsByOp: []fiscal.PaymentTotal{
			{MOP: "ES", OperationType: "SALE", Enabled: true, Amount: 15000},
			{MOP: "CB", OperationType: "SALE", Enabled: true, Amount: 8000},
		},
	}
	e, err := BuildEntry(sales, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	d, c := sums(e)
	if d != 23000 || c != 23000 {
		t.Fatalf("débit %d, crédit %d, attendu 23000/23000", d, c)
	}
	if e.Difference != 0 {
		t.Errorf("écart = %d, attendu 0", e.Difference)
	}
	if l, ok := lineFor(e, "531000"); !ok || l.Debit != 15000 {
		t.Errorf("espèces : %+v", l)
	}
	if l, ok := lineFor(e, "580000"); !ok || l.Debit != 8000 {
		t.Errorf("carte (compte par défaut) : %+v", l)
	}
	if l, ok := lineFor(e, "706000"); !ok || l.Credit != 10000 {
		t.Errorf("ventes 10 %% : %+v", l)
	}
	if l, ok := lineFor(e, "445712"); !ok || l.Credit != 1000 {
		t.Errorf("TVA 10 %% : %+v", l)
	}
	if l, ok := lineFor(e, "707000"); !ok || l.Credit != 10000 {
		t.Errorf("ventes 20 %% (compte par défaut) : %+v", l)
	}
	if _, ok := lineFor(e, "471000"); ok {
		t.Error("pas d'écart attendu, pas de ligne d'écart")
	}
	if e.Reference != "WR-212-20261011" || !strings.Contains(e.Label, "[WR-212-20261011]") || !strings.Contains(e.Label, "11/10/2026") {
		t.Errorf("référence / libellé : %q %q", e.Reference, e.Label)
	}
}

// Une commande non réglée : 230 € vendus, 200 € encaissés → 30 € au débit du compte d'écart.
func TestBuildEntry_UnpaidSaleGoesToDifferenceAccount(t *testing.T) {
	sales := DailySales{
		MerchantID: "212", Date: "2026-10-11",
		VATByRate:    []fiscal.VATTotal{{Rate: 10, TTC: 23000, HT: 20909, TVA: 2091}},
		PaymentsByOp: []fiscal.PaymentTotal{{MOP: "CB", OperationType: "SALE", Enabled: true, Amount: 20000}},
	}
	e, err := BuildEntry(sales, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if e.Difference != 3000 {
		t.Fatalf("écart = %d, attendu 3000", e.Difference)
	}
	if l, ok := lineFor(e, "471000"); !ok || l.Debit != 3000 || l.Credit != 0 {
		t.Errorf("écart : %+v", l)
	}
	d, c := sums(e)
	if d != c {
		t.Errorf("déséquilibre %d / %d", d, c)
	}
}

// Encaissé plus que vendu (acompte d'un jour précédent) : l'écart passe au crédit.
func TestBuildEntry_OverpaidGoesToCredit(t *testing.T) {
	sales := DailySales{
		MerchantID: "212", Date: "2026-10-11",
		VATByRate:    []fiscal.VATTotal{{Rate: 10, TTC: 11000, HT: 10000, TVA: 1000}},
		PaymentsByOp: []fiscal.PaymentTotal{{MOP: "ES", OperationType: "SALE", Enabled: true, Amount: 12000}},
	}
	e, err := BuildEntry(sales, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if e.Difference != -1000 {
		t.Fatalf("écart = %d, attendu -1000", e.Difference)
	}
	if l, _ := lineFor(e, "471000"); l.Credit != 1000 || l.Debit != 0 {
		t.Errorf("écart : %+v", l)
	}
}

// Remises exclues, paiements désactivés ignorés, remboursements retranchés
// quel que soit le signe sous lequel ils sont enregistrés.
func TestBuildEntry_PaymentsFiltering(t *testing.T) {
	for _, refundAmount := range []int64{2000, -2000} {
		sales := DailySales{
			MerchantID: "212", Date: "2026-10-11",
			VATByRate: []fiscal.VATTotal{{Rate: 10, TTC: 8000, HT: 7273, TVA: 727}},
			PaymentsByOp: []fiscal.PaymentTotal{
				{MOP: "ES", OperationType: "SALE", Enabled: true, Amount: 10000},
				{MOP: "ES", OperationType: "REFUND", Enabled: true, Amount: refundAmount},
				{MOP: "CURRENCY", OperationType: "SALE", Enabled: true, Amount: 500},
				{MOP: "CB", OperationType: "SALE", Enabled: false, Amount: 9999},
			},
		}
		e, err := BuildEntry(sales, testMapping())
		if err != nil {
			t.Fatal(err)
		}
		if l, ok := lineFor(e, "531000"); !ok || l.Debit != 8000 {
			t.Errorf("remboursement %d : espèces nettes = %+v, attendu 8000", refundAmount, l)
		}
		if e.Difference != 0 {
			t.Errorf("remboursement %d : écart %d, attendu 0", refundAmount, e.Difference)
		}
	}
}

// Journée de remboursements seuls : les soldes négatifs passent du côté opposé.
func TestBuildEntry_RefundOnlyDay(t *testing.T) {
	sales := DailySales{
		MerchantID: "212", Date: "2026-10-11",
		VATByRate:    []fiscal.VATTotal{{Rate: 10, TTC: -1100, HT: -1000, TVA: -100}},
		PaymentsByOp: []fiscal.PaymentTotal{{MOP: "ES", OperationType: "REFUND", Enabled: true, Amount: 1100}},
	}
	e, err := BuildEntry(sales, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := lineFor(e, "706000"); l.Debit != 1000 || l.Credit != 0 {
		t.Errorf("ventes : %+v", l)
	}
	if l, _ := lineFor(e, "531000"); l.Credit != 1100 || l.Debit != 0 {
		t.Errorf("espèces : %+v", l)
	}
	d, c := sums(e)
	if d != c {
		t.Errorf("déséquilibre %d / %d", d, c)
	}
}

func TestBuildEntry_EmptyDay(t *testing.T) {
	_, err := BuildEntry(DailySales{MerchantID: "212", Date: "2026-10-11"}, testMapping())
	if !errors.Is(err, ErrNothingToPost) {
		t.Fatalf("err = %v, attendu ErrNothingToPost", err)
	}
}

func TestMappingValidate(t *testing.T) {
	if err := testMapping().Validate(); err != nil {
		t.Fatalf("correspondance valide refusée : %v", err)
	}
	bad := testMapping()
	bad.DifferenceAccount = ""
	bad.PaymentAccounts = map[string]string{"ES": "531000"}
	err := bad.Validate()
	if !errors.Is(err, ErrInvalidMapping) || !strings.Contains(err.Error(), "payment_accounts.default") || !strings.Contains(err.Error(), "difference_account") {
		t.Errorf("err = %v", err)
	}
	notDigits := testMapping()
	notDigits.SalesAccounts["default"] = "70 7000; DROP"
	if err := notDigits.Validate(); !errors.Is(err, ErrInvalidMapping) {
		t.Errorf("numéro non numérique accepté : %v", err)
	}
}

func TestRateKey(t *testing.T) {
	for in, want := range map[float64]string{20: "20", 10: "10", 5.5: "5.5", 0: "0", 5.50001: "5.5"} {
		if got := rateKey(in); got != want {
			t.Errorf("rateKey(%v) = %q, attendu %q", in, got, want)
		}
	}
}

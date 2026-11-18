package accountingsync

import (
	"testing"

	"welloresto-api/internal/fiscal"
)

// Compte client (docs/customer-accounts.md) : une commande réglée sur le
// compte débite le compte des soldes clients ; un versement est de l'argent
// encaissé contre ce même compte, sans vente.
func TestBuildEntry_CustomerAccount(t *testing.T) {
	m := testMapping()
	m.PaymentAccounts["ACCOUNT"] = "419100"
	m.PaymentAccounts["WRITE_OFF"] = "654000"

	sales := DailySales{
		MerchantID: "212", Date: "2026-11-17",
		// 33 € vendus à 10 % : 10 € par carte, 23 € sur le compte client.
		VATByRate: []fiscal.VATTotal{{Rate: 10, TTC: 3300, HT: 3000, TVA: 300}},
		PaymentsByOp: []fiscal.PaymentTotal{
			{MOP: "CB", OperationType: "SALE", Enabled: true, Amount: 1000},
			{MOP: "ACCOUNT", OperationType: "SALE", Enabled: true, Amount: 2300},
		},
		// Le même jour : 1 000 € versés par carte, 20 € rendus en espèces, 5 €
		// d'ardoise abandonnés.
		AccountOperations: []fiscal.AccountOperationTotal{
			{OperationType: "DEPOSIT", MOP: "CB", Amount: 100000, Count: 1},
			{OperationType: "REFUND", MOP: "ES", Amount: -2000, Count: 1},
			{OperationType: "WRITE_OFF", Amount: 500, Count: 1},
		},
	}
	e, err := BuildEntry(sales, m)
	if err != nil {
		t.Fatal(err)
	}
	d, c := sums(e)
	if d != c {
		t.Fatalf("écriture déséquilibrée : débit %d, crédit %d", d, c)
	}
	if e.Difference != 0 {
		t.Errorf("écart = %d : une vente réglée sur le compte n'est pas une vente impayée", e.Difference)
	}
	// Carte : 10 € de vente + 1 000 € de versement.
	if l, ok := lineFor(e, "580000"); !ok || l.Debit != 101000 {
		t.Errorf("carte : %+v", l)
	}
	// Espèces rendues : au crédit de la caisse.
	if l, ok := lineFor(e, "531000"); !ok || l.Credit != 2000 {
		t.Errorf("espèces rendues : %+v", l)
	}
	if l, ok := lineFor(e, "654000"); !ok || l.Debit != 500 {
		t.Errorf("abandon de créance : %+v", l)
	}
	// Compte client : débité de 23 € (consommation), crédité de 1 000 − 20 + 5.
	// Les deux montants sont regroupés sur une ligne de chaque côté au plus.
	var accDebit, accCredit int64
	for _, l := range e.Lines {
		if l.Account == "419100" {
			accDebit += l.Debit
			accCredit += l.Credit
		}
	}
	if accCredit-accDebit != 100000-2000+500-2300 {
		t.Errorf("compte client : débit %d, crédit %d", accDebit, accCredit)
	}
}

// Sans compte « ACCOUNT » renseigné, rien ne part dans le compte par défaut des
// encaissements : le compte d'écart, visible, absorbe.
func TestBuildEntry_CustomerAccountWithoutMapping(t *testing.T) {
	sales := DailySales{
		MerchantID: "212", Date: "2026-11-17",
		VATByRate:    []fiscal.VATTotal{{Rate: 10, TTC: 2300, HT: 2091, TVA: 209}},
		PaymentsByOp: []fiscal.PaymentTotal{{MOP: "ACCOUNT", OperationType: "SALE", Enabled: true, Amount: 2300}},
	}
	e, err := BuildEntry(sales, testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lineFor(e, "580000"); ok {
		t.Error("le compte par défaut des encaissements ne doit pas recevoir la consommation d'un compte client")
	}
	if l, ok := lineFor(e, "471000"); !ok || l.Debit != 2300 {
		t.Errorf("compte d'écart : %+v", l)
	}
}

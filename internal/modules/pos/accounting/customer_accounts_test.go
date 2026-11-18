package accounting

import "testing"

// L'écart entre la TVA (ventes) et l'argent encaissé d'une période est égal à
// la variation des soldes clients (docs/customer-accounts.md).
func TestBuildAccountSummary(t *testing.T) {
	orderPayments := []orderPaymentLine{
		{OrderID: 1, MOP: "CB", Amount: 7000},
		{OrderID: 2, MOP: "ACCOUNT", Amount: 2000}, // consommation d'une avance
		{OrderID: 3, MOP: "ACCOUNT", Amount: 1000}, // ardoise
		{OrderID: 4, MOP: "ACCOUNT", Amount: -500}, // commande remboursée sur le compte
	}
	movements := []accountMovement{
		{Type: "DEPOSIT", MOP: "CB", Amount: 1500},
		{Type: "DEPOSIT", MOP: "ES", Amount: 400},
		{Type: "REFUND", MOP: "ES", Amount: -100},
		{Type: "WRITE_OFF", Amount: 50},
	}
	s := buildAccountSummary(orderPayments, movements)

	if s.Consumed != 2500 {
		t.Errorf("Consumed = %d, want 2500", s.Consumed)
	}
	if len(s.Deposits) != 2 || sumRows(s.Deposits) != 1900 {
		t.Errorf("Deposits = %+v", s.Deposits)
	}
	if len(s.Refunds) != 1 || s.Refunds[0].Amount != 100 || s.Refunds[0].Label != "Espèces" {
		t.Errorf("Refunds = %+v", s.Refunds)
	}
	if s.WriteOffs != 50 {
		t.Errorf("WriteOffs = %d", s.WriteOffs)
	}
	// 1900 versés − 100 rendus + 50 abandonnés − 2500 consommés.
	if got := s.NetChange(); got != -650 {
		t.Errorf("NetChange = %d, want -650", got)
	}

	// Ventes 9 500 (7 000 CB + 2 500 sur compte), argent 7 000 + 1 900 − 100 :
	// l'écart ventes − argent vaut − variation − abandons.
	sales := int64(9500)
	money := int64(7000 + 1900 - 100)
	if sales-money != -s.NetChange()+s.WriteOffs {
		t.Errorf("écart ventes/encaissements %d, variation %d, abandons %d", sales-money, s.NetChange(), s.WriteOffs)
	}
}

func TestBuildAccountSummary_EmptyWithoutMovement(t *testing.T) {
	s := buildAccountSummary([]orderPaymentLine{{MOP: "ES", Amount: 1000}}, nil)
	if !s.isEmpty() {
		t.Fatalf("summary should be empty: %+v", s)
	}
}

package customer_accounts

import (
	"bytes"
	"testing"
	"time"

	"welloresto-api/internal/models"
)

func TestParsePeriod(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, paris)
	from, to, err := parsePeriod("", "", now, paris)
	if err != nil || from.Format("2006-01-02") != "2026-09-01" || to.Format("2006-01-02") != "2026-11-18" {
		t.Fatalf("défaut = %v → %v (%v)", from, to, err)
	}
	if _, _, err := parsePeriod("2026-11-20", "2026-11-01", now, paris); err == nil {
		t.Fatal("période inversée acceptée")
	}
	if _, _, err := parsePeriod("18/11/2026", "", now, paris); err == nil {
		t.Fatal("date invalide acceptée")
	}
}

func TestBuildStatement(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(m time.Month, d int) int64 { return time.Date(2026, m, d, 12, 0, 0, 0, paris).Unix() }
	cb := "CB"
	entries := []models.CustomerAccountEntry{
		{Kind: "operation", Date: at(9, 20), Delta: 10000, Operation: &models.CustomerAccountOperation{OperationType: "DEPOSIT", MOP: &cb}},
		{Kind: "payment", Date: at(10, 2), Delta: -2350, Payment: &models.CustomerAccountPayment{OrderNum: "42", ReceiptNumber: "T-0042"}},
		{Kind: "payment", Date: at(10, 15), Delta: -1500, Payment: &models.CustomerAccountPayment{OrderNum: "57"}},
		{Kind: "payment", Date: at(11, 3), Delta: -900, Payment: &models.CustomerAccountPayment{OrderNum: "80"}},
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, paris)
	to := time.Date(2026, 10, 31, 0, 0, 0, 0, paris)
	st := buildStatement(entries, from, to)
	if st.Opening != 10000 || st.Closing != 6150 || len(st.Lines) != 2 {
		t.Fatalf("statement = %+v", st)
	}
	if st.Lines[0].Label != "Commande n° 42 (ticket T-0042)" || st.Lines[0].Balance != 7650 {
		t.Errorf("ligne 1 = %+v", st.Lines[0])
	}
	pdf, err := renderStatementPDF(MerchantInfo{Name: "Chez Léa", Address: "1 rue de Metz", SIRET: "123", Loc: paris},
		CustomerContact{Name: "Jean Dupont"}, st)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf = %d octets, %v", len(pdf), err)
	}
}

func TestEntryLabel(t *testing.T) {
	es := "ES"
	cases := map[string]models.CustomerAccountEntry{
		"Versement · Espèces":      {Operation: &models.CustomerAccountOperation{OperationType: "DEPOSIT", MOP: &es}},
		"Abandon de créance":       {Operation: &models.CustomerAccountOperation{OperationType: "WRITE_OFF"}},
		"Commande n° 9 remboursée": {Delta: 500, Payment: &models.CustomerAccountPayment{OrderNum: "9"}},
	}
	for want, e := range cases {
		if got := entryLabel(e); got != want {
			t.Errorf("entryLabel = %q, want %q", got, want)
		}
	}
	if balanceLabel(-850) != "Ardoise 8,50 €" || balanceLabel(12000) != "Avance 120,00 €" || balanceLabel(0) != "Soldé" {
		t.Error("balanceLabel")
	}
}

func TestOnlineRefundDelta(t *testing.T) {
	cases := []struct {
		name                 string
		total, paid, already int64
		want                 int64
	}{
		{"premier remboursement partiel", 4000, 10000, 0, 4000},
		{"second remboursement : seulement la différence", 10000, 10000, 4000, 6000},
		{"événement rejoué", 4000, 10000, 4000, 0},
		{"événement en retard", 2000, 10000, 4000, 0},
		{"jamais plus que le montant payé", 15000, 10000, 0, 10000},
	}
	for _, c := range cases {
		if got := onlineRefundDelta(c.total, c.paid, c.already); got != c.want {
			t.Errorf("%s : %d, attendu %d", c.name, got, c.want)
		}
	}
}

func TestReminderDue(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	daysAgo := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	lastAt := daysAgo(30)
	recent := daysAgo(3)
	cases := []struct {
		name   string
		oldest time.Time
		sent   int
		last   *time.Time
		want   bool
	}{
		{"dette trop récente", daysAgo(10), 0, nil, false},
		{"première relance à 30 jours", daysAgo(30), 0, nil, true},
		{"relance récente", daysAgo(45), 1, &recent, false},
		{"nouvelle relance 30 jours après", daysAgo(60), 1, &lastAt, true},
		{"nombre maximal atteint", daysAgo(120), 3, &lastAt, false},
	}
	for _, c := range cases {
		if got := reminderDue(now, c.oldest, c.sent, c.last, 30, 3); got != c.want {
			t.Errorf("%s : reminderDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestApplicationFee(t *testing.T) {
	if applicationFee(10000, 0, 0) != 0 {
		t.Error("sans frais ScanNOrder : aucun")
	}
	if got := applicationFee(10000, 0.015, 25); got != 175 {
		t.Errorf("frais = %d, want 175", got)
	}
	if got := applicationFee(100, 0.5, 100); got != 99 {
		t.Errorf("frais plafonnés = %d, want 99", got)
	}
}

func TestReminderSMS(t *testing.T) {
	got := reminderSMS("Chez Léa", "35,00 €", "https://api.example/pay/account/x")
	if got != "Chez Léa : votre ardoise de 35,00 € est à régler. Payer en ligne : https://api.example/pay/account/x" {
		t.Fatalf("sms = %q", got)
	}
}

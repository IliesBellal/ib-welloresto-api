package customeraccount

import (
	"errors"
	"testing"
	"time"

	"welloresto-api/internal/models"
)

func day(d int) time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, d) }

func TestAgeDebts_FIFO(t *testing.T) {
	now := day(90)
	entries := []LedgerEntry{
		// Client 1 : commandes à J10 (30 €), J40 (20 €), J80 (10 €) ; versement
		// de 35 € à J85 : il solde J10, puis 5 € de J40.
		{CustomerID: 1, Date: day(10), Delta: -3000, Kind: KindConsumption},
		{CustomerID: 1, Date: day(40), Delta: -2000, Kind: KindConsumption},
		{CustomerID: 1, Date: day(80), Delta: -1000, Kind: KindConsumption},
		{CustomerID: 1, Date: day(85), Delta: 3500, Kind: KindDeposit},
		// Client 2 : avance de 50 € puis 30 € consommés : pas de dette.
		{CustomerID: 2, Date: day(5), Delta: 5000, Kind: KindDeposit},
		{CustomerID: 2, Date: day(6), Delta: -3000, Kind: KindConsumption},
		// Client 3 : avance de 10 €, commande de 25 € : 15 € dus depuis J70.
		{CustomerID: 3, Date: day(60), Delta: 1000, Kind: KindDeposit},
		{CustomerID: 3, Date: day(70), Delta: -2500, Kind: KindConsumption},
	}
	aging := AgeDebts(entries, now)
	if len(aging) != 2 {
		t.Fatalf("aging = %+v, want 2 customers", aging)
	}
	c1 := aging[0]
	if c1.CustomerSummary.CustomerID != 1 || c1.Debt != 2500 {
		t.Fatalf("client 1 = %+v", c1)
	}
	// Reste 15 € de J40 (50 jours) et 10 € de J80 (10 jours).
	if c1.Days31To60 != 1500 || c1.Days0To30 != 1000 || c1.Days61Plus != 0 {
		t.Errorf("client 1 buckets = %+v", c1)
	}
	if c1.OldestUnpaid != day(40).Unix() {
		t.Errorf("client 1 oldest unpaid = %v", time.Unix(c1.OldestUnpaid, 0))
	}
	c3 := aging[1]
	if c3.CustomerSummary.CustomerID != 3 || c3.Debt != 1500 || c3.Days0To30 != 1500 {
		t.Errorf("client 3 = %+v", c3)
	}
}

func TestOldestUnpaid(t *testing.T) {
	if _, ok := OldestUnpaid([]LedgerEntry{{Date: day(1), Delta: 100}}); ok {
		t.Fatal("no debt expected")
	}
	at, ok := OldestUnpaid([]LedgerEntry{
		{Date: day(3), Delta: -500},
		{Date: day(1), Delta: -200},
		{Date: day(4), Delta: 200},
	})
	if !ok || !at.Equal(day(3)) {
		t.Fatalf("oldest unpaid = %v, %v ; want day 3 (day 1 paid first)", at, ok)
	}
}

func TestMonthlyTotals(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 12, 0, 0, 0, paris) }
	entries := []LedgerEntry{
		{CustomerID: 1, Date: time.Date(2025, 12, 20, 12, 0, 0, 0, paris), Delta: 10000, Kind: KindDeposit}, // avant l'année
		{CustomerID: 1, Date: at(1, 5), Delta: -2500, Kind: KindConsumption},
		{CustomerID: 2, Date: at(1, 10), Delta: -4000, Kind: KindConsumption},
		{CustomerID: 2, Date: at(2, 3), Delta: 3000, Kind: KindDeposit},
		{CustomerID: 1, Date: at(2, 4), Delta: -1000, Kind: KindRefund},
		{CustomerID: 2, Date: at(2, 6), Delta: 1000, Kind: KindWriteOff},
		{CustomerID: 1, Date: at(2, 7), Delta: 500, Kind: KindConsumption}, // commande remboursée sur le compte
	}
	months := MonthlyTotals(entries, 2026, paris)
	if len(months) != 12 {
		t.Fatalf("%d months", len(months))
	}
	jan, feb, dec := months[0], months[1], months[11]
	if jan.Consumed != 6500 || jan.Deposits != 0 || jan.NetChange != -6500 {
		t.Errorf("janvier = %+v", jan)
	}
	// Fin janvier : client 1 avance 75 €, client 2 ardoise 40 €.
	if jan.EndCredit != 7500 || jan.EndDebt != 4000 {
		t.Errorf("janvier soldes = %+v", jan)
	}
	if feb.Deposits != 3000 || feb.Refunds != 1000 || feb.WriteOffs != 1000 || feb.Consumed != -500 || feb.NetChange != 3500 {
		t.Errorf("février = %+v", feb)
	}
	// Fin février : client 1 70 €, client 2 soldé.
	if feb.EndCredit != 7000 || feb.EndDebt != 0 {
		t.Errorf("février soldes = %+v", feb)
	}
	if dec.EndCredit != 7000 || dec.NetChange != 0 {
		t.Errorf("décembre = %+v", dec)
	}
}

func TestLimitAlert(t *testing.T) {
	cases := []struct {
		debt, limit int64
		alert, over bool
	}{
		{debt: 7000, limit: 10000, alert: false},
		{debt: 8000, limit: 10000, alert: true},
		{debt: 10000, limit: 10000, alert: true},
		{debt: 10001, limit: 10000, alert: true, over: true},
		{debt: 500, limit: 0, alert: true, over: true}, // ardoise interdite ou plafond retiré
	}
	for _, c := range cases {
		a, ok := limitAlert(1, c.debt, c.limit)
		if ok != c.alert || a.Over != c.over {
			t.Errorf("limitAlert(%d, %d) = %+v, %v", c.debt, c.limit, a, ok)
		}
	}
}

func TestEffectiveCreditLimit(t *testing.T) {
	own := int64(5000)
	if got := EffectiveCreditLimit(20000, models.CustomerAccountCustomerSettings{}); got != 20000 {
		t.Errorf("défaut établissement = %d", got)
	}
	if got := EffectiveCreditLimit(20000, models.CustomerAccountCustomerSettings{CreditLimit: &own}); got != 5000 {
		t.Errorf("plafond propre = %d", got)
	}
	if got := EffectiveCreditLimit(20000, models.CustomerAccountCustomerSettings{CreditLimit: &own, CreditBlocked: true}); got != 0 {
		t.Errorf("ardoise interdite = %d", got)
	}
}

func TestCheckCashDeposit(t *testing.T) {
	es := "ES"
	cb := "CB"
	dep := func(amount int64, mop *string) Operation {
		return Operation{Type: models.AccountOperationDeposit, Amount: amount, MOP: mop}
	}
	cases := []struct {
		name      string
		op        Operation
		mode      string
		confirmed bool
		want      error
	}{
		{"carte au-delà de 1 000 € : toujours", dep(500000, &cb), models.AccountCashModeBlock, false, nil},
		{"espèces 1 000 € : toujours", dep(100000, &es), models.AccountCashModeBlock, false, nil},
		{"espèces au-delà, mode bloquer", dep(100001, &es), models.AccountCashModeBlock, true, models.ErrAccountCashLimit},
		{"espèces au-delà, avertir sans confirmation", dep(200000, &es), models.AccountCashModeWarn, false, models.ErrAccountCashConfirmationRequired},
		{"espèces au-delà, avertir confirmé", dep(200000, &es), models.AccountCashModeWarn, true, nil},
		{"espèces au-delà de 15 000 €", dep(1500001, &es), models.AccountCashModeWarn, true, models.ErrAccountCashNonResidentLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := c.op
			err := CheckCashDeposit(&op, c.mode, c.confirmed)
			if !errors.Is(err, c.want) && !(err == nil && c.want == nil) {
				t.Fatalf("CheckCashDeposit() = %v, want %v", err, c.want)
			}
		})
	}
	// Confirmation tracée dans le motif scellé.
	op := dep(200000, &es)
	note := "Avance saison"
	op.Comment = &note
	if err := CheckCashDeposit(&op, models.AccountCashModeWarn, true); err != nil {
		t.Fatal(err)
	}
	if op.Comment == nil || *op.Comment != "Avance saison · Client non-résident (déclaré à la caisse)" {
		t.Fatalf("comment = %v", op.Comment)
	}
}

func TestValidateOperation_OnlineDeposit(t *testing.T) {
	stripe := "STRIPE"
	if err := ValidateOperation(Operation{Type: models.AccountOperationDeposit, Amount: 1000, MOP: &stripe}); err != nil {
		t.Fatalf("online deposit = %v", err)
	}
	// Le remboursement d'un paiement en ligne est écrit par le webhook ; un
	// abandon de créance n'a jamais de moyen.
	reason := "x"
	if err := ValidateOperation(Operation{Type: models.AccountOperationRefund, Amount: -1000, MOP: &stripe, Comment: &reason}); err != nil {
		t.Fatalf("online refund = %v", err)
	}
	if err := ValidateOperation(Operation{Type: models.AccountOperationWriteOff, Amount: 1000, MOP: &stripe, Comment: &reason}); !errors.Is(err, models.ErrAccountInvalidMOP) {
		t.Fatalf("write-off with mop = %v, want invalid mop", err)
	}
}

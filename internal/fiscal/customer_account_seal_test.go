package fiscal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Compte client (docs/customer-accounts.md) : les champs ajoutés aux charges
// scellées ne doivent pas changer l'empreinte des lignes déjà scellées.

func TestPaymentPayload_CustomerOnlyForAccountPayments(t *testing.T) {
	t.Setenv("FISCAL_SIGNING_KEY", "unit-test-key")
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

	plain, err := NewPaymentPayload("42", "1001", 1500, "CB", "SALE", at, "u1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(plain)
	// Forme d'avant le compte client, à l'identique : même empreinte pour les
	// paiements existants.
	want := `{"merchant_id":"42","order_id":1001,"amount":1500,"mop":"CB","operation_type":"SALE","payment_date":"2026-11-17T12:00:00Z","user_id":"u1","comment":null}`
	if string(b) != want {
		t.Fatalf("payment payload changed:\n got  %s\n want %s", b, want)
	}

	customer := int64(77)
	account, _ := NewPaymentPayload("42", "1001", 1500, "ACCOUNT", "SALE", at, "u1", nil, &customer)
	b, _ = json.Marshal(account)
	if !strings.Contains(string(b), `"customer_id":77`) {
		t.Fatalf("account payment payload without customer: %s", b)
	}
	// Le client débité entre dans l'empreinte.
	other := int64(78)
	moved, _ := NewPaymentPayload("42", "1001", 1500, "ACCOUNT", "SALE", at, "u1", nil, &other)
	h1, _, _ := Seal(ChainPayments, GenesisHash, account)
	h2, _, _ := Seal(ChainPayments, GenesisHash, moved)
	if h1 == h2 {
		t.Fatal("customer not bound to the payment hash")
	}
}

func TestClosurePayload_AccountOperationsOmittedWhenEmpty(t *testing.T) {
	p := ClosurePayload{MerchantID: "42", PeriodType: PeriodDay}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "account_operations") {
		t.Fatalf("empty account operations must be omitted (hash of older closures): %s", b)
	}
	p.AccountOperations = []AccountOperationTotal{{OperationType: "DEPOSIT", MOP: "CB", Amount: 100000, Count: 1}}
	b, _ = json.Marshal(p)
	if !strings.Contains(string(b), `"account_operations":[{"operation_type":"DEPOSIT","mop":"CB","amount":100000,"count":1}]`) {
		t.Fatalf("account operations missing: %s", b)
	}
}

func TestSumAccountOperations(t *testing.T) {
	acc := map[string]*AccountOperationTotal{}
	sumAccountOperations(acc, []AccountOperationTotal{{OperationType: "DEPOSIT", MOP: "ES", Amount: 1000, Count: 1}})
	sumAccountOperations(acc, []AccountOperationTotal{
		{OperationType: "DEPOSIT", MOP: "ES", Amount: 500, Count: 2},
		{OperationType: "WRITE_OFF", Amount: 300, Count: 1},
	})
	got := sortedAccountOperations(acc)
	if len(got) != 2 || got[0].Amount != 1500 || got[0].Count != 3 || got[1].OperationType != "WRITE_OFF" {
		t.Fatalf("sortedAccountOperations = %+v", got)
	}
	if sortedAccountOperations(map[string]*AccountOperationTotal{}) != nil {
		t.Fatal("empty totals must be nil (omitted from the payload)")
	}
}

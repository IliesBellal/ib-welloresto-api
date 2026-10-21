package fiscal

import (
	"testing"
)

// L'empreinte d'un paiement dépend de son saisisseur : sealedWith retrouve
// celui avec lequel elle a été calculée, et seulement celui-là. C'est ce qui
// permet à FindReassignedPayments de remonter à l'ancien user_id.
func TestSealedPayment_SealedWith(t *testing.T) {
	at := Now()
	comment := "ticket 12"
	sealed := func(userID string) sealedPayment {
		payload, err := NewPaymentPayload("212", "9001", 500, "ES", "SALE", at, userID, &comment)
		if err != nil {
			t.Fatalf("payload: %v", err)
		}
		hash, _, err := Seal(ChainPayments, "prev-hash", payload)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		return sealedPayment{
			CollectorRepair: CollectorRepair{MerchantID: "212", OrderID: "9001", MOP: "ES", hash: hash},
			amount:          500, op: "SALE", at: at, comment: &comment, prev: "prev-hash",
		}
	}

	p := sealed("itest-cashier")
	for userID, want := range map[string]bool{
		"itest-cashier": true,
		"itest-driver":  false,
		"":              false,
	} {
		got, err := p.sealedWith(userID)
		if err != nil {
			t.Fatalf("sealedWith(%q): %v", userID, err)
		}
		if got != want {
			t.Errorf("sealedWith(%q) = %v, want %v", userID, got, want)
		}
	}

	// Une autre donnée modifiée change l'empreinte, quel que soit le saisisseur.
	p.amount = 501
	if got, _ := p.sealedWith("itest-cashier"); got {
		t.Error("a modified amount must not verify")
	}
}

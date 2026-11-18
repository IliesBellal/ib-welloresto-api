package models

import "testing"

func TestAccountRegisterMOP(t *testing.T) {
	if got := AccountRegisterMOP("ES", 5000); got != "ACCOUNT_IN_ES" {
		t.Errorf("versement = %q", got)
	}
	if got := AccountRegisterMOP("TRANSFER", -5000); got != "ACCOUNT_OUT_TRANSFER" {
		t.Errorf("remboursement = %q", got)
	}
	// Tous les codes tiennent dans cash_registers_items.mop (varchar(20)).
	for _, mop := range AccountMoneyMOPs {
		for _, amount := range []int64{1, -1} {
			if code := AccountRegisterMOP(mop, amount); len(code) > 20 {
				t.Errorf("%q : %d caractères, max 20", code, len(code))
			}
		}
	}
}

func TestAccountRegisterLineMOP(t *testing.T) {
	cases := map[string]struct {
		mop string
		ok  bool
	}{
		"ACCOUNT_IN_ES":   {"ES", true},
		"ACCOUNT_OUT_CB":  {"CB", true},
		"ACCOUNT":         {"", false},
		"ES":              {"", false},
		"ACCOUNT_INVALID": {"", false},
	}
	for in, want := range cases {
		mop, ok := AccountRegisterLineMOP(in)
		if mop != want.mop || ok != want.ok {
			t.Errorf("AccountRegisterLineMOP(%q) = %q, %v ; want %q, %v", in, mop, ok, want.mop, want.ok)
		}
	}
}

func TestAccountRegisterLabel(t *testing.T) {
	cases := map[string]string{
		"ACCOUNT":              "Compte client",
		"ACCOUNT_IN_ES":        "Versement compte client · Espèces",
		"ACCOUNT_OUT_TRANSFER": "Remboursement compte client · Virement",
		"CB":                   "",
	}
	for in, want := range cases {
		if got := AccountRegisterLabel(in); got != want {
			t.Errorf("AccountRegisterLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

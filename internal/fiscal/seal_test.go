package fiscal

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Ce que jsonb fait d'un contenu (clés réordonnées, espaces retirés, nombres
// réécrits) ne doit pas changer sa forme canonique : c'est ce qui permet de
// recalculer une empreinte à partir de la ligne relue en base.
func TestCanonical_StableAcrossJSONBRewrites(t *testing.T) {
	cases := []struct{ written, readBack string }{
		{`{"b": 1, "a": [1, 2, {"d": "x", "c": null}]}`, `{"a": [1, 2, {"c": null, "d": "x"}], "b": 1}`},
		{`{"amount": 1e-06}`, `{"amount": 0.000001}`},
		{`{"rate": 1.50}`, `{"rate": 1.5}`},
		{`{"n": 1E3}`, `{"n": 1000}`},
		{`[{"name":"Café <crème> & co","qty":2}]`, `[{"qty": 2, "name": "Café <crème> & co"}]`},
		{`{"neg": -0.0}`, `{"neg": 0}`},
	}
	for _, c := range cases {
		a, err := Canonical([]byte(c.written))
		if err != nil {
			t.Fatalf("Canonical(%s): %v", c.written, err)
		}
		b, err := Canonical([]byte(c.readBack))
		if err != nil {
			t.Fatalf("Canonical(%s): %v", c.readBack, err)
		}
		if string(a) != string(b) {
			t.Errorf("canonical forms differ:\n  %s -> %s\n  %s -> %s", c.written, a, c.readBack, b)
		}
	}
}

func TestCanonical_EmptyIsNull(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(""), []byte("  "), []byte("null")} {
		got, err := Canonical(in)
		if err != nil || string(got) != "null" {
			t.Errorf("Canonical(%q) = %s, %v; want null", in, got, err)
		}
	}
}

func TestCanonical_Numbers(t *testing.T) {
	cases := map[string]string{
		`12`: `12`, `-7`: `-7`, `0.1`: `0.1`, `2.50`: `2.5`, `1e-06`: `0.000001`, `5.5e1`: `55`, `123456789012345678901234567890`: `123456789012345678901234567890`,
	}
	for in, want := range cases {
		got, err := Canonical([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("Canonical(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
}

func TestSeal_DeterministicAndBound(t *testing.T) {
	t.Setenv("FISCAL_SIGNING_KEY", "unit-test-key")
	at := time.Date(2026, 10, 7, 12, 30, 0, 123456789, time.UTC)
	p, err := NewPaymentPayload("42", "1001", 1500, "ES", "SALE", at, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	h1, s1, err := Seal(ChainPayments, GenesisHash, p)
	if err != nil {
		t.Fatal(err)
	}
	h2, s2, _ := Seal(ChainPayments, GenesisHash, p)
	if h1 != h2 || s1 != s2 {
		t.Fatalf("Seal not deterministic: %s/%s vs %s/%s", h1, s1, h2, s2)
	}
	if len(h1) != 64 || s1 == "" {
		t.Fatalf("unexpected hash/signature: %q / %q", h1, s1)
	}
	// Le parent, la chaîne et chaque donnée entrent dans l'empreinte.
	if h, _, _ := Seal(ChainPayments, "other-parent", p); h == h1 {
		t.Error("prev not bound to the hash")
	}
	if h, _, _ := Seal(ChainReceipts, GenesisHash, p); h == h1 {
		t.Error("chain not bound to the hash")
	}
	p.Amount++
	if h, _, _ := Seal(ChainPayments, GenesisHash, p); h == h1 {
		t.Error("amount not bound to the hash")
	}
}

func TestFormatTime_TruncatesToMicrosecondUTC(t *testing.T) {
	paris := time.FixedZone("Paris", 2*3600)
	at := time.Date(2026, 10, 7, 14, 30, 0, 123456789, paris)
	if got, want := FormatTime(at), "2026-10-07T12:30:00.123456Z"; got != want {
		t.Fatalf("FormatTime = %s, want %s", got, want)
	}
	if n := Now(); n.Nanosecond()%1000 != 0 || n.Location() != time.UTC {
		t.Fatalf("Now() not truncated to µs UTC: %v", n)
	}
}

func TestPrevOrGenesis(t *testing.T) {
	if PrevOrGenesis("") != GenesisHash || PrevOrGenesis("abc") != "abc" {
		t.Fatal("PrevOrGenesis")
	}
}

func TestLockChain_RequiresTransaction(t *testing.T) {
	if err := LockChain(context.Background(), ChainPayments, "42"); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("LockChain outside a transaction = %v, want ErrNoTransaction", err)
	}
}

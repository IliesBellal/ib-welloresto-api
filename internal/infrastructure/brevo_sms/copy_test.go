package brevo_sms

import "testing"

func TestIsCopyPhone(t *testing.T) {
	if !isCopyPhone(" +33 6 09 21 79 28 ") {
		t.Fatal("le numéro de la copie doit être reconnu malgré les espaces")
	}
	if isCopyPhone("+33612345678") {
		t.Fatal("un autre numéro ne doit pas être traité comme la copie")
	}
}

func TestCopyMessage(t *testing.T) {
	got := copyMessage("+33612345678", "Code 1234")
	want := "[copie → +33612345678] Code 1234"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

package customers

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeBlockPhone(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"vide", "", "", nil},
		{"espaces seuls", "   ", "", nil},
		{"national FR", "06 12 34 56 78", "+33612345678", nil},
		{"international", "+33 6 12 34 56 78", "+33612345678", nil},
		{"00 international", "0033612345678", "+33612345678", nil},
		{"trop court", "12 34", "", ErrBlockInvalidPhone},
		{"pas un numéro", "abc", "", ErrBlockInvalidPhone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBlockPhone(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeBlockEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"vide", "", "", nil},
		{"minuscules", "  Jean.Dupont@Example.COM ", "jean.dupont@example.com", nil},
		{"sans arobase", "jean.dupont", "", ErrBlockInvalidEmail},
		{"avec nom d'affichage", "Jean <jean@example.com>", "", ErrBlockInvalidEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBlockEmail(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlockedCustomerName(t *testing.T) {
	if got := blockedCustomerName("Jean", "Dupont", "+33612345678", "a@b.fr"); got != "Jean Dupont" {
		t.Errorf("prénom+nom: got %q", got)
	}
	if got := blockedCustomerName("", "Dupont", "+33612345678", ""); got != "Dupont" {
		t.Errorf("nom seul: got %q", got)
	}
	if got := blockedCustomerName("", "", "+33612345678", "a@b.fr"); got != "+33612345678" {
		t.Errorf("repli téléphone: got %q", got)
	}
	if got := blockedCustomerName("", "", "", "a@b.fr"); got != "a@b.fr" {
		t.Errorf("repli email: got %q", got)
	}
	long := strings.Repeat("é", 80)
	if got := blockedCustomerName(long, "", "", ""); len([]rune(got)) != 50 {
		t.Errorf("troncature à 50 caractères: got %d", len([]rune(got)))
	}
}

func TestIdentityWhere(t *testing.T) {
	where, args := identityWhere("m1", blockIdentity{CustomerID: "7", Tel: "+33612345678", Email: "A@B.fr"})
	wantWhere := "merchant_id = ? AND (customer_id = ? OR customer_tel = ? OR LOWER(customer_email) = ?)"
	if where != wantWhere {
		t.Errorf("where = %q, want %q", where, wantWhere)
	}
	if want := []interface{}{"m1", "7", "+33612345678", "a@b.fr"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}

	// Un champ vide est ignoré, jamais comparé à '' (qui matcherait les fiches
	// sans téléphone / sans email).
	where, args = identityWhere("m1", blockIdentity{Tel: "+33612345678"})
	if where != "merchant_id = ? AND (customer_tel = ?)" {
		t.Errorf("where = %q", where)
	}
	if want := []interface{}{"m1", "+33612345678"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestBlockIdentity_isEmpty(t *testing.T) {
	if !(blockIdentity{}).isEmpty() {
		t.Error("identité vide : isEmpty doit être vrai")
	}
	if (blockIdentity{Email: "a@b.fr"}).isEmpty() {
		t.Error("identité avec email : isEmpty doit être faux")
	}
}

func TestBlockErrorStatus(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{ErrBlockIdentityRequired, http.StatusBadRequest, "identity_required"},
		{ErrBlockInvalidPhone, http.StatusBadRequest, "invalid_phone"},
		{ErrBlockInvalidEmail, http.StatusBadRequest, "invalid_email"},
		{ErrBlockReasonInvalid, http.StatusBadRequest, "invalid_reason"},
		{ErrBlockCommentRequired, http.StatusBadRequest, "comment_required"},
		{ErrCustomerNotFound, http.StatusNotFound, "customer_not_found"},
	}
	for _, tt := range tests {
		status, code, ok := blockErrorStatus(tt.err)
		if !ok || status != tt.wantStatus || code != tt.wantCode {
			t.Errorf("%v: got (%d, %q, %v)", tt.err, status, code, ok)
		}
	}
	if _, _, ok := blockErrorStatus(errors.New("boom")); ok {
		t.Error("une erreur inattendue ne doit pas être traduite (500 via SendErrorJSON)")
	}
}

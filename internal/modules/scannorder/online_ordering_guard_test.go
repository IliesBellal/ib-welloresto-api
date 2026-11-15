package scannorder

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestOnlineOrderingEnabled(t *testing.T) {
	account := "acct_123"
	blank := "  "
	ready := func() *models.MerchantRow {
		return &models.MerchantRow{Activated: true, StorefrontEnabled: true, AccountID: &account}
	}

	cases := []struct {
		name      string
		row       func() *models.MerchantRow
		suspended bool
		want      bool
	}{
		{"compte Stripe, commande ouverte", ready, false, true},
		{"sans compte Stripe", func() *models.MerchantRow { r := ready(); r.AccountID = nil; return r }, false, false},
		{"identifiant Stripe vide", func() *models.MerchantRow { r := ready(); r.AccountID = &blank; return r }, false, false},
		{"commande coupée par l'établissement", func() *models.MerchantRow { r := ready(); r.Activated = false; return r }, false, false},
		{"carte masquée", func() *models.MerchantRow { r := ready(); r.StorefrontEnabled = false; return r }, false, false},
		{"abonnement suspendu", ready, true, false},
		{"marchand inconnu", func() *models.MerchantRow { return nil }, false, false},
	}
	for _, c := range cases {
		if got := onlineOrderingEnabled(c.row(), c.suspended); got != c.want {
			t.Errorf("%s : onlineOrderingEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

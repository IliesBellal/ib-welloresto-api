package scannorder

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/models"
)

// TestResolveOrderRef_RefusesInternalID verrouille la phase 2
// (docs/SCANNORDER_PUBLIC_ORDER_ID.md, D1) : un id interne séquentiel n'atteint
// jamais la base sur une route publique — sinon il suffirait de parcourir les
// numéros pour lire ou annuler la commande d'un autre client.
func TestResolveOrderRef_RefusesInternalID(t *testing.T) {
	s := &Service{} // pas de repo : le refus doit précéder toute requête
	for _, ref := range []string{"12345", "1", "", "0x1F", "order"} {
		if _, err := s.resolveOrderRef(context.Background(), "qr", ref); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("resolveOrderRef(%q) = %v, want ErrNotFound", ref, err)
		}
		if _, err := s.GetOrderSNO(context.Background(), "qr", ref); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("GetOrderSNO(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}

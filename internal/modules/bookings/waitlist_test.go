package bookings

import (
	"context"
	"errors"
	"testing"
	"welloresto-api/internal/models"
)

// Les validations d'entrée (nom, téléphone, party_size) court-circuitent avant
// tout accès base ; elles peuvent donc être testées sans repository.
func TestCreateWaitlistEntry_InvalidInput(t *testing.T) {
	svc := &BookingsService{}

	cases := []struct {
		name string
		req  CreateWaitlistRequest
	}{
		{"empty name", CreateWaitlistRequest{CustomerName: "  ", CustomerPhone: "+33612345678", PartySize: 2}},
		{"empty phone", CreateWaitlistRequest{CustomerName: "Jean", CustomerPhone: "", PartySize: 2}},
		{"zero party size", CreateWaitlistRequest{CustomerName: "Jean", CustomerPhone: "+33612345678", PartySize: 0}},
		{"negative party size", CreateWaitlistRequest{CustomerName: "Jean", CustomerPhone: "+33612345678", PartySize: -1}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.createWaitlistEntry(context.Background(), "m_1", c.req)
			if !errors.Is(err, models.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

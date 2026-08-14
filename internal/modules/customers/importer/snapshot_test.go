package importer

import (
	"testing"
	"time"
)

func TestPreviewSnapshotRoundTrip(t *testing.T) {
	email := "jean@example.com"
	phone := "+33612345678"
	business := "Ma Societe"
	birthdate := time.Date(1985, 5, 12, 0, 0, 0, 0, time.UTC)
	consent := true

	original := &PreviewSnapshot{
		MerchantID: "merchant-1",
		Provider:   ZeltySlug,
		CreatedAt:  time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC),
		Customers: []CanonicalCustomer{
			{
				ExternalID: "Z1", Name: "Jean Dupont", FirstName: "Jean", LastName: "Dupont",
				Email: &email, Phone: &phone,
				Address:            Address{Address: "12 rue de la Paix", FloorNumber: "3", DoorNumber: "12", AdditionalAddress: "Bat B"},
				BusinessName:       &business,
				Birthdate:          &birthdate,
				AdvertisingConsent: &consent,
				CreationDate:       &birthdate,
				SourceLine:         2,
			},
			{
				// Ligne aux champs pointeurs tous nil : le cas le plus courant
				// (Zelty tolerant, contact absent) doit survivre au round-trip.
				ExternalID: "Z2", Name: "Sans Contact", SourceLine: 3,
			},
		},
		Rows: []PreviewRow{
			{ExternalID: "Z1", SourceLine: 2, DisplayName: "Jean Dupont", Status: StatusCreate, Resolution: ResolutionCreate},
			{ExternalID: "Z2", SourceLine: 3, DisplayName: "Sans Contact", Status: StatusConflict,
				EmailCustomerID: 1, PhoneCustomerID: 2, Resolution: ResolutionSkip},
		},
	}

	payload, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := DecodePreviewSnapshot(payload)
	if err != nil {
		t.Fatalf("DecodePreviewSnapshot: %v", err)
	}

	if decoded.MerchantID != original.MerchantID {
		t.Fatalf("MerchantID = %q, want %q", decoded.MerchantID, original.MerchantID)
	}
	if decoded.Provider != original.Provider {
		t.Fatalf("Provider = %q, want %q", decoded.Provider, original.Provider)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if len(decoded.Customers) != 2 {
		t.Fatalf("Customers = %d, want 2", len(decoded.Customers))
	}

	c1 := decoded.Customers[0]
	if c1.ExternalID != "Z1" || c1.Name != "Jean Dupont" {
		t.Fatalf("Customers[0] = %+v", c1)
	}
	if c1.Email == nil || *c1.Email != email {
		t.Fatalf("Customers[0].Email = %v, want %q", c1.Email, email)
	}
	if c1.Phone == nil || *c1.Phone != phone {
		t.Fatalf("Customers[0].Phone = %v, want %q", c1.Phone, phone)
	}
	if c1.BusinessName == nil || *c1.BusinessName != business {
		t.Fatalf("Customers[0].BusinessName = %v, want %q", c1.BusinessName, business)
	}
	if c1.Birthdate == nil || !c1.Birthdate.Equal(birthdate) {
		t.Fatalf("Customers[0].Birthdate = %v, want %v", c1.Birthdate, birthdate)
	}
	if c1.AdvertisingConsent == nil || *c1.AdvertisingConsent != true {
		t.Fatalf("Customers[0].AdvertisingConsent = %v, want true", c1.AdvertisingConsent)
	}
	if c1.Address != original.Customers[0].Address {
		t.Fatalf("Customers[0].Address = %+v, want %+v", c1.Address, original.Customers[0].Address)
	}

	// Champs pointeurs nil : doivent rester nil, pas devenir des pointeurs sur
	// des valeurs zero.
	c2 := decoded.Customers[1]
	if c2.Email != nil || c2.Phone != nil || c2.BusinessName != nil || c2.Birthdate != nil ||
		c2.AdvertisingConsent != nil || c2.CreationDate != nil || c2.AdditionalInfo != nil || c2.DeliveryNotes != nil {
		t.Fatalf("Customers[1] devrait n'avoir que des pointeurs nil, got %+v", c2)
	}

	if len(decoded.Rows) != 2 {
		t.Fatalf("Rows = %d, want 2", len(decoded.Rows))
	}
	if decoded.Rows[1].Status != StatusConflict || decoded.Rows[1].EmailCustomerID != 1 || decoded.Rows[1].PhoneCustomerID != 2 {
		t.Fatalf("Rows[1] = %+v", decoded.Rows[1])
	}
}

func TestDecodePreviewSnapshotInvalidPayload(t *testing.T) {
	if _, err := DecodePreviewSnapshot("{pas du json"); err == nil {
		t.Fatal("DecodePreviewSnapshot(invalide) = nil, want une erreur")
	}
}

func TestPreviewSnapshotEncodeEmptySnapshot(t *testing.T) {
	snapshot := &PreviewSnapshot{}
	payload, err := snapshot.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := DecodePreviewSnapshot(payload)
	if err != nil {
		t.Fatalf("DecodePreviewSnapshot: %v", err)
	}
	if decoded.MerchantID != "" || len(decoded.Customers) != 0 {
		t.Fatalf("decoded = %+v, want vide", decoded)
	}
}

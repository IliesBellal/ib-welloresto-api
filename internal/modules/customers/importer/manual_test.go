package importer

import (
	"errors"
	"testing"
)

func TestBuildManualCustomerImportNominal(t *testing.T) {
	imp, err := BuildManualCustomerImport([]ManualCustomerInput{
		{
			Name: "Jean Dupont", FirstName: "Jean", LastName: "Dupont",
			Email: "jean.dupont@example.com", Phone: "0612345678",
			Address: "12 rue de la Paix", FloorNumber: "3", DoorNumber: "12", AdditionalAddress: "Bat B",
			BusinessName: "", Birthdate: "12/05/1985",
			AdditionalInfo: "Allergique aux noix", DeliveryNotes: "Livrer avant midi",
		},
	})
	if err != nil {
		t.Fatalf("BuildManualCustomerImport: %v", err)
	}
	if len(imp.Customers) != 1 {
		t.Fatalf("Customers = %d, want 1", len(imp.Customers))
	}

	c := imp.Customers[0]
	if c.Name != "Jean Dupont" {
		t.Fatalf("Name = %q", c.Name)
	}
	if c.Email == nil || *c.Email != "jean.dupont@example.com" {
		t.Fatalf("Email = %v", c.Email)
	}
	if c.Phone == nil {
		t.Fatal("Phone = nil")
	}
	if c.Birthdate == nil || c.Birthdate.Format("2006-01-02") != "1985-05-12" {
		t.Fatalf("Birthdate = %v", c.Birthdate)
	}
	// AdvertisingConsent nil en entree -> false explicite au canonique.
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != false {
		t.Fatalf("AdvertisingConsent = %v, want false", c.AdvertisingConsent)
	}
	if c.ExternalID == "" {
		t.Fatal("ExternalID vide")
	}
}

func TestBuildManualCustomerImportRequiresName(t *testing.T) {
	_, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Email: "jean@example.com"},
	})
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("BuildManualCustomerImport(sans nom) = %v, want *RowError", err)
	}
}

func TestBuildManualCustomerImportRequiresEmailOrPhone(t *testing.T) {
	_, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Name: "Jean Dupont"},
	})
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("BuildManualCustomerImport(sans email ni telephone) = %v, want *RowError", err)
	}
}

func TestBuildManualCustomerImportMalformedEmail(t *testing.T) {
	_, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Name: "Jean Dupont", Email: "pas-un-email", Phone: "0612345678"},
	})
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("BuildManualCustomerImport(email malforme) = %v, want *RowError", err)
	}
}

func TestBuildManualCustomerImportIntraBatchCollision(t *testing.T) {
	_, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Name: "Jean Dupont", Email: "jean.dupont@example.com"},
		{Name: "Jean D.", Email: "jean.dupont@example.com"},
	})
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("BuildManualCustomerImport(email duplique) = %v, want *RowError", err)
	}
	if rowErr.Line != 2 {
		t.Fatalf("RowError.Line = %d, want 2 (2e entree en collision)", rowErr.Line)
	}
}

func TestBuildManualCustomerImportConsentNilMeansFalse(t *testing.T) {
	imp, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Name: "Jean Dupont", Email: "jean.dupont@example.com", AdvertisingConsent: nil},
	})
	if err != nil {
		t.Fatalf("BuildManualCustomerImport: %v", err)
	}
	c := imp.Customers[0]
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != false {
		t.Fatalf("AdvertisingConsent = %v, want false", c.AdvertisingConsent)
	}
}

func TestBuildManualCustomerImportConsentExplicitTrue(t *testing.T) {
	trueVal := true
	imp, err := BuildManualCustomerImport([]ManualCustomerInput{
		{Name: "Jean Dupont", Email: "jean.dupont@example.com", AdvertisingConsent: &trueVal},
	})
	if err != nil {
		t.Fatalf("BuildManualCustomerImport: %v", err)
	}
	c := imp.Customers[0]
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != true {
		t.Fatalf("AdvertisingConsent = %v, want true", c.AdvertisingConsent)
	}
}

func TestBuildManualCustomerImportEmpty(t *testing.T) {
	_, err := BuildManualCustomerImport(nil)
	if !errors.Is(err, ErrNoCustomers) {
		t.Fatalf("BuildManualCustomerImport(vide) = %v, want ErrNoCustomers", err)
	}
}

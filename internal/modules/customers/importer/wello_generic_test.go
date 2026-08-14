package importer

import (
	"errors"
	"testing"
)

var welloGenericCustomerHeader = []string{
	"Nom", "Prenom", "Nom de famille", "Email", "Telephone",
	"Adresse", "Etage", "Porte", "Complement d'adresse",
	"Raison sociale", "Date de naissance", "Infos complementaires",
	"Notes de livraison", "Consentement marketing",
}

func TestWelloGenericCustomerProviderSlug(t *testing.T) {
	if got := NewWelloGenericCustomerProvider().Slug(); got != WelloGenericSlug {
		t.Fatalf("Slug() = %q, want %q", got, WelloGenericSlug)
	}
}

func TestWelloGenericCustomerNominal(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{
			"Jean Dupont", "Jean", "Dupont", "jean.dupont@example.com", "0612345678",
			"12 rue de la Paix", "3", "12", "Bat B",
			"", "12/05/1985", "Allergique aux noix",
			"Livrer avant midi", "Oui",
		},
	}

	imp, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(imp.Customers) != 1 {
		t.Fatalf("Customers = %d, want 1", len(imp.Customers))
	}

	c := imp.Customers[0]
	if c.Name != "Jean Dupont" {
		t.Fatalf("Name = %q, want %q", c.Name, "Jean Dupont")
	}
	if c.FirstName != "Jean" || c.LastName != "Dupont" {
		t.Fatalf("FirstName/LastName = %q/%q", c.FirstName, c.LastName)
	}
	if c.Email == nil || *c.Email != "jean.dupont@example.com" {
		t.Fatalf("Email = %v", c.Email)
	}
	if c.Phone == nil {
		t.Fatal("Phone = nil, want une valeur")
	}
	if c.Address.Address != "12 rue de la Paix" || c.Address.FloorNumber != "3" || c.Address.DoorNumber != "12" || c.Address.AdditionalAddress != "Bat B" {
		t.Fatalf("Address = %+v", c.Address)
	}
	if c.AdditionalInfo == nil || *c.AdditionalInfo != "Allergique aux noix" {
		t.Fatalf("AdditionalInfo = %v", c.AdditionalInfo)
	}
	if c.DeliveryNotes == nil || *c.DeliveryNotes != "Livrer avant midi" {
		t.Fatalf("DeliveryNotes = %v", c.DeliveryNotes)
	}
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != true {
		t.Fatalf("AdvertisingConsent = %v, want true", c.AdvertisingConsent)
	}
	if c.Birthdate == nil || c.Birthdate.Format("2006-01-02") != "1985-05-12" {
		t.Fatalf("Birthdate = %v", c.Birthdate)
	}
	if c.ExternalID == "" {
		t.Fatal("ExternalID vide")
	}
}

func TestWelloGenericCustomerMissingNameColumn(t *testing.T) {
	rows := [][]string{
		{"Email", "Telephone"},
		{"jean@example.com", "0612345678"},
	}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	if !errors.Is(err, ErrMissingColumn) {
		t.Fatalf("Parse(sans colonne Nom) = %v, want ErrMissingColumn", err)
	}
}

func TestWelloGenericCustomerRequiresEmailOrPhone(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{"Jean Dupont", "Jean", "Dupont", "", "", "", "", "", "", "", "", "", "", ""},
	}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("Parse(sans email ni telephone) = %v, want *RowError", err)
	}
}

func TestWelloGenericCustomerRequiresName(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{"", "Jean", "Dupont", "jean@example.com", "", "", "", "", "", "", "", "", "", ""},
	}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("Parse(sans nom) = %v, want *RowError", err)
	}
}

func TestWelloGenericCustomerMalformedEmail(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{"Jean Dupont", "Jean", "Dupont", "pas-un-email", "0612345678", "", "", "", "", "", "", "", "", ""},
	}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("Parse(email malforme) = %v, want *RowError", err)
	}
}

func TestWelloGenericCustomerIntraFileCollision(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{"Jean Dupont", "Jean", "Dupont", "jean.dupont@example.com", "", "", "", "", "", "", "", "", "", ""},
		{"Jean D.", "Jean", "D.", "jean.dupont@example.com", "", "", "", "", "", "", "", "", "", ""},
	}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	var rowErr *RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("Parse(email duplique) = %v, want *RowError", err)
	}
	if rowErr.Line != 3 {
		t.Fatalf("RowError.Line = %d, want 3 (2e ligne en collision)", rowErr.Line)
	}
}

func TestWelloGenericCustomerEmptyConsentMeansFalse(t *testing.T) {
	rows := [][]string{
		welloGenericCustomerHeader,
		{"Jean Dupont", "Jean", "Dupont", "jean.dupont@example.com", "", "", "", "", "", "", "", "", "", ""},
	}

	imp, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := imp.Customers[0]
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != false {
		t.Fatalf("AdvertisingConsent = %v, want false (consentement vide)", c.AdvertisingConsent)
	}
}

func TestWelloGenericCustomerNoRows(t *testing.T) {
	rows := [][]string{welloGenericCustomerHeader}

	_, err := NewWelloGenericCustomerProvider().Parse(buildXLSX(t, rows))
	if !errors.Is(err, ErrNoCustomers) {
		t.Fatalf("Parse(sans ligne) = %v, want ErrNoCustomers", err)
	}
}

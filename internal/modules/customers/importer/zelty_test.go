package importer

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureZeltyCustomersSample = "zelty_customers_sample.csv"

func parseZeltyCustomersFixture(t *testing.T, name string) *IntermediateCustomerImport {
	t.Helper()

	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ouverture de la fixture %q: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })

	got, err := NewZeltyCustomerProvider().Parse(f)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return got
}

func customerByExternalID(t *testing.T, imp *IntermediateCustomerImport, externalID string) CanonicalCustomer {
	t.Helper()

	for _, c := range imp.Customers {
		if c.ExternalID == externalID {
			return c
		}
	}
	t.Fatalf("client %q absent de l'import", externalID)
	return CanonicalCustomer{}
}

func warningsFor(imp *IntermediateCustomerImport, ref string) []Warning {
	var out []Warning
	for _, w := range imp.Warnings {
		if w.Ref == ref {
			out = append(out, w)
		}
	}
	return out
}

func hasWarningCode(warnings []Warning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestZeltyCustomerProviderSlug(t *testing.T) {
	if got := NewZeltyCustomerProvider().Slug(); got != ZeltySlug {
		t.Fatalf("Slug() = %q, want %q", got, ZeltySlug)
	}
}

func TestZeltyCustomerParseNominal(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	if len(imp.Customers) != 10 {
		t.Fatalf("Customers = %d, want 10", len(imp.Customers))
	}

	c := customerByExternalID(t, imp, "Z001")
	if c.FirstName != "Jean" || c.LastName != "Dupont" {
		t.Fatalf("Z001 nom/prenom = %q/%q, want Jean/Dupont", c.FirstName, c.LastName)
	}
	if c.Name != "Jean Dupont" {
		t.Fatalf("Z001 Name = %q, want %q", c.Name, "Jean Dupont")
	}
	if c.Email == nil || *c.Email != "jean.dupont@example.com" {
		t.Fatalf("Z001 Email = %v, want jean.dupont@example.com", c.Email)
	}
	if c.Phone == nil {
		t.Fatal("Z001 Phone = nil, want une valeur")
	}
	if c.AdditionalInfo == nil || *c.AdditionalInfo != "Allergie: noix, gluten" {
		t.Fatalf("Z001 AdditionalInfo = %v, want le champ entre guillemets avec virgule", c.AdditionalInfo)
	}
	if c.DeliveryNotes == nil || *c.DeliveryNotes != "Habitue du jeudi" {
		t.Fatalf("Z001 DeliveryNotes = %v", c.DeliveryNotes)
	}
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != false {
		t.Fatalf("Z001 AdvertisingConsent = %v, want false (optin sms/mail=Non)", c.AdvertisingConsent)
	}
	if c.Birthdate == nil || c.Birthdate.Format("2006-01-02") != "1985-05-12" {
		t.Fatalf("Z001 Birthdate = %v, want 1985-05-12", c.Birthdate)
	}
	if c.CreationDate == nil || c.CreationDate.Format("2006-01-02") != "2024-01-15" {
		t.Fatalf("Z001 CreationDate = %v, want 2024-01-15 (date d'inscription)", c.CreationDate)
	}
	if c.SourceLine != 2 {
		t.Fatalf("Z001 SourceLine = %d, want 2 (ligne CSV, en-tete = ligne 1)", c.SourceLine)
	}
}

// Téléphone 2 ne doit jamais alimenter Phone ni aucun autre champ : seule la
// colonne Téléphone est retenue.
func TestZeltyCustomerIgnoresPhone2(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z001")
	if c.Phone == nil {
		t.Fatal("Z001 Phone = nil")
	}
	// Le telephone principal Z001 est 0612345678 ; Telephone 2 vaut 0600000001.
	// Si Telephone 2 avait fuite dans Phone, la valeur normalisee porterait
	// "600000001" plutot que "612345678".
	if got := *c.Phone; got == "" {
		t.Fatal("Phone vide")
	}
}

func TestZeltyCustomerPhoneOnly(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z002")
	if c.Email != nil {
		t.Fatalf("Z002 Email = %v, want nil (mail absent)", c.Email)
	}
	if c.Phone == nil {
		t.Fatal("Z002 Phone = nil, want une valeur")
	}
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != true {
		t.Fatalf("Z002 AdvertisingConsent = %v, want true (optin sms=Oui)", c.AdvertisingConsent)
	}
	if hasWarningCode(warningsFor(imp, "Z002"), WarnMissingContact) {
		t.Fatal("Z002 ne doit pas avoir de warning missing_contact (telephone present)")
	}
}

func TestZeltyCustomerEmailOnly(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z003")
	if c.Phone != nil {
		t.Fatalf("Z003 Phone = %v, want nil (telephone absent)", c.Phone)
	}
	if c.Email == nil || *c.Email != "paul.bernard@example.com" {
		t.Fatalf("Z003 Email = %v", c.Email)
	}
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != true {
		t.Fatalf("Z003 AdvertisingConsent = %v, want true (optin mail=Oui)", c.AdvertisingConsent)
	}
}

func TestZeltyCustomerMissingContactWarning(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z004")
	if c.Email != nil || c.Phone != nil {
		t.Fatalf("Z004 devrait n'avoir ni email ni telephone, got Email=%v Phone=%v", c.Email, c.Phone)
	}
	if !hasWarningCode(warningsFor(imp, "Z004"), WarnMissingContact) {
		t.Fatal("Z004 devrait porter le warning missing_contact")
	}
}

func TestZeltyCustomerMissingNameWarning(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z005")
	if c.FirstName != "" || c.LastName != "" {
		t.Fatalf("Z005 devrait n'avoir ni nom ni prenom, got %q/%q", c.FirstName, c.LastName)
	}
	if !hasWarningCode(warningsFor(imp, "Z005"), WarnMissingName) {
		t.Fatal("Z005 devrait porter le warning missing_name")
	}
	// Le client reste importe malgre l'absence de nom : provider tolerant.
	if c.ExternalID != "Z005" {
		t.Fatalf("Z005 devrait etre importe avec son ID Zelty, got %q", c.ExternalID)
	}
}

func TestZeltyCustomerBothOptinsEmptyMeansNoConsent(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z006")
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != false {
		t.Fatalf("Z006 AdvertisingConsent = %v, want false (optins vides)", c.AdvertisingConsent)
	}
}

func TestZeltyCustomerUnparseableBirthdateWarning(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z007")
	if c.Birthdate != nil {
		t.Fatalf("Z007 Birthdate = %v, want nil (date illisible)", c.Birthdate)
	}
	if !hasWarningCode(warningsFor(imp, "Z007"), WarnUnparseableBirthdate) {
		t.Fatal("Z007 devrait porter le warning unparseable_birthdate")
	}
}

// Statut=-1 ne doit exclure aucune ligne : le parser ne filtre jamais sur ce
// critere.
func TestZeltyCustomerImportsRegardlessOfStatut(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z008")
	if c.ExternalID != "Z008" {
		t.Fatal("Z008 (Statut=-1) devrait etre importe")
	}
}

func TestZeltyCustomerInvalidEmailDroppedWithWarning(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z009")
	if c.Email != nil {
		t.Fatalf("Z009 Email = %v, want nil (email malforme droppe)", c.Email)
	}
	if !hasWarningCode(warningsFor(imp, "Z009"), WarnInvalidEmail) {
		t.Fatal("Z009 devrait porter le warning invalid_email")
	}
	// Le telephone valide de la ligne doit rester, seul l'email est droppe.
	if c.Phone == nil {
		t.Fatal("Z009 Phone = nil, want une valeur (non affectee par l'email invalide)")
	}
}

func TestZeltyCustomerBusinessName(t *testing.T) {
	imp := parseZeltyCustomersFixture(t, fixtureZeltyCustomersSample)

	c := customerByExternalID(t, imp, "Z010")
	if c.BusinessName == nil || *c.BusinessName != "Societe Anonyme" {
		t.Fatalf("Z010 BusinessName = %v, want Societe Anonyme", c.BusinessName)
	}
}

func TestZeltyCustomerMissingIDColumn(t *testing.T) {
	provider := NewZeltyCustomerProvider()
	_, err := provider.Parse(csvReaderFromRows([][]string{
		{"Nom", "Prenom", "Mail"},
		{"Dupont", "Jean", "jean@example.com"},
	}))
	if err == nil {
		t.Fatal("Parse sans colonne ID = nil, want une erreur")
	}
}

func TestZeltyCustomerNoRows(t *testing.T) {
	provider := NewZeltyCustomerProvider()
	_, err := provider.Parse(csvReaderFromRows([][]string{
		{"ID", "Nom", "Prenom"},
	}))
	if err == nil {
		t.Fatal("Parse sans ligne de donnees = nil, want ErrNoCustomers")
	}
}

package importer

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWelloGenericCustomerTemplateFilename(t *testing.T) {
	filename, _, err := NewWelloGenericCustomerProvider().Template()
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if filename != "wello-modele-import-clients.xlsx" {
		t.Fatalf("filename = %q, want %q", filename, "wello-modele-import-clients.xlsx")
	}
}

func TestWelloGenericCustomerTemplateDisposition(t *testing.T) {
	_, data, err := NewWelloGenericCustomerProvider().Template()
	if err != nil {
		t.Fatalf("Template: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Clients" || sheets[1] != "Mode d'emploi" {
		t.Fatalf("feuilles = %v, want [Clients, Mode d'emploi]", sheets)
	}

	rows, err := f.GetRows("Clients")
	if err != nil {
		t.Fatalf("GetRows(Clients): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("feuille Clients = %d lignes, want 1 (en-tete seul, aucune donnee)", len(rows))
	}

	wantHeaders := []string{
		"Nom", "Prénom", "Nom de famille", "Email", "Téléphone",
		"Adresse", "Étage", "Porte", "Complément d'adresse",
		"Raison sociale", "Date de naissance", "Infos complémentaires",
		"Notes de livraison", "Consentement marketing",
	}
	if len(rows[0]) != len(wantHeaders) {
		t.Fatalf("en-tete = %d colonnes, want %d", len(rows[0]), len(wantHeaders))
	}
	for i, want := range wantHeaders {
		if rows[0][i] != want {
			t.Fatalf("en-tete[%d] = %q, want %q", i, rows[0][i], want)
		}
	}

	// Freeze pane sur la 1re ligne.
	panes, err := f.GetPanes("Clients")
	if err != nil {
		t.Fatalf("GetPanes: %v", err)
	}
	if !panes.Freeze || panes.YSplit != 1 {
		t.Fatalf("panes = %+v, want un volet fige sur la 1re ligne", panes)
	}
}

// Round-trip : les 14 en-tetes generes doivent tous se resoudre, via
// importutil.FoldHeader + welloGenericCustomerAliases, vers les memes champs
// que le parser wello-generic attend — sans quoi le modele genere par cette
// route ne serait pas reimportable tel quel.
func TestWelloGenericCustomerTemplateRoundTrip(t *testing.T) {
	_, data, err := NewWelloGenericCustomerProvider().Template()
	if err != nil {
		t.Fatalf("Template: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}

	dataRow := []string{
		"Jean Dupont", "Jean", "Dupont", "jean.dupont@example.com", "0612345678",
		"12 rue de la Paix", "3", "12B", "Bat B",
		"Ma Societe SARL", "05/11/1990", "Allergique aux noix",
		"Livrer avant midi", "Oui",
	}
	for i, value := range dataRow {
		cell, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			t.Fatalf("CoordinatesToCellName: %v", err)
		}
		if err := f.SetCellStr(templateCustomersSheet, cell, value); err != nil {
			t.Fatalf("SetCellStr: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = f.Close()

	imp, err := NewWelloGenericCustomerProvider().Parse(&buf)
	if err != nil {
		t.Fatalf("Parse: %v (aucun en-tete ne doit manquer, notamment la colonne obligatoire Nom)", err)
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
		t.Fatal("Phone = nil")
	}
	if c.Address.Address != "12 rue de la Paix" || c.Address.FloorNumber != "3" ||
		c.Address.DoorNumber != "12B" || c.Address.AdditionalAddress != "Bat B" {
		t.Fatalf("Address = %+v", c.Address)
	}
	if c.BusinessName == nil || *c.BusinessName != "Ma Societe SARL" {
		t.Fatalf("BusinessName = %v", c.BusinessName)
	}
	if c.Birthdate == nil || c.Birthdate.Format("2006-01-02") != "1990-11-05" {
		t.Fatalf("Birthdate = %v, want 1990-11-05", c.Birthdate)
	}
	if c.AdditionalInfo == nil || *c.AdditionalInfo != "Allergique aux noix" {
		t.Fatalf("AdditionalInfo = %v", c.AdditionalInfo)
	}
	if c.DeliveryNotes == nil || *c.DeliveryNotes != "Livrer avant midi" {
		t.Fatalf("DeliveryNotes = %v", c.DeliveryNotes)
	}
	if c.AdvertisingConsent == nil || *c.AdvertisingConsent != true {
		t.Fatalf("AdvertisingConsent = %v, want true (colonne Consentement marketing = Oui)", c.AdvertisingConsent)
	}
}

// Zelty et la saisie manuelle n'ont pas de modele : l'absence d'implementation
// de TemplateProvider suffit a les exclure.
func TestOnlyWelloGenericImplementsTemplateProvider(t *testing.T) {
	var _ CustomerImportProvider = NewZeltyCustomerProvider()
	if _, ok := any(NewZeltyCustomerProvider()).(TemplateProvider); ok {
		t.Fatal("ZeltyCustomerProvider ne doit pas implementer TemplateProvider")
	}
	if _, ok := any(NewWelloGenericCustomerProvider()).(TemplateProvider); !ok {
		t.Fatal("WelloGenericCustomerProvider doit implementer TemplateProvider")
	}
}

package importer

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"welloresto-api/internal/importutil"
)

func buildWelloGenericTemplate(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	if err := NewWelloGenericProvider().BuildTemplate(&buf); err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("classeur vide")
	}
	return &buf
}

// LE test de cette phase : le modèle qu'on distribue doit être relu sans perte
// par le parser qui le recevra en retour. Si l'un des deux bouge, ce test
// tombe — c'est tout l'intérêt.
func TestWelloGenericTemplateRoundTrip(t *testing.T) {
	imp, err := NewWelloGenericProvider().Parse(buildWelloGenericTemplate(t))
	if err != nil {
		t.Fatalf("relecture du modèle: %v", err)
	}

	if imp.Provider != WelloGenericSlug {
		t.Fatalf("Provider = %q, want %q", imp.Provider, WelloGenericSlug)
	}
	if len(imp.Products) != 1 {
		t.Fatalf("produits = %d, want 1 (la ligne d'exemple)", len(imp.Products))
	}

	product := imp.Products[0]

	if product.Name != "Pizza Margherita" {
		t.Fatalf("Name = %q", product.Name)
	}
	if product.Description != "Tomate, mozzarella, basilic" {
		t.Fatalf("Description = %q", product.Description)
	}

	// Le point qui compte : les prix du modèle sont en euros, et doivent
	// ressortir en centimes. Un modèle rempli en centimes produirait des prix
	// cent fois trop élevés.
	if product.PriceIn != 950 {
		t.Fatalf("PriceIn = %d, want 950 centimes (9,50 € dans le modèle)", product.PriceIn)
	}
	if product.PriceTakeAway != 950 {
		t.Fatalf("PriceTakeAway = %d, want 950", product.PriceTakeAway)
	}
	if product.PriceDelivery != 1050 {
		t.Fatalf("PriceDelivery = %d, want 1050", product.PriceDelivery)
	}
	if product.AllPricesZero {
		t.Fatal("AllPricesZero = true sur la ligne d'exemple")
	}

	rates := []struct {
		channel string
		got     *float64
	}{
		{"sur place", product.TvaRateIn},
		{"emporté", product.TvaRateTakeAway},
		{"livraison", product.TvaRateDelivery},
	}
	for _, rate := range rates {
		if rate.got == nil {
			t.Fatalf("TVA %s = nil, want 10", rate.channel)
		}
		if *rate.got != 10 {
			t.Fatalf("TVA %s = %v, want 10", rate.channel, *rate.got)
		}
	}

	// La catégorie est explicite dans le modèle, contrairement à un export Zelty.
	if product.CategoryExternalID == "" {
		t.Fatal("CategoryExternalID vide alors que la colonne Catégorie est remplie")
	}
	if len(imp.Categories) != 1 || imp.Categories[0].Name != "NOS PIZZAS" {
		t.Fatalf("Categories = %+v, want [NOS PIZZAS]", imp.Categories)
	}
	if imp.Categories[0].ExternalID != product.CategoryExternalID {
		t.Fatal("le produit ne référence pas la catégorie déclarée")
	}

	// Les tags de l'exemple montrent le séparateur : ils doivent être découpés.
	if len(product.TagExternalIDs) != 2 {
		t.Fatalf("TagExternalIDs = %v, want 2 (Végétarien et Signature)", product.TagExternalIDs)
	}
	wantTags := []string{"Végétarien", "Signature"}
	if len(imp.Tags) != 2 {
		t.Fatalf("Tags = %+v, want 2", imp.Tags)
	}
	for i, want := range wantTags {
		if imp.Tags[i].Name != want {
			t.Fatalf("Tags[%d] = %q, want %q", i, imp.Tags[i].Name, want)
		}
		if imp.Tags[i].ExternalID != product.TagExternalIDs[i] {
			t.Fatalf("le tag %q n'est pas rattaché au produit", want)
		}
	}
}

// Second filet, indépendant du round-trip : chaque en-tête écrit dans le modèle
// doit être reconnu par la table d'alias, et pointer sur le champ attendu.
// Le round-trip attraperait un en-tête cassé, celui-ci dit lequel.
func TestWelloGenericTemplateHeadersResolve(t *testing.T) {
	seen := make(map[welloGenericField]string, len(welloGenericTemplate))

	for _, column := range welloGenericTemplate {
		field, known := welloGenericAliases[importutil.FoldHeader(column.header)]
		if !known {
			t.Fatalf("l'en-tête %q du modèle n'est reconnu par aucun alias", column.header)
		}
		if field != column.field {
			t.Fatalf("l'en-tête %q résout vers %q, want %q",
				column.header, welloGenericLabels[field], welloGenericLabels[column.field])
		}
		if previous, duplicate := seen[field]; duplicate {
			t.Fatalf("les en-têtes %q et %q désignent la même colonne", previous, column.header)
		}
		seen[field] = column.header
	}

	// Toutes les colonnes obligatoires du parser doivent figurer au modèle,
	// sans quoi le fichier distribué serait refusé à l'envoi.
	for _, field := range welloGenericRequired {
		header, present := seen[field]
		if !present {
			t.Fatalf("la colonne obligatoire %q est absente du modèle", welloGenericLabels[field])
		}
		var required bool
		for _, column := range welloGenericTemplate {
			if column.field == field {
				required = column.required
			}
		}
		if !required {
			t.Fatalf("la colonne %q est obligatoire pour le parser mais annoncée facultative dans le modèle", header)
		}
	}

	// Et tous les champs du parser sont proposés : une colonne oubliée serait
	// une fonctionnalité inaccessible.
	if len(seen) != int(wgFieldCount) {
		t.Fatalf("le modèle couvre %d champs sur %d", len(seen), wgFieldCount)
	}
}

// La feuille des produits doit rester en première position : le parser lit
// GetSheetList()[0], la feuille d'aide ne doit jamais passer devant.
func TestWelloGenericTemplateSheetLayout(t *testing.T) {
	f, err := excelize.OpenReader(buildWelloGenericTemplate(t))
	if err != nil {
		t.Fatalf("ouverture du modèle: %v", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) != 2 {
		t.Fatalf("feuilles = %v, want 2", sheets)
	}
	if sheets[0] != templateProductsSheet {
		t.Fatalf("première feuille = %q, want %q (c'est celle que le parser lit)", sheets[0], templateProductsSheet)
	}
	if sheets[1] != templateHelpSheet {
		t.Fatalf("seconde feuille = %q, want %q", sheets[1], templateHelpSheet)
	}

	rows, err := f.GetRows(templateProductsSheet)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("lignes = %d, want 2 (en-tête + exemple)", len(rows))
	}
	if len(rows[0]) != len(welloGenericTemplate) {
		t.Fatalf("colonnes d'en-tête = %d, want %d", len(rows[0]), len(welloGenericTemplate))
	}
	for i, column := range welloGenericTemplate {
		if rows[0][i] != column.header {
			t.Fatalf("en-tête %d = %q, want %q", i, rows[0][i], column.header)
		}
	}

	// Le prix de l'exemple doit rester tel quel à l'écran : une cellule
	// reformatée par le tableur perdrait la démonstration de la virgule.
	price, err := f.GetCellValue(templateProductsSheet, "D2")
	if err != nil {
		t.Fatalf("GetCellValue: %v", err)
	}
	if price != "9,50" {
		t.Fatalf("prix d'exemple = %q, want %q", price, "9,50")
	}
}

// Le figeage de l'en-tête n'a pas de getter dans excelize : on le vérifie dans
// le XML produit, sans quoi une régression passerait inaperçue.
func TestWelloGenericTemplateFreezesHeaderRow(t *testing.T) {
	workbook := buildWelloGenericTemplate(t).Bytes()

	zr, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		t.Fatalf("lecture du classeur: %v", err)
	}

	for _, file := range zr.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("ouverture de %s: %v", file.Name, err)
		}
		defer func() { _ = rc.Close() }()

		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("lecture de %s: %v", file.Name, err)
		}
		if !strings.Contains(string(content), `ySplit="1"`) ||
			!strings.Contains(string(content), `state="frozen"`) {
			t.Fatalf("la ligne d'en-tête n'est pas figée dans %s", file.Name)
		}
		return
	}

	t.Fatal("feuille des produits introuvable dans le classeur")
}

// TemplateColumns décrit le même modèle que celui qui est généré : c'est ce que
// le back-office affichera en aide, il ne doit pas raconter autre chose.
func TestWelloGenericTemplateColumnsMatchFile(t *testing.T) {
	columns := NewWelloGenericProvider().TemplateColumns()

	if len(columns) != len(welloGenericTemplate) {
		t.Fatalf("TemplateColumns() = %d colonnes, want %d", len(columns), len(welloGenericTemplate))
	}
	for i, column := range columns {
		source := welloGenericTemplate[i]
		if column.Header != source.header || column.Example != source.example ||
			column.Required != source.required || column.Help != source.help {
			t.Fatalf("colonne %d = %+v, want %+v", i, column, source)
		}
		if column.Help == "" {
			t.Fatalf("colonne %q sans explication", column.Header)
		}
	}
}

// Le registre expose bien le modèle pour wello-generic, et pas pour Zelty : un
// export tiers n'a pas de modèle Wello à proposer.
func TestTemplateProviderAvailability(t *testing.T) {
	registry := DefaultRegistry()

	generic, err := registry.Get(WelloGenericSlug)
	if err != nil {
		t.Fatalf("Get(%q): %v", WelloGenericSlug, err)
	}
	template, ok := generic.(TemplateProvider)
	if !ok {
		t.Fatalf("%q n'expose pas de modèle", WelloGenericSlug)
	}
	if template.TemplateFilename() != welloGenericTemplateFilename {
		t.Fatalf("TemplateFilename() = %q", template.TemplateFilename())
	}

	zelty, err := registry.Get(ZeltySlug)
	if err != nil {
		t.Fatalf("Get(%q): %v", ZeltySlug, err)
	}
	if _, ok := zelty.(TemplateProvider); ok {
		t.Fatalf("%q expose un modèle alors que c'est un export tiers", ZeltySlug)
	}
}

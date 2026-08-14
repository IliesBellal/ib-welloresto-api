package importer

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Noms des feuilles et du fichier du modèle. La feuille des clients est en
// première position : c'est celle que WelloGenericCustomerProvider.Parse relit
// (importutil.ReadSheetRows lit GetSheetList()[0]).
const (
	templateCustomersSheet = "Clients"
	templateHelpSheet      = "Mode d'emploi"
	templateFilename       = "wello-modele-import-clients.xlsx"
)

// TemplateProvider est implémenté par les providers capables de fournir un
// modèle vierge à remplir.
//
// Interface distincte de CustomerImportProvider, et volontairement
// facultative : Zelty est un export produit par un logiciel tiers, la saisie
// manuelle n'a pas de fichier du tout — il n'y a pas de modèle Wello à
// proposer pour eux. L'absence d'implémentation suffit à les exclure, sans
// liste de providers en dur à maintenir quelque part (même parti pris que
// menu/importer.TemplateProvider).
type TemplateProvider interface {
	Slug() string
	Template() (filename string, data []byte, err error)
}

// templateColumn décrit une colonne du modèle et sa documentation dans la
// feuille d'aide.
type templateColumn struct {
	header        string
	requiredLabel string // "Obligatoire", "Obligatoire*" ou "Facultatif"
	example       string
	width         float64
	help          string
}

// welloGenericTemplateColumns décrit le modèle colonne par colonne, dans
// l'ordre du fichier. Les libellés doivent se résoudre, via
// importutil.FoldHeader et welloGenericCustomerAliases (wello_generic.go),
// vers les mêmes champs internes que le parser attend — verrouillé par
// TestWelloGenericCustomerTemplateRoundTrip.
var welloGenericTemplateColumns = []templateColumn{
	{
		header: "Nom", requiredLabel: "Obligatoire", width: 22,
		example: "Jean Dupont",
		help:    "Obligatoire. Nom d'affichage du client.",
	},
	{
		header: "Prénom", requiredLabel: "Facultatif", width: 16,
		example: "Jean",
		help:    "Facultatif.",
	},
	{
		header: "Nom de famille", requiredLabel: "Facultatif", width: 18,
		example: "Dupont",
		help:    "Facultatif.",
	},
	{
		header: "Email", requiredLabel: "Obligatoire*", width: 28,
		example: "jean.dupont@email.fr",
		help:    "Obligatoire si aucun téléphone n'est renseigné sur la ligne. Adresse email valide.",
	},
	{
		header: "Téléphone", requiredLabel: "Obligatoire*", width: 18,
		example: "0612345678",
		help:    "Obligatoire si aucun email n'est renseigné sur la ligne. Format FR (0612345678) ou international (+33612345678).",
	},
	{
		header: "Adresse", requiredLabel: "Facultatif", width: 28,
		example: "12 rue de la Paix",
		help:    "Facultatif. Adresse de livraison.",
	},
	{
		header: "Étage", requiredLabel: "Facultatif", width: 10,
		help: "Facultatif.",
	},
	{
		header: "Porte", requiredLabel: "Facultatif", width: 10,
		help: "Facultatif.",
	},
	{
		header: "Complément d'adresse", requiredLabel: "Facultatif", width: 22,
		help: "Facultatif.",
	},
	{
		header: "Raison sociale", requiredLabel: "Facultatif", width: 22,
		example: "Ma Société SARL",
		help:    "Facultatif. Pour un client professionnel.",
	},
	{
		header: "Date de naissance", requiredLabel: "Facultatif", width: 16,
		example: "05/11/1990",
		help:    "Facultatif. Format JJ/MM/AAAA.",
	},
	{
		header: "Infos complémentaires", requiredLabel: "Facultatif", width: 28,
		help: "Facultatif. Note libre visible sur la fiche client.",
	},
	{
		header: "Notes de livraison", requiredLabel: "Facultatif", width: 24,
		help: "Facultatif. Consignes de livraison.",
	},
	{
		header: "Consentement marketing", requiredLabel: "Facultatif", width: 20,
		example: "Oui",
		help:    "Facultatif. \"Oui\" ou \"Non\" — laissé vide, vaut \"Non\" (aucun consentement présumé).",
	},
}

// Vérification à la compilation : le provider wello-generic est bien un
// provider de modèle.
var _ TemplateProvider = (*WelloGenericCustomerProvider)(nil)

// Template génère le classeur vierge du modèle clients. Deux feuilles :
// "Clients" (l'en-tête que le restaurateur remplit, figé en 1re ligne, sans
// aucune ligne de données) et "Mode d'emploi" (documentation colonne par
// colonne + règles générales).
func (p *WelloGenericCustomerProvider) Template() (string, []byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName(f.GetSheetName(0), templateCustomersSheet); err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: nommage de la feuille: %w", err)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EEF2F7"}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: style d'en-tête: %w", err)
	}

	for i, column := range welloGenericTemplateColumns {
		header, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return "", nil, fmt.Errorf("modèle d'import clients: cellule d'en-tête %d: %w", i+1, err)
		}
		if err := f.SetCellStr(templateCustomersSheet, header, column.header); err != nil {
			return "", nil, fmt.Errorf("modèle d'import clients: écriture de l'en-tête %q: %w", column.header, err)
		}

		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return "", nil, fmt.Errorf("modèle d'import clients: nom de colonne %d: %w", i+1, err)
		}
		if err := f.SetColWidth(templateCustomersSheet, name, name, column.width); err != nil {
			return "", nil, fmt.Errorf("modèle d'import clients: largeur de la colonne %q: %w", column.header, err)
		}
	}

	lastHeader, err := excelize.CoordinatesToCellName(len(welloGenericTemplateColumns), 1)
	if err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: dernière cellule d'en-tête: %w", err)
	}
	if err := f.SetCellStyle(templateCustomersSheet, "A1", lastHeader, headerStyle); err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: application du style d'en-tête: %w", err)
	}

	// L'en-tête reste visible pendant la saisie.
	if err := f.SetPanes(templateCustomersSheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection:   []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}},
	}); err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: figeage de l'en-tête: %w", err)
	}

	if err := writeCustomerTemplateHelpSheet(f); err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return "", nil, fmt.Errorf("modèle d'import clients: écriture du classeur: %w", err)
	}

	return templateFilename, buf.Bytes(), nil
}

// writeCustomerTemplateHelpSheet ajoute la feuille d'explications.
//
// Les consignes vivent ici et non dans les en-têtes : y ajouter un « * » ou
// une précision changerait le libellé exact, ce qui casserait la
// reconnaissance des colonnes (résolution par en-tête replié, pas par
// position).
func writeCustomerTemplateHelpSheet(f *excelize.File) error {
	if _, err := f.NewSheet(templateHelpSheet); err != nil {
		return fmt.Errorf("modèle d'import clients: création de la feuille d'aide: %w", err)
	}

	rows := [][]string{
		{"Colonne", "Obligatoire", "Exemple", "Explication"},
	}
	for _, column := range welloGenericTemplateColumns {
		rows = append(rows, []string{column.header, column.requiredLabel, column.example, column.help})
	}
	rows = append(rows,
		[]string{},
		[]string{"Règles générales"},
		[]string{"(*) Au moins l'une des deux colonnes Email OU Téléphone doit être renseignée sur chaque ligne, sinon la ligne est rejetée."},
		[]string{"Une ligne = un client. Ne modifiez pas la ligne d'en-tête (1re ligne) : les colonnes sont reconnues par leur libellé."},
		[]string{"Les clients sont rapprochés des clients existants par email ou téléphone : un client déjà présent vous sera signalé à l'étape de vérification (vous choisirez d'ignorer, de mettre à jour, ou d'importer quand même)."},
		[]string{"Consentement marketing : laissez vide ou \"Non\" si vous n'avez pas recueilli le consentement (RGPD) ; \"Oui\" uniquement si le client a explicitement consenti."},
	)

	for i, row := range rows {
		for j, value := range row {
			cell, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				return fmt.Errorf("modèle d'import clients: cellule d'aide (%d, %d): %w", j+1, i+1, err)
			}
			if err := f.SetCellStr(templateHelpSheet, cell, value); err != nil {
				return fmt.Errorf("modèle d'import clients: écriture de l'aide: %w", err)
			}
		}
	}

	for _, width := range []struct {
		column string
		size   float64
	}{{"A", 24}, {"B", 14}, {"C", 24}, {"D", 90}} {
		if err := f.SetColWidth(templateHelpSheet, width.column, width.column, width.size); err != nil {
			return fmt.Errorf("modèle d'import clients: largeur de la colonne d'aide %q: %w", width.column, err)
		}
	}

	return nil
}

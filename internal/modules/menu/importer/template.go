package importer

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// Noms des feuilles du modèle. La feuille des produits est en première
// position, et c'est contractuel : readSheetRows lit GetSheetList()[0].
const (
	templateProductsSheet = "Produits"
	templateHelpSheet     = "Mode d'emploi"
)

// TemplateColumn décrit une colonne du modèle vierge, telle qu'un appelant
// extérieur peut vouloir la présenter.
type TemplateColumn struct {
	Header   string `json:"header"`
	Required bool   `json:"required"`
	Example  string `json:"example"`
	Help     string `json:"help"`
}

// TemplateProvider est implémenté par les providers capables de fournir un
// modèle vierge à remplir.
//
// Interface distincte de ImportProvider, et volontairement facultative : Zelty
// est un export produit par un logiciel tiers, il n'y a pas de modèle Wello à
// proposer pour lui. L'absence d'implémentation suffit à l'exclure, sans liste
// de providers en dur à maintenir quelque part.
type TemplateProvider interface {
	ImportProvider

	TemplateColumns() []TemplateColumn
	TemplateFilename() string
	BuildTemplate(w io.Writer) error
}

// welloGenericTemplateColumn est la description interne d'une colonne.
//
// field relie l'en-tête écrit dans le fichier au champ que le parser attend :
// c'est ce lien que TestWelloGenericTemplateHeadersResolve vérifie, pour que le
// modèle et la table d'alias ne puissent pas diverger en silence.
type welloGenericTemplateColumn struct {
	field    welloGenericField
	header   string
	required bool
	example  string
	width    float64
	help     string
}

// welloGenericTemplate décrit le modèle colonne par colonne, dans l'ordre du
// fichier.
//
// Les en-têtes sont accentués parce qu'un restaurateur les lit ;
// welloGenericLabels, lui, reste en ASCII car il sert aux messages d'erreur.
// Les deux se rejoignent par foldHeader, qui replie les accents.
//
// Les prix sont en euros avec virgule décimale, pas en centimes : c'est ce que
// quelqu'un tape dans un tableur, et c'est ce que parsePriceCents attend. La
// saisie de masse en JSON, elle, est en centimes — deux portes, deux unités,
// chacune adaptée à qui la remplit.
var welloGenericTemplate = []welloGenericTemplateColumn{
	{
		field: wgFieldName, header: "Nom", required: true, width: 30,
		example: "Pizza Margherita",
		help:    "Obligatoire. Doit être unique dans le fichier.",
	},
	{
		field: wgFieldDescription, header: "Description", width: 38,
		example: "Tomate, mozzarella, basilic",
		help:    "Facultatif. Texte libre affiché sur la fiche produit.",
	},
	{
		field: wgFieldCategory, header: "Catégorie", required: true, width: 20,
		example: "NOS PIZZAS",
		help:    "Obligatoire. Une catégorie du même nom sera réutilisée si elle existe déjà, sinon créée.",
	},
	{
		field: wgFieldPriceIn, header: "Prix sur place", required: true, width: 15,
		example: "9,50",
		help:    "Obligatoire. En euros, avec une virgule décimale — pas en centimes.",
	},
	{
		field: wgFieldPriceTakeAway, header: "Prix emporté", width: 15,
		example: "9,50",
		help:    "Facultatif. En euros. Laissé vide, le prix sera à compléter avant validation.",
	},
	{
		field: wgFieldPriceDelivery, header: "Prix livraison", width: 15,
		example: "10,50",
		help:    "Facultatif. En euros.",
	},
	{
		field: wgFieldTvaIn, header: "TVA sur place", width: 15,
		example: "10",
		help:    "Taux en pourcentage (5,5 · 10 · 20). 0 signifie que le produit n'est pas vendu sur ce canal.",
	},
	{
		field: wgFieldTvaTakeAway, header: "TVA emporté", width: 15,
		example: "10",
		help:    "Taux en pourcentage. 0 signifie que le produit n'est pas vendu à emporter.",
	},
	{
		field: wgFieldTvaDelivery, header: "TVA livraison", width: 15,
		example: "10",
		help:    "Taux en pourcentage. 0 signifie que le produit n'est pas livré.",
	},
	{
		field: wgFieldTags, header: "Tags", width: 30,
		example: "Végétarien, Signature",
		help:    "Facultatif. Plusieurs tags séparés par des virgules.",
	},
}

// welloGenericTemplateFilename est le nom proposé au téléchargement.
const welloGenericTemplateFilename = "wello-modele-import-produits.xlsx"

// Vérification à la compilation : le provider du template Wello est bien un
// provider d'import à part entière.
var _ TemplateProvider = (*WelloGenericProvider)(nil)

func (p *WelloGenericProvider) TemplateFilename() string { return welloGenericTemplateFilename }

// TemplateColumns décrit le modèle sans le générer, pour un appelant qui
// voudrait l'afficher (aide en ligne, formulaire de saisie).
func (p *WelloGenericProvider) TemplateColumns() []TemplateColumn {
	columns := make([]TemplateColumn, 0, len(welloGenericTemplate))
	for _, column := range welloGenericTemplate {
		columns = append(columns, TemplateColumn{
			Header:   column.header,
			Required: column.required,
			Example:  column.example,
			Help:     column.help,
		})
	}
	return columns
}

// BuildTemplate écrit le classeur vierge.
//
// Toutes les valeurs sont posées en chaîne : « 9,50 » doit rester « 9,50 » à
// l'écran, et non être reformaté par le tableur selon sa locale. Le parser
// accepte de toute façon les deux séparateurs décimaux, un restaurateur qui
// retape la ligne en cellule numérique reste donc compris.
func (p *WelloGenericProvider) BuildTemplate(w io.Writer) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName(f.GetSheetName(0), templateProductsSheet); err != nil {
		return fmt.Errorf("modèle d'import: nommage de la feuille: %w", err)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EEF2F7"}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return fmt.Errorf("modèle d'import: style d'en-tête: %w", err)
	}

	for i, column := range welloGenericTemplate {
		header, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return fmt.Errorf("modèle d'import: cellule d'en-tête %d: %w", i+1, err)
		}
		if err := f.SetCellStr(templateProductsSheet, header, column.header); err != nil {
			return fmt.Errorf("modèle d'import: écriture de l'en-tête %q: %w", column.header, err)
		}

		example, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			return fmt.Errorf("modèle d'import: cellule d'exemple %d: %w", i+1, err)
		}
		if err := f.SetCellStr(templateProductsSheet, example, column.example); err != nil {
			return fmt.Errorf("modèle d'import: écriture de l'exemple %q: %w", column.example, err)
		}

		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return fmt.Errorf("modèle d'import: nom de colonne %d: %w", i+1, err)
		}
		if err := f.SetColWidth(templateProductsSheet, name, name, column.width); err != nil {
			return fmt.Errorf("modèle d'import: largeur de la colonne %q: %w", column.header, err)
		}
	}

	lastHeader, err := excelize.CoordinatesToCellName(len(welloGenericTemplate), 1)
	if err != nil {
		return fmt.Errorf("modèle d'import: dernière cellule d'en-tête: %w", err)
	}
	if err := f.SetCellStyle(templateProductsSheet, "A1", lastHeader, headerStyle); err != nil {
		return fmt.Errorf("modèle d'import: application du style d'en-tête: %w", err)
	}

	// L'en-tête reste visible pendant la saisie : sur un menu complet, on perd
	// vite de vue quelle colonne on remplit.
	if err := f.SetPanes(templateProductsSheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection:   []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}},
	}); err != nil {
		return fmt.Errorf("modèle d'import: figeage de l'en-tête: %w", err)
	}

	if err := writeTemplateHelpSheet(f); err != nil {
		return err
	}

	if err := f.Write(w); err != nil {
		return fmt.Errorf("modèle d'import: écriture du classeur: %w", err)
	}
	return nil
}

// writeTemplateHelpSheet ajoute la feuille d'explications.
//
// Les consignes vivent ici et non dans les en-têtes : y ajouter un « * » ou une
// unité (« Prix sur place (€) ») casserait la reconnaissance des colonnes, qui
// se fait sur le libellé exact replié.
func writeTemplateHelpSheet(f *excelize.File) error {
	if _, err := f.NewSheet(templateHelpSheet); err != nil {
		return fmt.Errorf("modèle d'import: création de la feuille d'aide: %w", err)
	}

	rows := [][]string{
		{"Colonne", "Obligatoire", "Exemple", "Explication"},
	}
	for _, column := range welloGenericTemplate {
		required := "non"
		if column.required {
			required = "oui"
		}
		rows = append(rows, []string{column.header, required, column.example, column.help})
	}
	rows = append(rows,
		[]string{},
		[]string{"À savoir"},
		[]string{"Ne renommez pas les colonnes de la feuille « " + templateProductsSheet + " » : elles servent à relire le fichier."},
		[]string{"La ligne d'exemple peut être remplacée par votre premier produit, ou supprimée."},
		[]string{"Les prix sont en euros (9,50), jamais en centimes."},
		[]string{"Un produit dont tous les prix valent 0 est importé, mais retiré de la carte."},
		[]string{"Rien n'est enregistré tant que vous n'avez pas validé l'aperçu affiché après l'envoi."},
	)

	for i, row := range rows {
		for j, value := range row {
			cell, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				return fmt.Errorf("modèle d'import: cellule d'aide (%d, %d): %w", j+1, i+1, err)
			}
			if err := f.SetCellStr(templateHelpSheet, cell, value); err != nil {
				return fmt.Errorf("modèle d'import: écriture de l'aide: %w", err)
			}
		}
	}

	for _, width := range []struct {
		column string
		size   float64
	}{{"A", 22}, {"B", 13}, {"C", 30}, {"D", 90}} {
		if err := f.SetColWidth(templateHelpSheet, width.column, width.column, width.size); err != nil {
			return fmt.Errorf("modèle d'import: largeur de la colonne d'aide %q: %w", width.column, err)
		}
	}

	return nil
}

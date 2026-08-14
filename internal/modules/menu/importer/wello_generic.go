package importer

import (
	"errors"
	"fmt"
	"io"

	"welloresto-api/internal/importutil"
)

// WelloGenericSlug identifie le template .xlsx defini par Wello : le
// restaurateur telecharge un classeur vide, le remplit, le renvoie.
const WelloGenericSlug = "wello-generic"

// Prefixes des identifiants externes generes. Le template n'a pas d'ID source :
// l'identite d'une ligne est son nom, et l'identifiant en derive de facon
// deterministe pour que deux imports successifs se reconnaissent.
const (
	welloGenericProductPrefix  = "wg-p"
	welloGenericCategoryPrefix = "wg-c"
	welloGenericTagPrefix      = "wg-t"
)

// ErrMissingColumn signale une colonne obligatoire absente de l'en-tete.
var ErrMissingColumn = errors.New("colonne obligatoire absente")

type welloGenericField int

const (
	wgFieldName welloGenericField = iota
	wgFieldDescription
	wgFieldCategory
	wgFieldPriceIn
	wgFieldPriceTakeAway
	wgFieldPriceDelivery
	wgFieldTvaIn
	wgFieldTvaTakeAway
	wgFieldTvaDelivery
	wgFieldTags
	wgFieldCount
)

// welloGenericLabels donne le libelle de reference de chaque colonne, celui
// qui figure dans le template genere et dans les messages d'erreur.
var welloGenericLabels = [wgFieldCount]string{
	wgFieldName:          "Nom",
	wgFieldDescription:   "Description",
	wgFieldCategory:      "Categorie",
	wgFieldPriceIn:       "Prix sur place",
	wgFieldPriceTakeAway: "Prix emporte",
	wgFieldPriceDelivery: "Prix livraison",
	wgFieldTvaIn:         "TVA sur place",
	wgFieldTvaTakeAway:   "TVA emporte",
	wgFieldTvaDelivery:   "TVA livraison",
	wgFieldTags:          "Tags",
}

// welloGenericRequired liste les colonnes sans lesquelles le fichier n'est pas
// exploitable. Les autres sont facultatives : une colonne absente laisse le
// champ a sa valeur neutre (0 pour un prix, nil pour un taux), a charge pour la
// preview de completer.
var welloGenericRequired = []welloGenericField{
	wgFieldName,
	wgFieldCategory,
	wgFieldPriceIn,
}

// welloGenericAliases reconnait les en-tetes. Les cles sont repliees par
// foldHeader : la casse, les espaces multiples et les accents ne comptent pas,
// un restaurateur qui retape la ligne d'en-tete reste compris.
var welloGenericAliases = map[string]welloGenericField{
	"nom":               wgFieldName,
	"nom du produit":    wgFieldName,
	"produit":           wgFieldName,
	"description":       wgFieldDescription,
	"categorie":         wgFieldCategory,
	"categorie caisse":  wgFieldCategory,
	"prix sur place":    wgFieldPriceIn,
	"prix":              wgFieldPriceIn,
	"prix emporte":      wgFieldPriceTakeAway,
	"prix a emporter":   wgFieldPriceTakeAway,
	"prix livraison":    wgFieldPriceDelivery,
	"prix en livraison": wgFieldPriceDelivery,
	"tva sur place":     wgFieldTvaIn,
	"tva":               wgFieldTvaIn,
	"tva emporte":       wgFieldTvaTakeAway,
	"tva a emporter":    wgFieldTvaTakeAway,
	"tva livraison":     wgFieldTvaDelivery,
	"tva en livraison":  wgFieldTvaDelivery,
	"tags":              wgFieldTags,
	"etiquettes":        wgFieldTags,
}

// WelloGenericProvider lit le template maison. Le format etant le notre, il est
// tabulaire et sans section : une ligne d'en-tete, puis une ligne par produit.
//
// Il ne porte pas de groupes d'options : l'import produit alors un canonique
// sans attributs.
type WelloGenericProvider struct{}

func NewWelloGenericProvider() *WelloGenericProvider { return &WelloGenericProvider{} }

func (p *WelloGenericProvider) Slug() string { return WelloGenericSlug }

func (p *WelloGenericProvider) Parse(r io.Reader) (*IntermediateImport, error) {
	rows, err := importutil.ReadSheetRows(r)
	if err != nil {
		return nil, err
	}

	headerIdx, columns, err := parseWelloGenericHeader(rows)
	if err != nil {
		return nil, err
	}

	out := &IntermediateImport{Provider: WelloGenericSlug}

	// Index nom normalise -> identifiant, pour dedoublonner categories et tags
	// cites par plusieurs produits.
	categoryIDByName := make(map[string]string)
	tagIDByName := make(map[string]string)
	productLineByID := make(map[string]int)

	for i := headerIdx + 1; i < len(rows); i++ {
		row := rows[i]
		line := i + 1
		if importutil.RowIsEmpty(row) {
			continue
		}

		name := importutil.CellAt(row, columns[wgFieldName])
		if name == "" {
			return nil, rowErrorf(line, welloGenericLabels[wgFieldName], "ligne renseignee sans nom de produit")
		}

		externalID := importutil.GeneratedExternalID(welloGenericProductPrefix, name)
		if previous, dup := productLineByID[externalID]; dup {
			return nil, rowErrorf(line, welloGenericLabels[wgFieldName],
				"nom deja utilise ligne %d ; les noms doivent etre uniques pour que l'import reste rejouable", previous)
		}
		productLineByID[externalID] = line

		product := CanonicalProduct{
			ExternalID:  externalID,
			Name:        name,
			Description: importutil.CellAt(row, columns[wgFieldDescription]),
		}

		prices := [...]struct {
			field welloGenericField
			dest  *int
		}{
			{wgFieldPriceIn, &product.PriceIn},
			{wgFieldPriceTakeAway, &product.PriceTakeAway},
			{wgFieldPriceDelivery, &product.PriceDelivery},
		}
		for _, p := range prices {
			value, err := parsePriceCents(importutil.CellAt(row, columns[p.field]))
			if err != nil {
				return nil, rowErrorf(line, welloGenericLabels[p.field], "%s", err)
			}
			*p.dest = value
		}
		product.AllPricesZero = product.PriceIn == 0 && product.PriceTakeAway == 0 && product.PriceDelivery == 0

		rates := [...]struct {
			field welloGenericField
			dest  **float64
		}{
			{wgFieldTvaIn, &product.TvaRateIn},
			{wgFieldTvaTakeAway, &product.TvaRateTakeAway},
			{wgFieldTvaDelivery, &product.TvaRateDelivery},
		}
		for _, rate := range rates {
			value, err := parseTvaRate(importutil.CellAt(row, columns[rate.field]))
			if err != nil {
				return nil, rowErrorf(line, welloGenericLabels[rate.field], "%s", err)
			}
			*rate.dest = value
		}

		// Cellule categorie vide : on laisse la categorie indeterminee plutot
		// que de rejeter le fichier. Elle est obligatoire a la creation, mais
		// c'est la preview qui la reclame, avec le produit sous les yeux.
		if categoryName := importutil.CellAt(row, columns[wgFieldCategory]); categoryName != "" {
			key := importutil.NormalizeLabel(categoryName)
			id, known := categoryIDByName[key]
			if !known {
				id = importutil.GeneratedExternalID(welloGenericCategoryPrefix, categoryName)
				categoryIDByName[key] = id
				out.Categories = append(out.Categories, CanonicalCategory{ExternalID: id, Name: categoryName})
			}
			product.CategoryExternalID = id
		}

		product.TagExternalIDs = resolveWelloGenericTags(out, tagIDByName, importutil.CellAt(row, columns[wgFieldTags]))
		out.Products = append(out.Products, product)
	}

	if len(out.Products) == 0 {
		return nil, ErrNoProducts
	}
	return out, nil
}

// parseWelloGenericHeader localise la ligne d'en-tete et associe chaque champ a
// son index de colonne. Un champ absent vaut -1, ce que cellAt traite comme une
// cellule vide.
func parseWelloGenericHeader(rows [][]string) (int, [wgFieldCount]int, error) {
	var columns [wgFieldCount]int
	for i := range columns {
		columns[i] = -1
	}

	headerIdx := -1
	for i, row := range rows {
		if !importutil.RowIsEmpty(row) {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return 0, columns, fmt.Errorf("%w: %s", ErrMissingColumn, "fichier vide")
	}

	for idx, cell := range rows[headerIdx] {
		field, known := welloGenericAliases[importutil.FoldHeader(cell)]
		if !known {
			continue
		}
		// Premiere occurrence retenue : une colonne dupliquee ne doit pas
		// masquer celle que le restaurateur a effectivement remplie en
		// premier.
		if columns[field] < 0 {
			columns[field] = idx
		}
	}

	for _, field := range welloGenericRequired {
		if columns[field] < 0 {
			return 0, columns, fmt.Errorf("%w: %q (ligne %d)", ErrMissingColumn, welloGenericLabels[field], headerIdx+1)
		}
	}

	return headerIdx, columns, nil
}

// resolveWelloGenericTags enregistre les tags cites par un produit et rend
// leurs identifiants, dans l'ordre de la cellule.
func resolveWelloGenericTags(out *IntermediateImport, tagIDByName map[string]string, raw string) []string {
	labels := importutil.SplitLabels(raw)
	if len(labels) == 0 {
		return nil
	}

	ids := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))

	for _, label := range labels {
		key := importutil.NormalizeLabel(label)

		id, known := tagIDByName[key]
		if !known {
			id = importutil.GeneratedExternalID(welloGenericTagPrefix, label)
			tagIDByName[key] = id
			out.Tags = append(out.Tags, CanonicalTag{ExternalID: id, Name: label})
		}

		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids
}

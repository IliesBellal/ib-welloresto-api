package importer

import "strings"

// ManualSlug identifie la saisie en masse : le back-office envoie directement
// des produits en JSON, sans fichier. C'est la troisieme porte d'entree du
// pipeline, et elle rejoint le meme canonique que les deux parsers.
const ManualSlug = "manual"

// Prefixes des identifiants externes generes. Comme le template, la saisie
// manuelle n'a pas d'identifiant source : l'identite d'une ligne est son nom.
const (
	manualProductPrefix  = "mn-p"
	manualCategoryPrefix = "mn-c"
	manualTagPrefix      = "mn-t"
)

// ManualProduct est une ligne du formulaire de saisie en masse.
//
// Les prix sont en centimes, comme CreateProductPayload : le back-office
// convertit deja les euros a la saisie. Les taux sont en pourcentage et
// restent bruts, c'est la preview qui les resout en tva_id.
type ManualProduct struct {
	Name        string
	Description string

	// Category est un nom de categorie. Vide = indetermine, tranche en preview.
	Category string

	PriceIn       int
	PriceTakeAway int
	PriceDelivery int

	TvaRateIn       *float64
	TvaRateTakeAway *float64
	TvaRateDelivery *float64

	Tags []string
}

// BuildManualImport assemble le canonique a partir d'une saisie en masse.
// Aucun parsing : les valeurs arrivent deja typees. Les regles de nommage et
// d'identite sont celles du template, pour que les trois portes se comportent
// pareil en aval.
func BuildManualImport(products []ManualProduct) (*IntermediateImport, error) {
	out := &IntermediateImport{Provider: ManualSlug}

	categoryIDByName := make(map[string]string)
	tagIDByName := make(map[string]string)
	productLineByID := make(map[string]int)

	for i, in := range products {
		line := i + 1

		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, rowErrorf(line, "name", "produit sans nom")
		}

		externalID := generatedExternalID(manualProductPrefix, name)
		if previous, dup := productLineByID[externalID]; dup {
			return nil, rowErrorf(line, "name",
				"nom deja utilise ligne %d ; les noms doivent etre uniques pour que l'import reste rejouable", previous)
		}
		productLineByID[externalID] = line

		rates := [...]struct {
			field string
			value *float64
		}{
			{"tva_in", in.TvaRateIn},
			{"tva_take_away", in.TvaRateTakeAway},
			{"tva_delivery", in.TvaRateDelivery},
		}
		for _, rate := range rates {
			if rate.value != nil && *rate.value < 0 {
				return nil, rowErrorf(line, rate.field, "taux negatif (%v)", *rate.value)
			}
		}

		product := CanonicalProduct{
			ExternalID:      externalID,
			Name:            name,
			Description:     strings.TrimSpace(in.Description),
			PriceIn:         in.PriceIn,
			PriceTakeAway:   in.PriceTakeAway,
			PriceDelivery:   in.PriceDelivery,
			TvaRateIn:       in.TvaRateIn,
			TvaRateTakeAway: in.TvaRateTakeAway,
			TvaRateDelivery: in.TvaRateDelivery,
			AllPricesZero:   in.PriceIn == 0 && in.PriceTakeAway == 0 && in.PriceDelivery == 0,
		}

		if categoryName := strings.TrimSpace(in.Category); categoryName != "" {
			key := normalizeLabel(categoryName)
			id, known := categoryIDByName[key]
			if !known {
				id = generatedExternalID(manualCategoryPrefix, categoryName)
				categoryIDByName[key] = id
				out.Categories = append(out.Categories, CanonicalCategory{ExternalID: id, Name: categoryName})
			}
			product.CategoryExternalID = id
		}

		product.TagExternalIDs = registerManualTags(out, tagIDByName, in.Tags)
		out.Products = append(out.Products, product)
	}

	if len(out.Products) == 0 {
		return nil, ErrNoProducts
	}
	return out, nil
}

func registerManualTags(out *IntermediateImport, tagIDByName map[string]string, labels []string) []string {
	ids := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))

	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		key := normalizeLabel(label)

		id, known := tagIDByName[key]
		if !known {
			id = generatedExternalID(manualTagPrefix, label)
			tagIDByName[key] = id
			out.Tags = append(out.Tags, CanonicalTag{ExternalID: id, Name: label})
		}

		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil
	}
	return ids
}

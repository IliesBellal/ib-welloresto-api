package menu

import "welloresto-api/internal/modules/menu/importer"

// maxImportFileSize borne le fichier accepté par la preview d'import. Aligné
// sur la limite des uploads d'images du module (5 Mo) : les exports réels
// pèsent une quinzaine de kilo-octets pour 141 produits, la marge est large.
const maxImportFileSize = 5 << 20

// Champs du formulaire multipart.
const (
	importFormProviderField = "provider"
	importFormFileField     = "file"
)

// ImportPreviewJSONRequest est le corps accepté en application/json : la porte
// du formulaire de saisie en masse, qui n'a pas de fichier à parser.
type ImportPreviewJSONRequest struct {
	// Provider est optionnel et vaut "manual" par défaut. Il n'est là que pour
	// tracer l'origine dans import_*_mapping.
	Provider string                     `json:"provider"`
	Products []ImportPreviewJSONProduct `json:"products"`
}

// ImportPreviewJSONProduct est une ligne de saisie.
//
// Les prix sont en centimes, comme CreateProductPayload — le back-office
// convertit déjà les euros à la saisie. Les TVA sont des taux en pourcentage,
// pas des tva_id : c'est la preview qui les résout, exactement comme pour un
// fichier.
type ImportPreviewJSONProduct struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`

	PriceIn       int `json:"price"`
	PriceTakeAway int `json:"price_take_away"`
	PriceDelivery int `json:"price_delivery"`

	TvaRateIn       *float64 `json:"tva_in"`
	TvaRateTakeAway *float64 `json:"tva_take_away"`
	TvaRateDelivery *float64 `json:"tva_delivery"`

	Tags []string `json:"tags"`
}

// toManualProducts traduit la requête vers l'entrée du constructeur canonique.
func (r *ImportPreviewJSONRequest) toManualProducts() []importer.ManualProduct {
	products := make([]importer.ManualProduct, 0, len(r.Products))
	for _, p := range r.Products {
		products = append(products, importer.ManualProduct{
			Name:            p.Name,
			Description:     p.Description,
			Category:        p.Category,
			PriceIn:         p.PriceIn,
			PriceTakeAway:   p.PriceTakeAway,
			PriceDelivery:   p.PriceDelivery,
			TvaRateIn:       p.TvaRateIn,
			TvaRateTakeAway: p.TvaRateTakeAway,
			TvaRateDelivery: p.TvaRateDelivery,
			Tags:            p.Tags,
		})
	}
	return products
}

package deliveroo

// SiteBrandResponse représente la réponse de l'API Deliveroo pour le Brand ID
type SiteBrandResponse struct {
	ID      string   `json:"id"`
	BrandID []string `json:"brand_id"`
}

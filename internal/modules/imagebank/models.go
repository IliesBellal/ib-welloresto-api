package imagebank

// Image : image de la banque telle que renvoyée au back-office.
type Image struct {
	ID       int64    `json:"id"`
	Slug     string   `json:"slug"`
	Category string   `json:"category"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	Format   string   `json:"format"`
	ImageURL string   `json:"image_url"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
}

// ListFilter : filtres de GET /image-bank. Les champs vides ne filtrent pas.
type ListFilter struct {
	Format   string
	Category string
	Query    string
}

// ListResponse : réponse de GET /image-bank.
type ListResponse struct {
	Images []Image `json:"images"`
	Total  int     `json:"total"`
}

// CategoryCount : catégorie et nombre d'images actives (tous formats, et par
// format pour pré-filtrer le popup).
type CategoryCount struct {
	Category
	Count   int            `json:"count"`
	Formats map[string]int `json:"formats"`
}

// MyImage : image déjà utilisée par l'établissement (« Mes images »).
type MyImage struct {
	ImageURL string `json:"image_url"`
	// Usage : où l'image est utilisée (product, option, category,
	// marketing_category, scannorder_banner, kiosk_idle, kiosk_marketing,
	// cds_media).
	Usage string `json:"usage"`
	// Label : nom du produit, de l'option ou de la catégorie quand il existe.
	Label string `json:"label"`
}

// MyImagesResponse : réponse de GET /image-bank/mine.
type MyImagesResponse struct {
	Images []MyImage `json:"images"`
}

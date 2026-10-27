// Package franchises gère les franchises au back-office : création, apparence
// du hub ScanNOrder, établissements rattachés et gestionnaires. Le hub public
// (GET /scannorder/franchises/{slug}) vit dans le module scannorder. Règles et
// décisions : docs/franchises.md.
package franchises

import "time"

const (
	RoleOwner   = "owner"
	RoleManager = "manager"
)

// Actor : l'utilisateur qui agit. Les droits de franchise sont attachés à la
// personne (user_id), jamais à l'établissement sélectionné dans le
// back-office. Le staff WelloResto (users.is_platform_staff) a les droits d'un
// owner sur toutes les franchises, pour le support.
type Actor struct {
	UserID        string
	PlatformStaff bool
}

// Franchise : ligne de la table franchises.
type Franchise struct {
	FranchiseID string    `json:"franchise_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description *string   `json:"description"`
	LogoURL     *string   `json:"logo_url"`
	BannerURL   *string   `json:"banner_url"`
	BrandColor  *string   `json:"brand_color"`
	CreatedAt   time.Time `json:"created_at"`
	// Announcement : bandeau d'annonce du hub (migration 196, D34).
	Announcement Announcement `json:"announcement"`
}

// Announcement : bandeau d'annonce du hub, tel que réglé au back-office.
// Affiché sur le hub quand Enabled, avec un texte, jusqu'à EndsOn inclus.
type Announcement struct {
	Enabled bool    `json:"enabled"`
	Text    *string `json:"text"`
	// URL : lien facultatif (https) ouvert au clic.
	URL *string `json:"url"`
	// EndsOn : dernier jour d'affichage (YYYY-MM-DD, jour local), nil = sans fin.
	EndsOn *string `json:"ends_on"`
}

// FranchiseSummary : une franchise de la liste « mes franchises ».
type FranchiseSummary struct {
	FranchiseID   string  `json:"franchise_id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	LogoURL       *string `json:"logo_url"`
	Role          string  `json:"role"`
	MerchantCount int     `json:"merchant_count"`
}

type ListResponse struct {
	Franchises []FranchiseSummary `json:"franchises"`
	// CanCreate : l'utilisateur est administrateur d'au moins un
	// établissement. Avec Franchises, décide de l'affichage du menu Franchise.
	CanCreate bool `json:"can_create"`
}

// FranchiseMerchant : un établissement rattaché, tel que l'affiche l'onglet
// Établissements.
type FranchiseMerchant struct {
	MerchantID   string  `json:"merchant_id"`
	BusinessName string  `json:"business_name"`
	Address      string  `json:"address"`
	ZipCode      string  `json:"zip_code"`
	City         string  `json:"city"`
	LogoURL      *string `json:"logo_url"`
	HubVisible   bool    `json:"hub_visible"`
	// ScanNOrderActive : ScanNOrder activé, non coupé par l'abonnement, QR
	// principal existant. Faux = absent du hub même si HubVisible.
	ScanNOrderActive   bool                   `json:"scannorder_active"`
	OrderTypes         FranchiseMerchantModes `json:"order_types"`
	ReservationEnabled bool                   `json:"reservation_enabled"`
}

type FranchiseMerchantModes struct {
	TakeAway bool `json:"take_away"`
	Delivery bool `json:"delivery"`
	In       bool `json:"in"`
}

type DetailResponse struct {
	Franchise Franchise           `json:"franchise"`
	HubURL    string              `json:"hub_url"`
	Role      string              `json:"role"`
	Merchants []FranchiseMerchant `json:"merchants"`
}

// CandidateMerchant : un établissement que l'utilisateur administre, proposé
// par « Ajouter un établissement ».
type CandidateMerchant struct {
	MerchantID    string  `json:"merchant_id"`
	BusinessName  string  `json:"business_name"`
	Address       string  `json:"address"`
	City          string  `json:"city"`
	FranchiseID   *string `json:"franchise_id"`
	FranchiseName *string `json:"franchise_name"`
}

type Member struct {
	UserID    string    `json:"user_id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// --- Requêtes ---

type CreateRequest struct {
	Name string `json:"name"`
	// Slug facultatif : dérivé du nom quand absent.
	Slug string `json:"slug"`
}

// UpdateRequest : champs absents = inchangés. Description ou brand_color
// vides effacent la valeur.
type UpdateRequest struct {
	Name         *string             `json:"name"`
	Slug         *string             `json:"slug"`
	Description  *string             `json:"description"`
	BrandColor   *string             `json:"brand_color"`
	Announcement *AnnouncementUpdate `json:"announcement"`
}

// AnnouncementUpdate : champs absents inchangés ; chaîne vide = efface.
type AnnouncementUpdate struct {
	Enabled *bool   `json:"enabled"`
	Text    *string `json:"text"`
	URL     *string `json:"url"`
	EndsOn  *string `json:"ends_on"`
}

type AddMerchantsRequest struct {
	MerchantIDs []string `json:"merchant_ids"`
}

type UpdateMerchantRequest struct {
	HubVisible *bool `json:"hub_visible"`
}

type AddMemberRequest struct {
	Email string `json:"email"`
	// Role facultatif : manager par défaut.
	Role string `json:"role"`
}

type UpdateMemberRequest struct {
	Role string `json:"role"`
}

type SlugAvailabilityResponse struct {
	Slug      string `json:"slug"`
	Valid     bool   `json:"valid"`
	Available bool   `json:"available"`
}

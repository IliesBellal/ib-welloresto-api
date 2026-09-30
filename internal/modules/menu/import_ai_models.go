package menu

import (
	"encoding/json"
	"time"

	"welloresto-api/internal/modules/menu/importer"
)

// Statuts d'un brouillon d'import photo (menu_import_drafts.status).
const (
	AIDraftPending    = "pending"    // créé, photos déposées, lecture pas commencée
	AIDraftProcessing = "processing" // lecture des photos en cours
	AIDraftReady      = "ready"      // au moins une photo lue : relecture possible
	AIDraftFailed     = "failed"     // aucune photo lue
	AIDraftCommitted  = "committed"  // import validé
	AIDraftExpired    = "expired"    // abandonné ou périmé
)

// Statuts d'une photo dans un brouillon.
const (
	AIPagePending = "pending"
	AIPageDone    = "done"
	AIPageFailed  = "failed"
)

// AIDraft est une ligne de menu_import_drafts.
type AIDraft struct {
	ID             string
	MerchantID     string
	CreatedBy      string
	Source         string
	Status         string
	ConsumesCredit bool
	Pages          []AIDraftPage
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
	CommittedAt    *time.Time
	FilesPurgedAt  *time.Time
}

// AIDraftPage est une photo du brouillon et sa lecture par l'IA, stockée en
// jsonb (menu_import_drafts.pages).
type AIDraftPage struct {
	Photo int    `json:"photo"`
	R2Key string `json:"r2_key"`
	// Status : pending | done | failed.
	Status string `json:"status"`
	// Ingredients : lire aussi les ingrédients des descriptions (choix fait à
	// l'envoi). Porté par chaque photo plutôt que par une colonne du
	// brouillon : une relance relit la photo dans le même mode, sans
	// migration de menu_import_drafts.
	Ingredients bool `json:"ingredients,omitempty"`
	// Output est l'AIMenuPage validée, gardée brute pour pouvoir rejouer la
	// fusion (BuildAIMenuImport) sans rappeler l'IA.
	Output  json.RawMessage   `json:"output,omitempty"`
	Error   string            `json:"error,omitempty"`
	Refused bool              `json:"refused,omitempty"`
	Usage   *AIDraftPageUsage `json:"usage,omitempty"`
}

// AIDraftPageUsage trace le coût et la durée d'un appel IA.
type AIDraftPageUsage struct {
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	LatencyMs    int64  `json:"latency_ms"`
	StopReason   string `json:"stop_reason"`
}

// ---- Réponses HTTP ----

// AIDraftResponse est la réponse de GET /menu/import/ai/{id}. Preview n'est
// présent que lorsque le brouillon est prêt : c'est une preview d'import
// ordinaire (même contrat que POST /menu/import/preview), avec son jeton
// pour POST /menu/import/commit.
type AIDraftResponse struct {
	ID          string                  `json:"id"`
	Status      string                  `json:"status"`
	Error       string                  `json:"error,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	ExpiresAt   time.Time               `json:"expires_at"`
	PhotosTotal int                     `json:"photos_total"`
	PhotosDone  int                     `json:"photos_done"`
	Photos      []AIDraftPhotoResponse  `json:"photos"`
	Preview     *importer.PreviewResult `json:"preview,omitempty"`
}

// AIDraftPhotoResponse décrit une photo ; URL est un lien R2 signé, valable
// une heure, pour l'afficher à côté des lignes en relecture.
type AIDraftPhotoResponse struct {
	Photo  int    `json:"photo"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	URL    string `json:"url,omitempty"`
}

// AIDraftSummary est une ligne de GET /menu/import/drafts.
type AIDraftSummary struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	PhotosTotal int       `json:"photos_total"`
	PhotosDone  int       `json:"photos_done"`
}

// AICredits est le solde de crédits d'import photo d'un marchand.
type AICredits struct {
	Total     int `json:"total"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
}

// AIDraftsResponse est la réponse de GET /menu/import/drafts.
type AIDraftsResponse struct {
	Drafts  []AIDraftSummary `json:"drafts"`
	Credits AICredits        `json:"credits"`
}

// SetAICreditsRequest est le corps de PUT /admin/merchants/{id}/menu-ocr-credits.
type SetAICreditsRequest struct {
	Credits *int `json:"credits"`
}

func (d *AIDraft) photosDone() int {
	n := 0
	for _, p := range d.Pages {
		if p.Status == AIPageDone {
			n++
		}
	}
	return n
}

// withIngredients dit si les ingrédients ont été demandés pour ce brouillon.
func (d *AIDraft) withIngredients() bool {
	for _, p := range d.Pages {
		if p.Ingredients {
			return true
		}
	}
	return false
}

func (d *AIDraft) summary() AIDraftSummary {
	return AIDraftSummary{
		ID:          d.ID,
		Status:      d.Status,
		CreatedAt:   d.CreatedAt,
		ExpiresAt:   d.ExpiresAt,
		PhotosTotal: len(d.Pages),
		PhotosDone:  d.photosDone(),
	}
}

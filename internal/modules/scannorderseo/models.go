// Package scannorderseo porte les réglages de référencement de la page
// ScanNOrder d'un établissement, édités depuis le back-office : titre,
// description, types de cuisine (liste fermée), liens publics, et la
// suggestion par IA de la description et des cuisines à partir de la carte.
// La page publique les lit par GET /scannorder/{slug} (module scannorder).
// Voir docs/scannorder-seo.md.
package scannorderseo

import (
	"errors"
	"time"

	"welloresto-api/internal/helpers"
)

// Bornes des champs, en caractères (runes). Le titre au-delà de ~60
// caractères est coupé par Google ; la description publiée en meta est
// coupée à 160 par le front, le texte entier est affiché sur la page.
const (
	MaxTitleLen       = 70
	MaxDescriptionLen = 500
)

// Settings : réglages de référencement tels qu'affichés dans le back-office.
type Settings struct {
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	CuisineTypes []string `json:"cuisine_types"` // codes de helpers.ScanNOrderCuisines, type principal en premier
	Links        []string `json:"links"`

	// CuisineOptions : la liste fermée, dans l'ordre d'affichage.
	CuisineOptions []helpers.ScanNOrderCuisine `json:"cuisine_options"`
	Limits         Limits                      `json:"limits"`
	// SuggestAvailable : la suggestion par IA est ouverte
	// (AI_TASK_SEO_PROFILE_ENABLED) — le back-office masque le bouton sinon.
	SuggestAvailable bool `json:"suggest_available"`
	// UpdatedAt : dernière modification (seo_updated_at), nil si jamais.
	UpdatedAt *time.Time `json:"updated_at"`
}

type Limits struct {
	TitleMaxLength       int `json:"title_max_length"`
	DescriptionMaxLength int `json:"description_max_length"`
	CuisinesMax          int `json:"cuisines_max"`
	LinksMax             int `json:"links_max"`
}

// UpdateRequest : PATCH partiel, un champ absent n'est pas modifié ; une
// chaîne vide ou une liste vide efface.
type UpdateRequest struct {
	Title        *string   `json:"title"`
	Description  *string   `json:"description"`
	CuisineTypes *[]string `json:"cuisine_types"`
	Links        *[]string `json:"links"`
}

// Suggestion : proposition de l'IA, jamais enregistrée par l'API — le
// restaurateur la relit, la modifie et l'enregistre par PATCH.
type Suggestion struct {
	Description  string   `json:"description"`
	CuisineTypes []string `json:"cuisine_types"`
}

// settingsRow : lecture brute de scannorder_settings.
type settingsRow struct {
	Title       string
	Description string
	Cuisines    string
	Links       string
	UpdatedAt   *time.Time
}

// update : valeurs validées à écrire (nil = colonne inchangée).
type update struct {
	Title       *string
	Description *string
	Cuisines    *string // codes joints par des virgules
	Links       *string // tableau JSON, "" pour NULL
}

// FieldError : champ refusé par la validation, rendu en 400 avec le champ et
// la raison pour que le back-office désigne la saisie fautive.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Value  string `json:"value,omitempty"`
}

func (e *FieldError) Error() string { return "invalid " + e.Field + ": " + e.Reason }

var (
	// ErrSuggestDisabled : tâche seo_profile fermée ou registre IA absent.
	ErrSuggestDisabled = errors.New("seo_suggest_disabled")
	// ErrMenuTooSmall : trop peu de produits visibles pour décrire
	// l'établissement.
	ErrMenuTooSmall = errors.New("menu_too_small")
	// ErrSuggestFailed : l'IA n'a pas rendu de proposition exploitable.
	ErrSuggestFailed = errors.New("seo_suggest_failed")
)

package kiosk

// Ruban marketing des bornes : rotation d'images et de vidéos affichée en haut
// de l'écran de menu (migration 190).
//
// Périmètre MERCHANT, comme kiosk_settings. Le module CDS porte une rotation
// équivalente mais par écran (cds_media_items) ; les helpers de durée
// ci-dessous en sont volontairement une copie : le module kiosk ne doit pas
// importer le module cds (sens unique, voir cds/models.go).

import (
	"time"

	"welloresto-api/internal/models"
)

const (
	// Bornes des durées d'affichage, alignées sur les contraintes CHECK de
	// kiosk_marketing_media et kiosk_settings (migration 190). Validées ici
	// pour renvoyer une erreur lisible plutôt qu'un 500 sur violation de
	// contrainte.
	minMarketingDurationSeconds = 3
	maxMarketingDurationSeconds = 120

	// defaultMarketingDurationSeconds est le DEFAULT de
	// kiosk_settings.marketing_default_duration_seconds.
	defaultMarketingDurationSeconds = 10

	// maxMarketingMedia plafonne la rotation d'un merchant. Une borne télécharge
	// et garde sur disque chaque média : sans plafond, un merchant pourrait
	// remplir le stockage d'une tablette d'entrée de gamme.
	maxMarketingMedia = 20

	marketingKindImage = "image"
	marketingKindVideo = "video"
)

// MarketingMediaRow mappe la table kiosk_marketing_media.
type MarketingMediaRow struct {
	ID         string
	MerchantID string
	Kind       string
	URL        string
	// DurationSeconds est la durée PROPRE à ce média. Nil = il suit la durée par
	// défaut du merchant (kiosk_settings.marketing_default_duration_seconds).
	// Toujours nil pour une vidéo, jouée en entier.
	DurationSeconds *int
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       *time.Time
}

// MarketingMediaResponse — un média de la rotation.
//
// DurationSeconds est TOUJOURS la durée effective, déjà résolue côté serveur :
// COALESCE(durée propre, durée par défaut). La borne reçoit un entier et n'a
// rien à savoir de cette distinction (ignoré pour une vidéo). CustomDuration dit
// si la durée est propre au média (true) ou héritée (false) : c'est ce qui
// permet au back-office d'afficher « par défaut » ou « personnalisée ».
type MarketingMediaResponse struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	URL             string `json:"url"`
	DurationSeconds int    `json:"duration_seconds"`
	CustomDuration  bool   `json:"custom_duration"`
	SortOrder       int    `json:"sort_order"`
}

// MarketingRibbonResponse — bloc `marketing_ribbon` de KioskSettingsResponse.
type MarketingRibbonResponse struct {
	Enabled                bool                     `json:"enabled"`
	DefaultDurationSeconds int                      `json:"default_duration_seconds"`
	Media                  []MarketingMediaResponse `json:"media"`
}

// UpdateMarketingMediaRequest — body de PUT .../marketing/media/{media_id}.
//
// DurationSeconds renseigné : durée propre à ce média. Absent, null ou 0 : le
// média redevient suiveur de la durée par défaut. Le corps ne sert qu'à cela,
// il n'y a donc pas lieu de distinguer « absent » de « null ».
type UpdateMarketingMediaRequest struct {
	DurationSeconds *int `json:"duration_seconds"`
}

// ReorderMarketingMediaRequest — body de PUT .../marketing/media/reorder.
type ReorderMarketingMediaRequest struct {
	OrderedIDs []string `json:"ordered_ids"`
}

// validMarketingDuration dit si une durée renseignée est dans les bornes.
func validMarketingDuration(seconds int) bool {
	return seconds >= minMarketingDurationSeconds && seconds <= maxMarketingDurationSeconds
}

// normalizeMarketingDuration décide de la durée PROPRE à stocker pour un média :
// nil pour « suit la durée par défaut », une valeur pour une durée personnalisée.
//
//   - absente ou 0 -> nil : 0 est traité comme « non renseigné » ;
//   - vidéo        -> nil, quoi qu'on envoie : elle est jouée en entier, une
//     durée n'aurait aucun effet et ferait croire le contraire ;
//   - hors bornes  -> erreur.
func normalizeMarketingDuration(kind string, seconds *int) (*int, error) {
	if kind == marketingKindVideo || seconds == nil || *seconds == 0 {
		return nil, nil
	}
	if !validMarketingDuration(*seconds) {
		return nil, models.ErrKioskMediaInvalid
	}
	value := *seconds
	return &value, nil
}

// toMarketingMediaResponse projette une ligne en réponse, durée résolue.
func toMarketingMediaResponse(item MarketingMediaRow, defaultDuration int) MarketingMediaResponse {
	seconds, custom := defaultDuration, false
	if item.DurationSeconds != nil {
		seconds, custom = *item.DurationSeconds, true
	}
	return MarketingMediaResponse{
		ID:              item.ID,
		Kind:            item.Kind,
		URL:             item.URL,
		DurationSeconds: seconds,
		CustomDuration:  custom,
		SortOrder:       item.SortOrder,
	}
}

// toMarketingMediaResponses projette toute une rotation. Jamais nil : la borne
// et le back-office reçoivent `[]` et non `null`.
func toMarketingMediaResponses(items []MarketingMediaRow, defaultDuration int) []MarketingMediaResponse {
	out := make([]MarketingMediaResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toMarketingMediaResponse(item, defaultDuration))
	}
	return out
}

// mergeMarketingOrder calcule l'ordre complet de la rotation à partir de
// l'ordre demandé.
//
// Les identifiants demandés passent en premier, dans l'ordre reçu ; les médias
// existants non cités suivent dans leur ordre actuel. Un back-office dont la
// liste est périmée (un média ajouté depuis par un autre onglet) ne fait donc ni
// échouer la requête ni disparaître le média récent, et deux médias ne se
// retrouvent jamais sur le même rang.
//
// Un identifiant inconnu ou cité deux fois est une erreur : c'est un bug
// d'appelant, pas une liste périmée.
func mergeMarketingOrder(existing, requested []string) ([]string, error) {
	known := make(map[string]bool, len(existing))
	for _, id := range existing {
		known[id] = true
	}

	placed := make(map[string]bool, len(requested))
	merged := make([]string, 0, len(existing))
	for _, id := range requested {
		if !known[id] || placed[id] {
			return nil, models.ErrKioskMediaInvalid
		}
		placed[id] = true
		merged = append(merged, id)
	}
	for _, id := range existing {
		if !placed[id] {
			merged = append(merged, id)
		}
	}
	return merged, nil
}

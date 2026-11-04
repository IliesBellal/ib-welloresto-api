package deliveroo

import "encoding/json"

// SiteBrandResponse représente la réponse de l'API Deliveroo pour le Brand ID
type SiteBrandResponse struct {
	ID      string   `json:"id"`
	BrandID []string `json:"brand_id"`
}

type UnavailabilitiesResponse struct {
	UnavailableIDs []string `json:"unavailable_ids"`
	HiddenIDs      []string `json:"hidden_ids"`
}

type UnavailabilitiesRequest struct {
	UnavailableIDs []string `json:"unavailable_ids"`
	HiddenIDs      []string `json:"hidden_ids"`
}

// UploadMenuResponse : réponse 200 du PUT menu V1. Result vaut
// MATCH_EXISTING_MENU quand le menu est identique à celui en ligne : dans ce
// cas Deliveroo n'envoie PAS de webhook de résultat.
type UploadMenuResponse struct {
	Status   string   `json:"status"`
	Result   string   `json:"result,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ResultMatchExistingMenu : menu identique à celui en ligne.
const ResultMatchExistingMenu = "MATCH_EXISTING_MENU"

// S3UploadResponse : réponse du PUT /menu/v3/brands/{brand}/menus/{id}.
type S3UploadResponse struct {
	ID        string `json:"id"`
	UploadURL string `json:"upload_url"`
	Version   string `json:"version"`
}

// JobRequest : POST /menu/v3/brands/{brand}/jobs. Les sites ne figurent pas
// ici mais dans le document déposé sur S3 (site_ids).
type JobRequest struct {
	Action string    `json:"action"` // "publish_menu_to_live"
	Params JobParams `json:"params"`
}

type JobParams struct {
	MenuID  string `json:"menu_id"`
	Version string `json:"version,omitempty"` // vide = dernière version déposée
}

// JobResponse : statut new | in_progress | success | failed.
type JobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// JobStatusResponse : GET /menu/v3/brands/{brand}/jobs/{job_id}.
type JobStatusResponse struct {
	ID      string          `json:"id"`
	Status  string          `json:"status"` // new, in_progress, success, failed
	Type    string          `json:"type"`
	Details *JobDetails     `json:"details,omitempty"`
	Outcome json.RawMessage `json:"outcome,omitempty"` // détail de l'erreur si failed
}

type JobDetails struct {
	Validation *JobValidation `json:"validation,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
}

type JobValidation struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// MenuUploadResult : body.menu_upload_result du webhook menu.upload_result.
// HTTPStatus : 200 menu traité (hors problèmes d'images), 400 menu invalide,
// 500 autre erreur.
type MenuUploadResult struct {
	HTTPStatus int               `json:"http_status"`
	BrandID    string            `json:"brand_id"`
	MenuID     string            `json:"menu_id"`
	SiteIDs    []string          `json:"site_ids"`
	Errors     *MenuUploadErrors `json:"errors,omitempty"`
}

type MenuUploadErrors struct {
	Processing string `json:"processing,omitempty"`
	Images     []struct {
		URL     string `json:"url"`
		Message string `json:"message"`
	} `json:"images,omitempty"`
	Barcodes []struct {
		Barcode string `json:"barcode"`
		Message string `json:"message"`
	} `json:"barcodes,omitempty"`
}

// Empty : aucune erreur signalée.
func (e *MenuUploadErrors) Empty() bool {
	return e == nil || (e.Processing == "" && len(e.Images) == 0 && len(e.Barcodes) == 0)
}

type FetchMenuV3Response struct {
	URL string `json:"url"` // L'URL où se trouve le JSON du menu
}

package deliveroo

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

type S3UploadResponse struct {
	UploadURL string `json:"upload_url"`
}

type JobRequest struct {
	Action string    `json:"action"` // "publish_menu_to_live"
	Params JobParams `json:"params"`
}

type JobParams struct {
	MenuID  string   `json:"menu_id"`
	Version string   `json:"version,omitempty"`
	SiteIDs []string `json:"site_ids"` // <--- Ajout crucial ici
}

type JobResponse struct {
	JobID string `json:"job_id"`
}

type JobStatusResponse struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"` // PENDING, PROCESSING, COMPLETED, FAILED
	Action     string     `json:"action"`
	CreatedAt  string     `json:"created_at"`
	FinishedAt string     `json:"finished_at,omitempty"`
	Errors     []JobError `json:"errors,omitempty"`
}

type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"` // Pratique pour savoir quel item pose problème
}

type FetchMenuV3Response struct {
	URL string `json:"url"` // L'URL où se trouve le JSON du menu
}

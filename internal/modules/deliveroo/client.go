package deliveroo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
	"welloresto-api/internal/config"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// DeliverooClient gère la communication avec l'API
type DeliverooClient struct {
	httpClient *http.Client
	config     config.DeliverooConfig

	// Gestion du token en cache
	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// TokenResponse structure pour parser la réponse OAuth
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // Secondes
}

// ErrorResponse structure pour capturer les erreurs API
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewDeliverooClient(httpClient *http.Client, config config.DeliverooConfig) *DeliverooClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &DeliverooClient{
		httpClient: httpClient,
		config:     config,
	}
}

// ==========================================
// AUTHENTIFICATION (Interne)
// ==========================================

// getToken récupère le token actuel ou en demande un nouveau s'il est expiré
// Équivalent de ta méthode PHP public function getToken()
func (c *DeliverooClient) getToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	// On prend une marge de sécurité de 60 secondes
	if c.accessToken != "" && time.Now().Add(60*time.Second).Before(c.tokenExpiry) {
		defer c.tokenMu.RUnlock()
		return c.accessToken, nil
	}
	c.tokenMu.RUnlock()

	// Si pas de token ou expiré, on verrouille en écriture pour le rafraîchir
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double check au cas où une autre goroutine l'aurait mis à jour entre temps
	if c.accessToken != "" && time.Now().Add(60*time.Second).Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	return c.refreshToken(context.Background())
}

func (c *DeliverooClient) refreshToken(ctx context.Context) (string, error) {

	//url := "https://auth-sandbox.developers.deliveroo.com/oauth2/token"
	url := fmt.Sprintf("%s/oauth2/token", c.config.AuthBaseURL)
	payload := "grant_type=client_credentials"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBufferString(payload))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Basic "+c.config.BasicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken
	// Le token expire dans X secondes, on calcule la date absolue
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return c.accessToken, nil
}

// ==========================================
// HELPERS HTTP
// ==========================================

func (c *DeliverooClient) doRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, error) {
	log := logger.FromContext(ctx)

	var bodyReader io.Reader
	if payload != nil {
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshaling payload: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	// Récupération automatique du token
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)

	if err != nil {
		log.Info("DeliverooClient.doRequest - calling " + url + " | error " + err.Error())
		return nil, err
	}

	log.Info("DeliverooClient.doRequest - calling " + method + " " + url + " | answered " + resp.Status)

	return resp, err
}

// ==========================================
// MÉTHODES MÉTIER (Correspondance PHP)
// ==========================================

// AcceptOrder correspond à $this->updateOrderStatus(..., ["status" => "accepted"])
func (c *DeliverooClient) AcceptOrder(ctx context.Context, brandOrderID string) error {
	//url := fmt.Sprintf("%shttps://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", url.PathEscape(brandOrderID))
	url := fmt.Sprintf("%s/order/v1/orders/%s", c.config.BaseURL, url.PathEscape(brandOrderID))
	payload := map[string]string{"status": "accepted"}

	resp, err := c.doRequest(ctx, "PATCH", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

func (c *DeliverooClient) UpdateUnavailabilities(ctx context.Context, brandID, menuID, siteID string, payload any) error {
	url := fmt.Sprintf("%s/menu/v1/brands/%s/menus/%s/item_unavailabilities/%s", c.config.BaseURL, brandID, menuID, siteID)

	resp, err := c.doRequest(ctx, "POST", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

func (c *DeliverooClient) GetUnavailabilities(ctx context.Context, brandID, menuID, siteID string) (*UnavailabilitiesResponse, error) {
	url := fmt.Sprintf("%s/menu/v1/brands/%s/menus/%s/item_unavailabilities/%s", c.config.BaseURL, brandID, menuID, siteID)

	// On utilise ton helper (payload est nil car c'est un GET)
	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // Très important de fermer ici !

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deliveroo api error (%d)", resp.StatusCode)
	}

	var result UnavailabilitiesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (c *DeliverooClient) ReplaceUnavailabilities(ctx context.Context, brandID, menuID, siteID string, payload UnavailabilitiesRequest) error {
	url := fmt.Sprintf("%s/menu/v1/brands/%s/menus/%s/item_unavailabilities/%s", c.config.BaseURL, brandID, menuID, siteID)

	// On utilise ton helper pour le PUT
	resp, err := c.doRequest(ctx, "PUT", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Deliveroo renvoie souvent 204 (No Content) ou 200 pour un PUT réussi
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("deliveroo api error (%d)", resp.StatusCode)
	}
	return nil
}

// UpdateSiteWorkloadMode sets the current workload mode for a site.
func (c *DeliverooClient) UpdateSiteWorkloadMode(ctx context.Context, brandID, siteID, mode string) error {
	url := fmt.Sprintf("%s/site/v1/brands/%s/sites/%s/workload/mode", c.config.BaseURL, url.PathEscape(brandID), url.PathEscape(siteID))
	payload := map[string]string{"mode": mode}

	resp, err := c.doRequest(ctx, "PUT", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}

	return nil
}

// CloseSiteTemporary closes the site until the provided timestamp.
func (c *DeliverooClient) CloseSiteTemporary(ctx context.Context, siteID string, offlineUntil time.Time) error {
	url := fmt.Sprintf("%s/order/v1/sites/%s", c.config.BaseURL, url.PathEscape(siteID))
	payload := map[string]interface{}{
		"is_open":       false,
		"offline_until": offlineUntil.UTC().Format(time.RFC3339),
	}

	resp, err := c.doRequest(ctx, "PATCH", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}

	return nil
}

// ConfirmOrder correspond à $this->updateOrderStatus(..., ["status" => "confirmed"])
func (c *DeliverooClient) ConfirmOrder(ctx context.Context, brandOrderID string) error {
	//url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", brandOrderID)
	url := fmt.Sprintf("%s/order/v1/orders/%s", c.config.BaseURL, url.PathEscape(brandOrderID))
	payload := map[string]string{"status": "confirmed"}

	resp, err := c.doRequest(ctx, "PATCH", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

// DenyOrder correspond à setDeliverooOrderDenied.
// NOTE: Deliveroo utilise "rejected" comme status, pas "denied".
func (c *DeliverooClient) DenyOrder(ctx context.Context, brandOrderID string, in models.DenyOrderRequest) error {
	//url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", url.PathEscape(brandOrderID))
	url := fmt.Sprintf("%s/order/v1/orders/%s", c.config.BaseURL, url.PathEscape(brandOrderID))

	payload := map[string]string{
		"status":        "rejected",
		"reject_reason": in.DeletionReasonType, // ex: "too_busy", "item_unavailable"
		"notes":         in.DeletionComment,
	}

	log := logger.FromContext(ctx)
	log.Info("DeliverooClient.DenyOrder - doRequest for order " + brandOrderID + "(reject_reason - " + in.DeletionReasonType + " | notes - " + in.DeletionComment + ")")

	resp, err := c.doRequest(ctx, "PATCH", url, payload)
	if err != nil {
		log.Info("DeliverooClient.DenyOrder - error doing request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

// SetReadyForCollection correspond à setDeliverooOrderReady -> createStageForOrder
func (c *DeliverooClient) SetReadyForCollection(ctx context.Context, brandOrderID string) error {
	return c.createStage(ctx, brandOrderID, "ready_for_collection")
}

// SetCollected correspond à setDeliverooOrderCollected -> createStageForOrder
func (c *DeliverooClient) SetCollected(ctx context.Context, brandOrderID string) error {
	return c.createStage(ctx, brandOrderID, "collected")
}

// createStage est la méthode générique interne
func (c *DeliverooClient) createStage(ctx context.Context, brandOrderID, stage string) error {
	//url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s/prep_stage", brandOrderID)
	url := fmt.Sprintf("%s/order/v1/orders/%s/prep_stage", c.config.BaseURL, url.PathEscape(brandOrderID))

	// Format de date ISO 8601 UTC requis par Deliveroo
	occurredAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	payload := map[string]string{
		"stage":       stage,
		"occurred_at": occurredAt,
	}

	resp, err := c.doRequest(ctx, "POST", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

// Dans deliveroo_client.go

// GetMenu récupère le menu d'un restaurant depuis l'API Deliveroo
func (c *DeliverooClient) GetMenu(ctx context.Context, brandID string) (map[string]interface{}, error) {
	urlPath := fmt.Sprintf("%s/menu/v1/brands/%s/menus", c.config.BaseURL, url.PathEscape(brandID))

	resp, err := c.doRequest(ctx, "GET", urlPath, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.handleError(resp)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode menu response: %w", err)
	}
	return result, nil
}

// SyncMenu pousse un menu vers l'API Deliveroo
func (c *DeliverooClient) SyncMenu(ctx context.Context, brandID string, menu interface{}) error {
	urlPath := fmt.Sprintf("%s/menu/v1/brands/%s/menus", c.config.BaseURL, url.PathEscape(brandID))

	resp, err := c.doRequest(ctx, "PUT", urlPath, menu)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

// SendSyncStatus envoie le statut de synchronisation à Deliveroo suite à un Webhook
func (c *DeliverooClient) SendSyncStatus(ctx context.Context, brandOrderID string, status string, reason string, notes string) error {
	// ATTENTION: Vérifie l'URL exacte dans la doc Deliveroo pour le sync_status
	// C'est souvent /order/v1/orders/{id}/sync_status
	urlPath := fmt.Sprintf("%s/order/v1/orders/%s/sync_status", c.config.BaseURL, url.PathEscape(brandOrderID))

	occurredAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	payload := map[string]string{
		"status":      status, // "succeeded" ou "failed"
		"occurred_at": occurredAt,
		"reason":      reason,
		"notes":       notes,
	}

	log := logger.FromContext(ctx)
	log.Info(fmt.Sprintf("DeliverooClient.SendSyncStatus - sending %s for order %s", status, brandOrderID))

	resp, err := c.doRequest(ctx, "POST", urlPath, payload)
	if err != nil {
		return fmt.Errorf("network error on sync_status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

// handleError parse le body pour donner une erreur Go propre
func (c *DeliverooClient) handleError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	// Essayer de décoder le JSON d'erreur standard de Deliveroo
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Code != "" {
		return fmt.Errorf("deliveroo api error (%d): %s - %s", resp.StatusCode, errResp.Code, errResp.Message)
	}

	return fmt.Errorf("deliveroo api error (%d): %s", resp.StatusCode, string(body))
}

// GetBrandIDBySiteID récupère le brand_id à partir d'un site_id externe
// Endpoint: GET /site/v1/restaurant_locations/{site_id}
func (c *DeliverooClient) GetBrandIDBySiteID(ctx context.Context, siteID string) (string, error) {
	url := fmt.Sprintf("%s/site/v1/restaurant_locations/%s", c.config.BaseURL, url.PathEscape(siteID))

	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.handleError(resp)
	}

	var result SiteBrandResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode site brand response: %w", err)
	}

	// On vérifie que le tableau n'est pas vide avant d'accéder à l'index 0
	if len(result.BrandID) == 0 {
		return "", fmt.Errorf("no brand_id found for site_id: %s", siteID)
	}

	// On retourne le premier brand_id trouvé
	return result.BrandID[0], nil
}

// UploadMenu envoie le payload complet du menu à Deliveroo
func (c *DeliverooClient) UploadMenu(ctx context.Context, brandID string, menuID string, payload interface{}) error {
	url := fmt.Sprintf("%s/menu/v1/brands/%s/menus/%s", c.config.BaseURL, brandID, menuID)

	resp, err := c.doRequest(ctx, "PUT", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleError(resp)
	}
	return nil
}

func (c *DeliverooClient) UpdateIndividualUnavailabilities(ctx context.Context, brandID, menuID, siteID string, payload any) error {
	url := fmt.Sprintf("%s/menu/v1/brands/%s/menus/%s/item_unavailabilities/%s", c.config.BaseURL, brandID, menuID, siteID)

	resp, err := c.doRequest(ctx, "POST", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("deliveroo api error (%d)", resp.StatusCode)
	}
	return nil
}

func (c *DeliverooClient) GenerateUploadURL(ctx context.Context, brandID, menuID string) (string, error) {
	// Note le "/v3/" dans l'URL
	url := fmt.Sprintf("%s/menu/v3/brands/%s/menus/%s", c.config.BaseURL, brandID, menuID)

	// On utilise ton doRequest. payload est nil car l'endpoint n'attend pas de body.
	resp, err := c.doRequest(ctx, "PUT", url, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deliveroo v3 error (%d)", resp.StatusCode)
	}

	var result S3UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.UploadURL, nil
}

func (c *DeliverooClient) UploadToS3(ctx context.Context, s3URL string, menuData any) error {
	// On transforme le menu en JSON
	payload, err := json.Marshal(menuData)
	if err != nil {
		return err
	}

	// On fait un PUT directement vers Amazon S3
	// Note : On n'utilise PAS nos headers d'authentification Deliveroo ici,
	// l'URL S3 est déjà "pré-signée".
	req, err := http.NewRequestWithContext(ctx, "PUT", s3URL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("S3 upload failed with status: %d", resp.StatusCode)
	}

	return nil
}

func (c *DeliverooClient) CreateMenuJob(ctx context.Context, brandID string, jobReq JobRequest) (string, error) {
	url := fmt.Sprintf("%s/menu/v3/brands/%s/jobs", c.config.BaseURL, brandID)

	resp, err := c.doRequest(ctx, "POST", url, jobReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res JobResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	return res.JobID, nil
}

func (c *DeliverooClient) GetJobStatus(ctx context.Context, brandID, jobID string) (*JobStatusResponse, error) {
	url := fmt.Sprintf("%s/menu/v3/brands/%s/jobs/%s", c.config.BaseURL, brandID, jobID)

	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result JobStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *DeliverooClient) GetMenuDownloadURL(ctx context.Context, brandID, menuID string) (string, error) {
	// Note : C'est un GET sur la V3
	url := fmt.Sprintf("%s/menu/v3/brands/%s/menus/%s", c.config.BaseURL, brandID, menuID)

	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result FetchMenuV3Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.URL, nil
}

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
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// Config contient les identifiants nécessaires
type Config struct {
	BasicAuth string // La chaîne Base64 (ClientID:Secret)
	IsSandbox bool
}

// DeliverooClient gère la communication avec l'API
type DeliverooClient struct {
	httpClient *http.Client
	config     Config

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

func NewDeliverooClient(httpClient *http.Client, config Config) *DeliverooClient {
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

	return c.refreshToken(ctx)
}

func (c *DeliverooClient) refreshToken(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)
	log.Info("DeliverooClient.refreshToken - refreshing")

	url := "https://auth-sandbox.developers.deliveroo.com/oauth2/token"
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

	log.Info("DeliverooClient.refreshToken - new token : " + c.accessToken)

	return c.accessToken, nil
}

// ==========================================
// HELPERS HTTP
// ==========================================

func (c *DeliverooClient) doRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, error) {
	log := logger.FromContext(ctx)
	log.Info("DeliverooClient.doRequest - " + url)

	var bodyReader io.Reader
	if payload != nil {
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshaling payload: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
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

	return c.httpClient.Do(req)
}

// ==========================================
// MÉTHODES MÉTIER (Correspondance PHP)
// ==========================================

// AcceptOrder correspond à $this->updateOrderStatus(..., ["status" => "accepted"])
func (c *DeliverooClient) AcceptOrder(ctx context.Context, brandOrderID string) error {
	url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", url.PathEscape(brandOrderID))
	payload := map[string]string{"status": "accepted"}

	log := logger.FromContext(ctx)
	log.Info("DeliverooClient.AcceptOrder - doRequest for order " + brandOrderID)

	resp, err := c.doRequest(ctx, "PATCH", url, payload)
	if err != nil {
		log.Info("DeliverooClient.AcceptOrder - error doing request")
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
	url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", brandOrderID)
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
	url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s", url.PathEscape(brandOrderID))

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
	url := fmt.Sprintf("https://api-sandbox.developers.deliveroo.com/order/v1/orders/%s/prep_stage", brandOrderID)

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

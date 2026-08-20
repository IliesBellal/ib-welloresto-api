package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"welloresto-api/internal/logger"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

type UberClient struct {
	config ConfigUberEats
	client *http.Client
}

func NewUberClient(cfg ConfigUberEats) *UberClient {
	return &UberClient{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// doJSONRequest est un helper générique
func (c *UberClient) doJSONRequest(ctx context.Context, method, url, token string, payload interface{}) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log := logger.FromContext(ctx)
	log.Info("UberClient.doJSONRequest - doJSONRequest : " + url + " response : " + resp.Status)
	log.Info("UberClient.doJSONRequest - token : " + token)
	log.Info("UberClient.doJSONRequest - payload : " + fmt.Sprint(payload))
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetNewToken appelle l'endpoint OAuth
func (c *UberClient) GetNewToken() (*UberAuthResponse, error) {
	data := url.Values{}
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("client_id", c.config.ClientID)
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "eats.order eats.report eats.store eats.store.orders.cancel eats.store.orders.read eats.store.status.read eats.store.status.write eats.store.orders.restaurantdelivery.status eats.byoc.position")

	resp, err := c.client.PostForm(c.config.TokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("uber auth error: status %d", resp.StatusCode)
	}

	var tokenResp UberAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

// ExchangeAuthCode transforme le code reçu par Uber en AccessToken/RefreshToken
func (c *UberClient) ExchangeAuthCode(ctx context.Context, code string, redirectURI string) (*TokenExchangeResponse, error) {
	data := url.Values{}
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("client_id", c.config.ClientID)
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	resp, err := c.client.PostForm("https://auth.uber.com/oauth/v2/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("uber token exchange error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp TokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

// GetMerchantStores récupère la liste des magasins associés au compte Uber du restaurateur
func (c *UberClient) GetMerchantStores(ctx context.Context, accessToken string) (*MerchantInfoResponse, error) {
	req, err := http.NewRequest("GET", "https://api.uber.com/v1/eats/stores", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("uber get stores error: status %d", resp.StatusCode)
	}

	var storesResp MerchantInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&storesResp); err != nil {
		return nil, err
	}
	return &storesResp, nil
}

// AcceptOrder envoie la requête d'acceptation
func (c *UberClient) AcceptOrder(ctx context.Context, uberOrderID string, token string, req UberAcceptRequest) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/order/%s/accept", c.config.BaseURL, uberOrderID)
	return c.doJSONRequest(ctx, "POST", endpoint, token, req)
}

func (c *UberClient) GetOrderByURL(ctx context.Context, url string, token string) (*ueModels.UberOrder, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("order_not_found")
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("uber api error %d", resp.StatusCode)
	}

	var order ueModels.UberOrder
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return nil, err
	}

	return &order, nil
}

// GetOrderDetails récupère l'état complet de la commande (pour la synchro)
func (c *UberClient) GetOrderDetails(uberOrderID string, token string) (*UberOrderDetails, error) {
	endpoint := fmt.Sprintf("%s/v1/delivery/order/%s", c.config.BaseURL, uberOrderID)

	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("order_not_found") // Erreur spécifique pour le handler 404
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api error %d", resp.StatusCode)
	}

	var details UberOrderDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}
	return &details, nil
}

// DenyOrder refuse une commande
func (c *UberClient) DenyOrder(ctx context.Context, uberOrderID string, token string, reasonType string, reasonID string, comment string) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/order/%s/deny", c.config.BaseURL, uberOrderID)
	payload := c.buildDenyPayload(reasonType, reasonID, comment)
	return c.doJSONRequest(ctx, "POST", endpoint, token, payload)
}

// CancelOrder annule une commande
func (c *UberClient) CancelOrder(ctx context.Context, uberOrderID string, token string, reasonType string, reasonID string, comment string) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/order/%s/cancel", c.config.BaseURL, uberOrderID)
	payload := c.buildDenyPayload(reasonType, reasonID, comment)
	return c.doJSONRequest(ctx, "POST", endpoint, token, payload)
}

// SetOrderReady indique que la commande est prête
func (c *UberClient) SetOrderReady(ctx context.Context, uberOrderID string, token string) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/order/%s/ready", c.config.BaseURL, uberOrderID)
	// Body vide souvent requis ou accepté
	return c.doJSONRequest(ctx, "POST", endpoint, token, nil)
}

// buildDenyPayload construit la structure JSON spécifique (avec les données hardcodées du PHP)
func (c *UberClient) buildDenyPayload(rType, rCode, info string) UberCancelRequest {
	return UberCancelRequest{
		DenyReason: DenyReasonDetails{
			Info:       rType, //info,
			Type:       rType,
			ClientCode: rCode,
			// Inclusion des métadonnées hardcodées comme dans ton PHP
			// Note: Dans une vraie implémentation Go propre, on passerait ça en paramètre,
			// mais je reproduis ici ton code PHP "item_metadata".
		},
	}
}

// UpdateBYOCStatus met à jour le statut de livraison pour une commande spécifique
func (c *UberClient) UpdateBYOCStatus(ctx context.Context, uberOrderID string, token string, status string) error {
	endpoint := fmt.Sprintf("%s/v1/eats/orders/%s/restaurantdelivery/status", c.config.BaseURL, uberOrderID)
	payload := BYOCStatusRequest{Status: status}
	return c.doJSONRequest(ctx, "POST", endpoint, token, payload)
}

// IngestLiveLocation reports the BYOC courier's (our delivery driver's) current position
// to Uber Eats. orderWorkflowUUID/restaurantUUID: see BYOCLocationRequest for the
// working hypothesis on what these identifiers are (documented — not confirmed against
// a live Uber sandbox).
func (c *UberClient) IngestLiveLocation(ctx context.Context, token, restaurantUUID, orderWorkflowUUID string, lat, lng float64, atMillis int64) error {
	endpoint := fmt.Sprintf("%s/v1/eats/byoc/restaurants/orders/event/location", c.config.BaseURL)
	payload := BYOCLocationRequest{
		LocationRequest: BYOCLocationRequestBody{
			OrderWorkflowUUID: orderWorkflowUUID,
			RestaurantUUID:    restaurantUUID,
			LocationEvents: []BYOCLocationEvent{
				{
					PositionEvent: BYOCPositionEvent{
						Point: BYOCPoint{Latitude: lat, Longitude: lng},
						Time:  BYOCEventTime{EpochMillis: atMillis},
					},
				},
			},
		},
	}
	return c.doJSONRequest(ctx, "POST", endpoint, token, payload)
}

// UpdateStorePrepTime met à jour la configuration de temps (Busy Mode ou Default Prep Time)
func (c *UberClient) UpdateStorePrepTime(ctx context.Context, storeID string, token string, req UberPrepTimeRequest) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/store/%s/update-store-prep-time", c.config.BaseURL, storeID)
	return c.doJSONRequest(ctx, "POST", endpoint, token, req)
}

// UpdateStoreStatus met le magasin hors ligne/en ligne
func (c *UberClient) UpdateStoreStatus(ctx context.Context, storeID string, token string, req UberStoreStatusRequest) error {
	endpoint := fmt.Sprintf("%s/v1/delivery/store/%s/update-store-status", c.config.BaseURL, storeID)
	return c.doJSONRequest(ctx, "POST", endpoint, token, req)
}

// GetMenu récupère le menu
func (c *UberClient) GetMenu(storeID string, token string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/v2/eats/stores/%s/menus", c.config.BaseURL, storeID)
	// Note: GET request with body is unusual but supported by some APIs.
	// However, usually param is query string. PHP logic sends body in GET.
	// Go http.NewRequest supports body in GET.
	payload := map[string]string{"menu_type": "MENU_TYPE_FULFILLMENT_DELIVERY"}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("GET", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api error %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// UpdateItemState suspend ou active un item
func (c *UberClient) UpdateItemState(ctx context.Context, storeID string, itemID string, token string, req UberItemSuspensionRequest) error {
	endpoint := fmt.Sprintf("%s/v2/eats/stores/%s/menus/items/%s", c.config.BaseURL, storeID, itemID)
	return c.doJSONRequest(ctx, "POST", endpoint, token, req)
}

// SyncMenu pousse un menu vers l'API Uber Eats (PUT /v2/eats/stores/{storeID}/menus)
func (c *UberClient) SyncMenu(ctx context.Context, storeID string, token string, menu interface{}) error {
	endpoint := fmt.Sprintf("%s/v2/eats/stores/%s/menus", c.config.BaseURL, storeID)

	body, err := json.Marshal(menu)
	if err != nil {
		return fmt.Errorf("failed to marshal menu: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("uber api error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

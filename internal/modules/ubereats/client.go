package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"welloresto-api/internal/logger"
	requestlogger "welloresto-api/internal/middleware/request_logger"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

type UberClient struct {
	config ConfigUberEats
	client *http.Client
}

func NewUberClient(cfg ConfigUberEats) *UberClient {
	return &UberClient{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second, Transport: requestlogger.Transport(nil)},
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
	data.Set("scope", "eats.order eats.report eats.store eats.store.orders.cancel eats.store.orders.read eats.store.status.read eats.store.status.write eats.store.orders.restaurantdelivery.status") //eats.byoc.position retiré en attendant l'ajout du scope par Uber

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

// ExchangeAuthCode échange le code reçu au retour d'Uber contre le token du
// restaurateur (scope eats.pos_provisioning). Le corps de la réponse n'est
// jamais remonté dans l'erreur : il porterait le token en cas de succès
// partiel, et l'appel est déjà tracé (masqué) par requestlogger.
func (c *UberClient) ExchangeAuthCode(ctx context.Context, code string, redirectURI string) (*TokenExchangeResponse, error) {
	data := url.Values{}
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("client_id", c.config.ClientID)
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("uber token exchange error: status %d", resp.StatusCode)
	}

	var tokenResp TokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("uber token exchange: empty access token")
	}
	return &tokenResp, nil
}

// apiBase : hôte de l'API Uber Eats (UBER_EATS_BASE_URL), https://api.uber.com
// par défaut.
func (c *UberClient) apiBase() string {
	if b := strings.TrimRight(strings.TrimSpace(c.config.BaseURL), "/"); b != "" {
		return b
	}
	return "https://api.uber.com"
}

// maxStorePages borne la pagination de ListStores (50 restaurants par page) :
// au-delà, c'est un groupe qu'on n'onboarde pas depuis l'écran du back-office.
const maxStorePages = 10

// ListStores liste tous les restaurants accessibles avec le token du
// restaurateur (GET /v1/eats/stores, paginé par start_key / next_key).
func (c *UberClient) ListStores(ctx context.Context, userToken string) ([]UberStoreInfo, error) {
	var all []UberStoreInfo
	startKey := ""
	for page := 0; page < maxStorePages; page++ {
		q := url.Values{}
		q.Set("limit", "50")
		if startKey != "" {
			q.Set("start_key", startKey)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase()+"/v1/eats/stores?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+userToken)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		var body UberStoresPage
		status := resp.StatusCode
		if status < 300 {
			err = json.NewDecoder(resp.Body).Decode(&body)
		}
		resp.Body.Close()
		if status >= 300 {
			return nil, fmt.Errorf("uber list stores error: status %d", status)
		}
		if err != nil {
			return nil, fmt.Errorf("uber list stores: %w", err)
		}

		all = append(all, body.Stores...)
		if body.NextKey == nil || *body.NextKey == "" {
			return all, nil
		}
		startKey = *body.NextKey
	}
	return all, nil
}

// ErrUberStoreNotFound : Uber répond 404 sur le restaurant (déjà retiré, ou
// inaccessible avec ce token).
var ErrUberStoreNotFound = errors.New("uber eats: store not found")

// ProvisionStore active l'intégration Wello Resto sur un restaurant
// (POST /v1/eats/stores/{store_id}/pos_data, token du restaurateur).
func (c *UberClient) ProvisionStore(ctx context.Context, userToken, storeID string, body PosDataRequest) error {
	return c.posData(ctx, http.MethodPost, userToken, storeID, body)
}

// DeprovisionStore retire l'intégration d'un restaurant
// (DELETE /v1/eats/stores/{store_id}/pos_data, token du restaurateur).
func (c *UberClient) DeprovisionStore(ctx context.Context, userToken, storeID string) error {
	return c.posData(ctx, http.MethodDelete, userToken, storeID, nil)
}

func (c *UberClient) posData(ctx context.Context, method, userToken, storeID string, body any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	endpoint := fmt.Sprintf("%s/v1/eats/stores/%s/pos_data", c.apiBase(), url.PathEscape(storeID))
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrUberStoreNotFound
	}
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("uber pos_data %s error %d: %s", method, resp.StatusCode, string(respBody))
	}
	return nil
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

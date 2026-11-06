package deliveroo

// Connexion d'un site Deliveroo depuis le back-office
// (docs/marketplace-onboarding.md).
//
// Deliveroo n'a pas de flux OAuth : le restaurateur relie son site à Wello
// Resto dans le Partner Hub (Integrations → Connect sites → Wello Resto) en y
// saisissant un « Site location ID ». Cet identifiant est le NÔTRE : Deliveroo
// le renvoie tel quel dans chaque commande (order.location_id, « The in-store
// system location ID ») et c'est la clé de routage des webhooks
// (integration_deliveroo.location_id). Wello Resto le fixe donc et l'affiche :
// l'identifiant de l'établissement (merchant_id), suffixé -2, -3… pour un site
// supplémentaire — la convention déjà en place pour les établissements reliés
// à la main.
//
// Une fois la liaison faite chez Deliveroo, « Vérifier la connexion » appelle
// l'API Deliveroo avec nos identifiants d'intégrateur : elle ne répond que pour
// un site effectivement relié à Wello Resto, ce qui vaut preuve. Le brand_id
// obtenu est enregistré et le site est réglé pour envoyer les Order Events.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

// maxExtraSites : nombre de sites Deliveroo qu'un établissement peut relier
// depuis le back-office (merchant_id, merchant_id-2 … merchant_id-9).
const maxExtraSites = 9

// orderEventsWebhookType : type de webhook traité par /webhooks/deliveroo/orders
// (order.new, order.status_update).
const orderEventsWebhookType = "ORDER_EVENTS"

var (
	// ErrLocationNotAllowed : identifiant qui n'appartient pas à cet établissement.
	ErrLocationNotAllowed = errors.New("deliveroo: location id not allowed for this merchant")
	// ErrLocationLinkedElsewhere : site déjà relié à un autre établissement Wello Resto.
	ErrLocationLinkedElsewhere = errors.New("deliveroo: location linked to another merchant")
	// ErrLocationNotLinked : Deliveroo ne connaît pas (encore) ce site pour Wello Resto.
	ErrLocationNotLinked = errors.New("deliveroo: location not linked to the integration yet")
	// ErrNoLocationAvailable : tous les identifiants de l'établissement sont pris.
	ErrNoLocationAvailable = errors.New("deliveroo: no location id available")
)

// ConnectInfo : ce que le back-office affiche pour relier un site.
type ConnectInfo struct {
	// LocationID : identifiant à saisir dans le Partner Hub pour le prochain
	// site à relier ("" si la limite est atteinte).
	LocationID string `json:"location_id"`
	// Sites : sites déjà connus de l'établissement.
	Sites []ConnectedSite `json:"sites"`
}

// ConnectedSite : un site Deliveroo de l'établissement.
type ConnectedSite struct {
	LocationID string `json:"location_id"`
	Enabled    bool   `json:"enabled"`
}

// ConnectResult : site relié.
type ConnectResult struct {
	LocationID string `json:"location_id"`
	BrandID    string `json:"brand_id"`
}

// locationCandidates : identifiants que cet établissement peut utiliser.
func locationCandidates(merchantID string) []string {
	out := []string{merchantID}
	for i := 2; i <= maxExtraSites; i++ {
		out = append(out, merchantID+"-"+strconv.Itoa(i))
	}
	return out
}

// GetConnectInfo renvoie l'identifiant à saisir dans le Partner Hub et les
// sites déjà connus. L'identifiant proposé est le premier libre : pas encore
// actif pour cet établissement, ni utilisé par un autre.
func (s *DeliverooService) GetConnectInfo(ctx context.Context, merchantID string) (*ConnectInfo, error) {
	rows, err := s.repo.ListSites(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	info := &ConnectInfo{Sites: rows}
	active := map[string]bool{}
	for _, site := range rows {
		if site.Enabled {
			active[site.LocationID] = true
		}
	}
	for _, candidate := range locationCandidates(merchantID) {
		if active[candidate] {
			continue
		}
		owner, err := s.repo.ActiveLocationOwner(ctx, candidate)
		if err != nil {
			return nil, err
		}
		if owner != "" && owner != merchantID {
			continue
		}
		info.LocationID = candidate
		break
	}
	return info, nil
}

// Connect vérifie auprès de Deliveroo que le site locationID est relié à Wello
// Resto, puis active le compte correspondant.
func (s *DeliverooService) Connect(ctx context.Context, merchantID, locationID string) (*ConnectResult, error) {
	log := logger.FromContext(ctx)
	locationID = strings.TrimSpace(locationID)

	allowed := false
	for _, c := range locationCandidates(merchantID) {
		if c == locationID {
			allowed = true
			break
		}
	}
	if !allowed && locationID != "" {
		// Identifiant historique posé à la main (ex. site de test) : accepté
		// s'il appartient déjà à cet établissement.
		acc, err := s.repo.GetAccountByLocationID(ctx, merchantID, locationID)
		if err != nil {
			return nil, err
		}
		allowed = acc != nil
	}
	if !allowed {
		return nil, ErrLocationNotAllowed
	}

	owner, err := s.repo.ActiveLocationOwner(ctx, locationID)
	if err != nil {
		return nil, err
	}
	if owner != "" && owner != merchantID {
		return nil, ErrLocationLinkedElsewhere
	}

	brandID, err := s.client.LookupBrandID(ctx, locationID)
	if err != nil {
		if !errors.Is(err, ErrLocationNotLinked) {
			log.Error("deliveroo onboarding: brand lookup failed",
				zap.String("merchant_id", merchantID), zap.String("location_id", locationID), zap.Error(err))
		}
		return nil, err
	}

	if err := s.repo.ActivateSite(ctx, merchantID, locationID, brandID); err != nil {
		return nil, err
	}
	log.Info("deliveroo onboarding: site connected",
		zap.String("merchant_id", merchantID), zap.String("location_id", locationID), zap.String("brand_id", brandID))

	// Best-effort : le compte est actif même si le réglage échoue (il se
	// refait à chaque vérification). Voir docs/marketplace-onboarding.md.
	if err := s.ensureOrderEventsWebhook(ctx, brandID, locationID); err != nil {
		log.Error("deliveroo onboarding: sites-config not updated",
			zap.String("merchant_id", merchantID), zap.String("location_id", locationID), zap.String("brand_id", brandID), zap.Error(err))
	}

	return &ConnectResult{LocationID: locationID, BrandID: brandID}, nil
}

// ensureOrderEventsWebhook règle le site pour envoyer les Order Events (le seul
// format que traite /webhooks/deliveroo/orders). La configuration de la marque
// est relue puis renvoyée en entier : la doc ne dit pas si PUT remplace la
// liste ou la fusionne, renvoyer la liste complète est correct dans les deux cas.
func (s *DeliverooService) ensureOrderEventsWebhook(ctx context.Context, brandID, locationID string) error {
	cfg, err := s.client.GetSitesConfig(ctx, brandID)
	if err != nil {
		return err
	}
	sites := make([]SiteWebhookConfig, 0, len(cfg.Sites)+1)
	found := false
	for _, site := range cfg.Sites {
		entry := SiteWebhookConfig{LocationID: string(site.LocationID), OrdersAPIWebhookType: site.OrdersAPIWebhookType}
		if entry.LocationID == locationID {
			found = true
			if entry.OrdersAPIWebhookType == orderEventsWebhookType || entry.OrdersAPIWebhookType == "POS_AND_ORDER_EVENTS" {
				return nil // déjà bon : rien à envoyer
			}
			entry.OrdersAPIWebhookType = orderEventsWebhookType
		}
		sites = append(sites, entry)
	}
	if !found {
		sites = append(sites, SiteWebhookConfig{LocationID: locationID, OrdersAPIWebhookType: orderEventsWebhookType})
	}
	return s.client.PutSitesConfig(ctx, brandID, sites)
}

// ─── client ──────────────────────────────────────────────────────────────────

// flexString accepte un identifiant JSON chaîne ou nombre (la doc Deliveroo
// type location_id en chaîne, ses exemples en nombre).
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

// SitesConfigResponse : GET /order/v1/integrator/brands/{brand_id}/sites-config.
type SitesConfigResponse struct {
	Sites []struct {
		LocationID           flexString `json:"location_id"`
		Name                 string     `json:"name"`
		OrdersAPIWebhookType string     `json:"orders_api_webhook_type"`
	} `json:"sites"`
}

// SiteWebhookConfig : une entrée du PUT sites-config (name est en lecture seule).
type SiteWebhookConfig struct {
	LocationID           string `json:"location_id"`
	OrdersAPIWebhookType string `json:"orders_api_webhook_type"`
}

// LookupBrandID : brand_id d'un site relié à Wello Resto
// (GET /site/v1/restaurant_locations/{location_id}). 400/403/404 ou une
// réponse sans marque signifient que Deliveroo ne connaît pas ce site pour
// notre intégration : ErrLocationNotLinked.
func (c *DeliverooClient) LookupBrandID(ctx context.Context, locationID string) (string, error) {
	endpoint := fmt.Sprintf("%s/site/v1/restaurant_locations/%s", c.config.BaseURL, url.PathEscape(locationID))
	resp, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		return "", ErrLocationNotLinked
	}
	if resp.StatusCode >= 300 {
		return "", c.handleError(resp)
	}
	var result SiteBrandResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("deliveroo: réponse illisible: %w", err)
	}
	for _, b := range result.BrandID {
		if strings.TrimSpace(b) != "" {
			return b, nil
		}
	}
	return "", ErrLocationNotLinked
}

// GetSitesConfig : type de webhook de chaque site de la marque.
func (c *DeliverooClient) GetSitesConfig(ctx context.Context, brandID string) (*SitesConfigResponse, error) {
	var out SitesConfigResponse
	if err := c.getJSON(ctx, c.sitesConfigURL(brandID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutSitesConfig règle le type de webhook des sites de la marque.
func (c *DeliverooClient) PutSitesConfig(ctx context.Context, brandID string, sites []SiteWebhookConfig) error {
	resp, err := c.doRequest(ctx, http.MethodPut, c.sitesConfigURL(brandID), map[string]any{"sites": sites})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return c.handleError(resp)
	}
	return nil
}

func (c *DeliverooClient) sitesConfigURL(brandID string) string {
	return fmt.Sprintf("%s/order/v1/integrator/brands/%s/sites-config", c.config.BaseURL, url.PathEscape(brandID))
}

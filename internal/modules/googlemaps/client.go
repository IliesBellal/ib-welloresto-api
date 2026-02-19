package googlemaps

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"welloresto-api/internal/config"
)

type GoogleMapsClient interface {
	FetchRoute(origin, destination string) ([]byte, error)
}

type googleMapsClient struct {
	apiKey string
	client *http.Client
}

func NewGoogleMapsClient(cfg config.GoogleConfig) GoogleMapsClient {
	return &googleMapsClient{
		apiKey: cfg.APIKey,
		client: &http.Client{},
	}
}

func (c *googleMapsClient) FetchRoute(origin, destination string) ([]byte, error) {
	baseURL := "https://maps.googleapis.com/maps/api/directions/json"

	// Construction des paramètres
	params := url.Values{}
	params.Add("origin", origin)
	params.Add("destination", destination)
	params.Add("key", c.apiKey)

	// Tes paramètres spécifiques du prompt
	params.Add("traffic_model", "pessimistic")
	params.Add("departure_time", "now")
	params.Add("avoid", "tolls")

	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	resp, err := c.client.Get(fullURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google api error: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type PlaceDetailsResponse struct {
	Status string `json:"status"`
	Result struct {
		FormattedAddress string `json:"formatted_address"`
		Geometry         struct {
			Location struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"location"`
		} `json:"geometry"`
	} `json:"result"`
}

func (c *googleMapsClient) GetAddressFromPlaceID(placeID string) (*PlaceDetailsResponse, error) {
	url := fmt.Sprintf(
		"https://maps.googleapis.com/maps/api/place/details/json?place_id=%s&fields=formatted_address,geometry&key=%s",
		placeID, c.apiKey,
	)

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data PlaceDetailsResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	return &data, err
}

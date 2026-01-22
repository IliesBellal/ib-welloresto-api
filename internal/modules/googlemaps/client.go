package googlemaps

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type GoogleMapsClient interface {
	FetchRoute(origin, destination string) ([]byte, error)
}

type googleMapsClient struct {
	apiKey string
	client *http.Client
}

func NewGoogleMapsClient(apiKey string) GoogleMapsClient {
	return &googleMapsClient{
		apiKey: apiKey,
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

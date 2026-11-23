package scannorderseo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Recherche de la fiche Google d'un établissement par l'API Places de Google
// (« Find Place from Text », même API Places que googlemaps.GetAddressFromPlaceID,
// même clé GOOGLE_API_KEY). Sans clé ou si Google refuse, le back-office
// propose la saisie manuelle des liens.

const placesFindURL = "https://maps.googleapis.com/maps/api/place/findplacefromtext/json"

// PlaceFinder cherche des lieux Google par texte, autour d'un point (0, 0 :
// sans préférence de position).
type PlaceFinder interface {
	FindPlaces(ctx context.Context, text string, lat, lng float64) ([]foundPlace, error)
}

type foundPlace struct {
	PlaceID string `json:"place_id"`
	Name    string `json:"name"`
	Address string `json:"formatted_address"`
}

type placesClient struct {
	apiKey   string
	endpoint string
	http     *http.Client
}

// NewPlacesClient : nil sans clé (recherche indisponible).
func NewPlacesClient(apiKey string) PlaceFinder {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	return &placesClient{apiKey: apiKey, endpoint: placesFindURL, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *placesClient) FindPlaces(ctx context.Context, text string, lat, lng float64) ([]foundPlace, error) {
	params := url.Values{}
	params.Set("input", text)
	params.Set("inputtype", "textquery")
	params.Set("fields", "place_id,name,formatted_address")
	params.Set("language", "fr")
	if lat != 0 || lng != 0 {
		params.Set("locationbias", fmt.Sprintf("circle:2000@%f,%f", lat, lng))
	}
	params.Set("key", c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google places: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Status       string       `json:"status"`
		ErrorMessage string       `json:"error_message"`
		Candidates   []foundPlace `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("google places: réponse illisible: %w", err)
	}
	switch body.Status {
	case "OK":
		return body.Candidates, nil
	case "ZERO_RESULTS":
		return nil, nil
	default:
		// REQUEST_DENIED (API non activée, clé restreinte), OVER_QUERY_LIMIT…
		return nil, fmt.Errorf("google places: %s %s", body.Status, body.ErrorMessage)
	}
}

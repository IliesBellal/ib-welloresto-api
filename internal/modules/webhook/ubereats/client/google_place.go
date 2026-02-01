package client

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type GoogleClient struct {
	APIKey string
	Client *http.Client
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

func (g *GoogleClient) GetAddressFromPlaceID(placeID string) (*PlaceDetailsResponse, error) {
	url := fmt.Sprintf(
		"https://maps.googleapis.com/maps/api/place/details/json?place_id=%s&fields=formatted_address,geometry&key=%s",
		placeID, g.APIKey,
	)

	resp, err := g.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data PlaceDetailsResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	return &data, err
}

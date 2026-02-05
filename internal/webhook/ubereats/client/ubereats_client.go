package client

import (
	"encoding/json"
	"net/http"
)

type UberEatsClient struct {
	Client *http.Client
}

func (c *UberEatsClient) GetOrderByURL(url string, bearer string, dest interface{}) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(dest)
}

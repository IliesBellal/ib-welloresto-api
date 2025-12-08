package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type UberEatsClient struct {
	httpClient *http.Client
}

func NewUberEatsClient() *UberEatsClient {
	return &UberEatsClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *UberEatsClient) PostReady(ctx context.Context, brandOrderID string, token string) (*http.Response, error) {
	url := "https://api.uber.com/v1/delivery/order/" + brandOrderID + "/ready"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *UberEatsClient) CancelOrder(ctx context.Context, brandOrderID string, token string, payload []byte) (*http.Response, error) {

	url := "https://api.uber.com/v1/delivery/order/" + brandOrderID + "/cancel"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *UberEatsClient) DenyOrder(ctx context.Context, brandOrderID, token, jsonBody string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.uber.com/v1/delivery/order/"+brandOrderID+"/deny",
		strings.NewReader(jsonBody),
	)

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *UberEatsClient) AcceptOrder(ctx context.Context, bearerToken, uberOrderID string, payload map[string]interface{}) (*http.Response, error) {
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://api.uber.com/v1/delivery/order/%s/accept", uberOrderID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

// Placeholder version
func (c *UberEatsClient) FinishOrderIfDoesNotExist(ctx context.Context, bearerToken, uberOrderID string) error {
	// On laisse vide pour l'instant
	return nil
}

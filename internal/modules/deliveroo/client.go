package deliveroo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type DeliverooClient struct {
	httpClient *http.Client
}

func NewDeliverooClient() *DeliverooClient {
	return &DeliverooClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *DeliverooClient) CreateStageForOrder(ctx context.Context, brandOrderID string, bearer string, payload map[string]any) (*http.Response, error) {

	url := "https://api.developers.deliveroo.com/order/v1/orders/" + brandOrderID + "/prep_stage"

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClient) UpdateOrderStatus(ctx context.Context, brandOrderID string, bearer string, payload map[string]any) (*http.Response, error) {

	url := "https://api.developers.deliveroo.com/order/v1/orders/" + brandOrderID

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClient) AcceptOrder(ctx context.Context, bearer string, brandOrderID string) (int, map[string]interface{}, error) {

	url := fmt.Sprintf("https://api.deliveroo.com/partner/v1/orders/%s/status", brandOrderID)

	body, _ := json.Marshal(map[string]string{"status": "accepted"})

	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("deliveroo http: %w", err)
	}
	defer resp.Body.Close()

	var parsed map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&parsed)

	return resp.StatusCode, parsed, nil
}

func (c *DeliverooClient) PatchOrderStatus(ctx context.Context, brandOrderID string, payload map[string]interface{}, token string) (*http.Response, error) {
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodPatch,
		"https://api.developers.deliveroo.com/order/v1/orders/"+brandOrderID,
		bytes.NewReader(body),
	)

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClient) CreateStage(ctx context.Context, brandOrderID string, payload map[string]interface{}, token string) (*http.Response, error) {
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.developers.deliveroo.com/order/v1/orders/"+brandOrderID+"/prep_stage",
		bytes.NewReader(body),
	)

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClient) CancelOrder(ctx context.Context, brandOrderID string, token string, payload []byte) (*http.Response, error) {

	url := "https://api.uber.com/v1/delivery/order/" + brandOrderID + "/cancel"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

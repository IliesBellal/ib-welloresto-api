package deliveroo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"welloresto-api/internal/logger"
)

type DeliverooClientOld struct {
	httpClient *http.Client
}

func NewDeliverooClientOld() *DeliverooClient {
	return &DeliverooClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *DeliverooClientOld) CreateStageForOrder(ctx context.Context, brandOrderID string, bearer string, payload map[string]any) (*http.Response, error) {

	url := "https://api.developers.deliveroo.com/order/v1/orders/" + brandOrderID + "/prep_stage"

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClientOld) UpdateOrderStatus(ctx context.Context, brandOrderID string, bearer string, payload map[string]any) (*http.Response, error) {

	url := "https://api.developers.deliveroo.com/order/v1/orders/" + brandOrderID

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

func (c *DeliverooClientOld) AcceptOrder(ctx context.Context, brandOrderID string) (int, map[string]interface{}, error) {
	bearer := "M3A2cDk3Y2UzcTRlbzR0NWwxOWJhdmlwNHM6dXE5bDE4dm5nODdlczh1MzFkMGVwM2ZvOHBjazdvZDN2N3EyYW5tZ25tZG1oZmFtZGdp"

	//url := fmt.Sprintf("https://api.deliveroo.com/partner/v1/orders/%s/status", brandOrderID)
	url := fmt.Sprintf("https://api-sandbox.deliveroo.com/partner/v1/orders/%s/status", brandOrderID)
	log := logger.FromContext(ctx)
	log.Info("DeliverooClient.AcceptOrder - calling " + url)

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

func (c *DeliverooClientOld) PatchOrderStatus(ctx context.Context, brandOrderID string, payload map[string]interface{}, token string) (*http.Response, error) {
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

func (c *DeliverooClientOld) CreateStage(ctx context.Context, brandOrderID string, payload map[string]interface{}, token string) (*http.Response, error) {
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

func (c *DeliverooClientOld) CancelOrder(ctx context.Context, brandOrderID string, token string, payload []byte) (*http.Response, error) {

	url := "https://api.uber.com/v1/delivery/order/" + brandOrderID + "/cancel"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

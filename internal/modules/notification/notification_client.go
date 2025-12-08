// notification/notification_client.go

package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

type FCMClient struct {
	HTTP *http.Client
}

func NewFCMClient() *FCMClient {
	return &FCMClient{
		HTTP: &http.Client{},
	}
}

func (c *FCMClient) SendFCMMessage(ctx context.Context, token string, accessToken string, message map[string]interface{}) (*http.Response, error) {

	body, _ := json.Marshal(message)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://fcm.googleapis.com/v1/projects/wello-resto-150721/messages:send",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	return c.HTTP.Do(req)
}

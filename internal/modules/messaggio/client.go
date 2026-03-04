package messaggio

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type MessaggioClient interface {
	SendSMS(ctx context.Context, login string, from string, phone string, content string) error
}

type messaggioClient struct {
	httpClient *http.Client
}

func NewMessaggioClient() MessaggioClient {
	return &messaggioClient{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *messaggioClient) SendSMS(
	ctx context.Context,
	login string,
	from string,
	phone string,
	content string,
) error {

	body := map[string]interface{}{
		"recipients": []map[string]string{
			{"phone": phone},
		},
		"channels": []string{"sms"},
		"sms": map[string]interface{}{
			"from": from,
			"content": []map[string]string{
				{
					"type": "text",
					"text": content,
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://msg.messaggio.com/api/v1/send",
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Messaggio-Login", login)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return err
	}

	return nil
}

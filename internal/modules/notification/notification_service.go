// notification/notification_service.go

package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

type NotificationService struct {
	repo   *NotificationRepository
	client *FCMClient
	tokenm FCMTokenManager // interface (voir plus bas)
}

func NewNotificationService(repo *NotificationRepository, client *FCMClient, tokenm FCMTokenManager) *NotificationService {
	return &NotificationService{
		repo:   repo,
		client: client,
		tokenm: tokenm,
	}
}

func (s *NotificationService) log(msg string) {
	log.Printf("[NOTIFICATION] %s\n", msg)
}

func (s *NotificationService) SendNotificationAsync(
	merchantID string,
	orderID string,
	nType string,
) error {
	ctx := context.Background()

	tokens, err := s.repo.GetDeviceTokens(ctx, merchantID)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		s.log(fmt.Sprintf("No tokens for merchant %d", merchantID))
		return nil
	}

	for _, t := range tokens {
		token := t
		go s.sendWithoutPayload(ctx, merchantID, orderID, token, nType)
	}

	return nil
}

func (s *NotificationService) SendNotificationAsyncWithPayload(
	merchantID string,
	nType string,
	entityID string,
	payload map[string]interface{},
) error {
	ctx := context.Background()

	tokens, err := s.repo.GetDeviceTokens(ctx, merchantID)
	if err != nil {
		return err
	}

	for _, t := range tokens {
		token := t
		go s.sendWithPayload(ctx, merchantID, token, nType, entityID, payload)
	}

	return nil
}

func (s *NotificationService) sendWithoutPayload(
	ctx context.Context,
	merchantID string,
	orderID string,
	token string,
	nType string,
) {

	accessToken, err := s.getFCMToken(ctx)
	if err != nil || accessToken == "" {
		s.log("Missing FCM access token")
		return
	}

	message := map[string]interface{}{
		"message": map[string]interface{}{
			"token": token,
			"notification": map[string]string{
				"title": "Nouvelle commande reçue",
				"body":  fmt.Sprintf("Vous avez une nouvelle commande. MerchantID Commande : %d", orderID),
			},
			"data": map[string]interface{}{
				"type":        nType,
				"merchant_id": merchantID,
				"order_id":    orderID,
				"entity_id":   orderID,
			},
			"android": map[string]interface{}{
				"priority": "high",
			},
			"apns": map[string]interface{}{
				"headers": map[string]string{"apns-priority": "10"},
				"payload": map[string]interface{}{
					"aps": map[string]interface{}{
						"sound":             "default",
						"content-available": 1,
					},
				},
			},
		},
	}

	resp, err := s.client.SendFCMMessage(ctx, token, accessToken, message)
	if err != nil {
		s.log(fmt.Sprintf("sendWithoutPayload error: %s", err))
		return
	}
	defer resp.Body.Close()

	s.log(fmt.Sprintf("FCM result code=%d", resp.StatusCode))
}

func (s *NotificationService) sendWithPayload(
	ctx context.Context,
	merchantID string,
	token string,
	nType string,
	entityID string,
	payload map[string]interface{},
) {

	accessToken, err := s.getFCMToken(ctx)
	if err != nil || accessToken == "" {
		s.log("Missing FCM token")
		return
	}

	data := map[string]interface{}{
		"type":        nType,
		"merchant_id": merchantID,
		"payload":     payload,
		"order_id":    entityID,
		"entity_id":   entityID,
	}

	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"token": token,
			"data":  data,
			"android": map[string]interface{}{
				"priority": "high",
			},
			"apns": map[string]interface{}{
				"headers": map[string]string{
					"apns-priority": "10",
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(msg)
	if len(bodyBytes) > 4000 {
		delete(data, "payload")
		s.log("Payload removed: message too large")
		bodyBytes, _ = json.Marshal(msg)
	}

	resp, err := s.client.SendFCMMessage(ctx, token, accessToken, msg)
	if err != nil {
		s.log("sendWithPayload error: " + err.Error())
		return
	}
	defer resp.Body.Close()

	s.log(fmt.Sprintf("FCM payload code=%d", resp.StatusCode))
}

func (s *NotificationService) getFCMToken(ctx context.Context) (string, error) {

	token, err := s.repo.GetValidFCMToken(ctx)
	if err != nil {
		s.log("Error in getFCMToken : " + err.Error())
		return "", err
	}

	if token != "" {
		s.log("token found : " + token)
		return token, nil
	}

	// Generate new token via FCM token manager
	token, err = s.tokenm.GenerateToken(ctx)
	if err != nil {
		s.log("Error in getFCMToken after generation : " + err.Error())
		return "", err
	}
	s.log("New BasicAuth generated : " + token)

	_ = s.repo.StoreFCMToken(ctx, token)

	return token, nil
}

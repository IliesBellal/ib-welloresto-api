// notification/notification_service.go

package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"welloresto-api/internal/logger"
)

type NotificationService struct {
	repo   *NotificationRepository
	client *FCMClient
	tokenm FCMTokenManager // interface (voir plus bas)
	mu     sync.Mutex
}

func NewNotificationService(repo *NotificationRepository, client *FCMClient, tokenm FCMTokenManager) *NotificationService {
	return &NotificationService{
		repo:   repo,
		client: client,
		tokenm: tokenm,
	}
}

func (s *NotificationService) SendNotificationAsync(merchantID, orderID, nType string) error {
	ctx := context.Background()

	tokens, err := s.repo.GetDeviceTokens(ctx, merchantID)
	log := logger.FromContext(ctx)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		log.Info("No tokens for merchant " + merchantID)
		return nil
	}

	// 👉 RÉCUPÉRATION DU TOKEN ICI (Une seule fois pour toute la volée)
	accessToken, err := s.getFCMToken(ctx)
	if err != nil || accessToken == "" {
		log.Error("Impossible de récupérer le token FCM access: " + err.Error())
		return err // On arrête tout si on ne peut pas s'authentifier
	}

	for _, t := range tokens {
		token := t
		// 👉 ON PASSE L'ACCESS TOKEN EN PARAMÈTRE
		go s.sendWithoutPayload(ctx, merchantID, orderID, token, nType, accessToken)
	}

	log.Info("Successfully sent notification for merchant " + merchantID + " order " + orderID)

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

func (s *NotificationService) sendWithoutPayload(ctx context.Context, merchantID, orderID, token, nType, accessToken string) {

	log := logger.FromContext(ctx)
	if accessToken == "" {
		log.Info("Missing FCM access token")
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
		//log.Info("sendWithoutPayload error: " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		//log.Warn("FCM result code=" + resp.Status + " for token " + token)
	}
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
	log := logger.FromContext(ctx)
	if err != nil || accessToken == "" {
		log.Error("Missing FCM Token")
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
		log.Warn("Payload removed: message too large")
		bodyBytes, _ = json.Marshal(msg)
	}

	resp, err := s.client.SendFCMMessage(ctx, token, accessToken, msg)
	if err != nil {
		log.Error("sendWithPayload error: " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Warn("FCM result code=" + resp.Status)
	}
}

func (s *NotificationService) getFCMToken(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)

	// 👉 ON VERROUILLE. Si une autre goroutine arrive, elle mettra l'exécution en pause ici
	// jusqu'à ce que la première ait fini (Unlock).
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. On vérifie en base (la goroutine en attente verra le token fraîchement créé par la 1ère)
	token, err := s.repo.GetValidFCMToken(ctx)
	if err != nil {
		log.Error("Error in getFCMToken : " + err.Error())
		return "", err
	}

	if token != "" {
		return token, nil
	}

	// 2. Si vraiment aucun token, on en génère un
	token, err = s.tokenm.GenerateToken(ctx)
	if err != nil {
		log.Error("Error in getFCMToken after generation : " + err.Error())
		return "", err
	}
	log.Info("New BasicAuth generated")

	_ = s.repo.StoreFCMToken(ctx, token)

	return token, nil
}

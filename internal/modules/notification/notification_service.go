// notification/notification_service.go

package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"
	"welloresto-api/internal/infrastructure/websocket"
	"welloresto-api/internal/logger"

	"golang.org/x/sync/singleflight"
)

type NotificationService struct {
	repo   *NotificationRepository
	client *FCMClient
	tokenm FCMTokenManager // interface (voir plus bas)
	hub    *websocket.Hub  // Hub WebSocket optionnel

	// Variables de cache en mémoire
	mu          sync.RWMutex // RWMutex permet de multiples lectures simultanées
	cachedToken string
	tokenExpiry time.Time

	sfGroup singleflight.Group // Bloque la génération concurrente
}

func NewNotificationService(repo *NotificationRepository, client *FCMClient, tokenm FCMTokenManager, hub *websocket.Hub) *NotificationService {
	return &NotificationService{
		repo:   repo,
		client: client,
		tokenm: tokenm,
		hub:    hub,
	}
}

/*
Will send notifications to all devices of a merchant for a given order and notification type, without payload.
The notification type (nType) can be used by the client app to determine how to handle the notification.
Will mange Go routines and FCM token retrieval to optimize performance and avoid redundant token generation.
No need to put in Go routine here, the function itself will handle that for each token.
*/
func (s *NotificationService) SendNotificationAsync(merchantID, orderID, nType string) error {
	ctx := context.Background()

	// Dispatcher via WebSocket si le hub est disponible
	if s.hub != nil {
		wsPayload := map[string]interface{}{
			"type":      nType,
			"entity_id": orderID,
		}
		wsPayloadJSON, _ := json.Marshal(wsPayload)
		s.hub.BroadcastToMerchant(merchantID, wsPayloadJSON)
	}

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
		go s.sendWithoutPayload(ctx, merchantID, orderID, token, nType, accessToken, true)
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

func (s *NotificationService) sendWithoutPayload(ctx context.Context, merchantID, orderID, deviceToken, nType, accessToken string, canRetry bool) {

	log := logger.FromContext(ctx)
	if accessToken == "" {
		log.Info("Missing FCM access token")
		return
	}

	message := map[string]interface{}{
		"message": map[string]interface{}{
			"token": deviceToken,
			"notification": map[string]string{
				"title": "Nouvelle commande reçue",
				"body":  fmt.Sprintf("Vous avez une nouvelle commande. Commande : %s", orderID),
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

	resp, err := s.client.SendFCMMessage(ctx, deviceToken, accessToken, message)
	if err != nil {
		//log.Info("sendWithoutPayload error: " + err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Info("📢 Notification envoyée avec succès à " + merchantID + " - " + nType + " - " + orderID + " - " + deviceToken)
		return
	}

	// --- GESTION DES ERREURS ---
	log.Warn("Status code " + strconv.Itoa(resp.StatusCode) + " received from FCM for merchant " + merchantID + " - " + nType + " - " + orderID + " - " + deviceToken)

	// 1. CAS DU 401 (Jeton d'accès expiré)
	if resp.StatusCode == 401 && canRetry {
		log.Warn("🚨 Access Token expiré (401). Tentative de rafraîchissement et retry...")

		// On nettoie le cache (Mémoire + DB)
		s.handleFCMError(ctx, merchantID, deviceToken, accessToken, 401)

		// On récupère un TOUT NOUVEAU token
		// (Grâce au Singleflight dans getFCMToken, si 50 goroutines font ça,
		// une seule génère le token, les 49 autres attendent le résultat).
		newToken, err := s.getFCMToken(ctx)
		if err != nil {
			log.Error("Échec du rafraîchissement du token pour le retry: " + err.Error())
			return
		}

		// RE-TENTATIVE (On passe canRetry à false pour ne pas boucler à l'infini)
		s.sendWithoutPayload(ctx, merchantID, orderID, deviceToken, nType, newToken, false)
		return
	}

	// 2. AUTRES CAS (404, 410, etc.)
	//s.handleFCMError(ctx, merchantID, deviceToken, accessToken, resp.StatusCode)
}

func (s *NotificationService) handleFCMError(ctx context.Context, merchantID, deviceToken, accessToken string, statusCode int) {
	log := logger.FromContext(ctx)

	switch statusCode {
	case 401:
		// Invalidation réactive
		s.mu.Lock()
		if s.cachedToken == accessToken {
			s.cachedToken = ""
			s.tokenExpiry = time.Time{}
		}
		s.mu.Unlock()
		_ = s.repo.DeleteAccessToken(ctx, accessToken)

	case 404, 410:
		// Nettoyage des devices morts
		log.Warn(fmt.Sprintf("🗑️ Suppression device token pour %s", merchantID))
		// _ = s.repo.DeleteDeviceToken(ctx, deviceToken)
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
	} else {
		log.Info("Successfully sent FCM notification to " + merchantID + " : " + token)
	}
}

func (s *NotificationService) getFCMToken(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)

	// 1. TENTATIVE MÉMOIRE (Trés rapide)
	s.mu.RLock()
	// On garde une marge de sécurité de 30 secondes pour éviter de renvoyer
	// un token qui expire pendant l'appel réseau vers Google.
	if s.cachedToken != "" && time.Now().Add(30*time.Second).Before(s.tokenExpiry) {
		token := s.cachedToken
		s.mu.RUnlock()
		return token, nil
	}
	s.mu.RUnlock()

	// 2. SINGLEFLIGHT (Protection contre les appels simultanés)
	res, err, _ := s.sfGroup.Do("fcm_access_token", func() (interface{}, error) {

		// Double check mémoire
		s.mu.RLock()
		if s.cachedToken != "" && time.Now().Add(30*time.Second).Before(s.tokenExpiry) {
			token := s.cachedToken
			s.mu.RUnlock()
			return token, nil
		}
		s.mu.RUnlock()

		// A. Récupération DB (avec la date d'expiration réelle)
		token, expiry, err := s.repo.GetValidFCMToken(ctx)
		if err != nil {
			return "", err
		}

		// B. Si pas de token valide en DB, on génère
		if token == "" {
			token, err = s.tokenm.GenerateToken(ctx)
			if err != nil {
				return "", err
			}

			// On stocke en base (la DB calculera NOW + 50 min)
			_ = s.repo.StoreFCMToken(ctx, token)

			// Pour le cache local, on simule le même TTL que la DB (50 min)
			expiry = time.Now().Add(50 * time.Minute)
			log.Info("New FCM Access Token generated and stored")
		}

		// C. Mise à jour du cache mémoire avec les infos précises
		s.mu.Lock()
		s.cachedToken = token
		s.tokenExpiry = expiry
		s.mu.Unlock()

		return token, nil
	})

	if err != nil {
		return "", err
	}

	return res.(string), nil
}

func (s *NotificationService) getFCMTokenOld(ctx context.Context) (string, error) {
	log := logger.FromContext(ctx)

	// TODO Sauvegarder le token en local afin de pouvoir le réutiliser sans avoir à Query la base de données ??

	// 👉 ON VERROUILLE. Si une autre goroutine arrive, elle mettra l'exécution en pause ici
	// jusqu'à ce que la première ait fini (Unlock).
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. On vérifie en base (la goroutine en attente verra le token fraîchement créé par la 1ère)
	token, err := s.repo.GetValidFCMTokenOld(ctx)
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

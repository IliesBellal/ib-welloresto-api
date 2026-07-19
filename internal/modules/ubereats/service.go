package ubereats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"time"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	ueModels "welloresto-api/internal/webhook/ubereats/models"

	"go.uber.org/zap"
)

type UberEatsService struct {
	client *UberClient
	repo   *UberRepository
	db     *sql.DB // Nécessaire pour gérer les transactions (Begin/Commit)
	config ConfigUberEats
	redis  *redis.Client // Optionnel, pour le caching
}

func NewUberEatsService(db *sql.DB, config ConfigUberEats, redisClient *redis.Client) *UberEatsService {
	return &UberEatsService{
		client: NewUberClient(config),
		repo:   NewUberEatsRepository(db),
		db:     db,
		config: config,
		redis:  redisClient,
	}
}

func (s *UberEatsService) GetValidToken(ctx context.Context) (string, error) {

	// TODO Sauvegarder le token en local afin de pouvoir le réutiliser sans avoir à Query la base de données ??

	tokenData, err := s.repo.GetCurrentToken(ctx, s.config.TokenType)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	shouldRefresh := false
	if err == sql.ErrNoRows {
		shouldRefresh = true
	} else {
		refreshThreshold := tokenData.ExpiresAt.AddDate(0, 0, -5)
		if time.Now().UTC().After(refreshThreshold) {
			shouldRefresh = true
		}
	}

	if shouldRefresh {
		newToken, err := s.client.GetNewToken()
		if err != nil {
			return "", err
		}

		if err := s.repo.SaveNewToken(
			ctx,
			s.config.TokenType,
			newToken.AccessToken,
			newToken.ExpiresIn,
		); err != nil {
			return "", err
		}

		return newToken.AccessToken, nil
	}

	return tokenData.AccessToken, nil
}

func (s *UberEatsService) GetByStoreID(ctx context.Context, storeID string) (*Store, error) {
	// ==========================================
	// 1. CACHE REDIS ETABLISSEMENT (24 Heures)
	// ==========================================
	cacheKey := fmt.Sprintf("webhook:uber:store:%s", storeID)

	if s.redis != nil {
		val, found := s.redis.Get(ctx, cacheKey)
		if found {
			var store Store
			// Sécurité : On vérifie que la désérialisation fonctionne et que l'objet n'est pas vide
			if errUnmarshal := json.Unmarshal([]byte(val), &store); errUnmarshal == nil {
				return &store, nil
			}
		}
	}

	// 2. Fallback DB : Récupérer le store et le token
	merchantID, err := s.repo.GetMerchantIDFromStoreID(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("store error: %v", err)
	}
	if merchantID == nil {
		return nil, fmt.Errorf("No store %s", storeID)
	}

	// 3. Récupérer le store
	store, err := s.repo.GetStoreData(ctx, *merchantID)
	if err != nil {
		return nil, fmt.Errorf("store error: %v", err)
	}

	// ==========================================
	// 4. SAUVEGARDE REDIS
	// ==========================================
	if s.redis != nil && store != nil {
		jsonData, _ := json.Marshal(store)
		_ = s.redis.Set(ctx, cacheKey, string(jsonData), 24*time.Hour)
	}

	return store, nil
}

// GenerateAuthURL crée le lien vers lequel le front-end doit rediriger le client
func (s *UberEatsService) GenerateAuthURL(merchantID string, redirectURI string) string {
	// Le paramètre "state" est crucial : on y glisse le merchantID pour se souvenir de QUI se connecte
	state := merchantID

	u, _ := url.Parse(s.config.AuthURL)
	q := u.Query()
	q.Set("client_id", s.config.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "eats.order eats.report eats.store")
	q.Set("state", state)
	u.RawQuery = q.Encode()

	return u.String()
}

// HandleCallback traite le retour de Uber, récupère le store_id et l'enregistre
func (s *UberEatsService) HandleCallback(ctx context.Context, code, state, redirectURI string) error {
	merchantID := state // Le state contient le merchantID qu'on a envoyé

	if code == "" || merchantID == "" {
		return fmt.Errorf("paramètres invalides ou manquants")
	}

	// 1. Échanger le code contre un Token
	tokens, err := s.client.ExchangeAuthCode(ctx, code, redirectURI)
	if err != nil {
		return fmt.Errorf("échec de l'échange du token: %v", err)
	}

	// 2. Utiliser le token pour récupérer le StoreID du restaurateur
	merchantInfos, err := s.client.GetMerchantStores(ctx, tokens.AccessToken)
	if err != nil {
		return fmt.Errorf("échec de la récupération des stores: %v", err)
	}

	if len(merchantInfos.Stores) == 0 {
		return fmt.Errorf("aucun restaurant trouvé sur ce compte Uber Eats")
	}

	// Par défaut, on prend le premier restaurant trouvé (à adapter si un compte gère plusieurs stores)
	storeID := merchantInfos.Stores[0].StoreID

	// 3. Sauvegarder dans ta base de données (Table integration_uber_eats)
	err = s.repo.EnableIntegration(ctx, merchantID, storeID, tokens.AccessToken, tokens.RefreshToken)
	if err != nil {
		return fmt.Errorf("erreur lors de la sauvegarde de l'intégration: %v", err)
	}

	return nil
}

// Disconnect supprime l'intégration
func (s *UberEatsService) Disconnect(ctx context.Context, merchantID string) error {
	return s.repo.DisableIntegration(ctx, merchantID)
}

// AcceptOrder réplique setUberEatsOrderAccepted de manière optimisée pour éviter les Deadlocks
func (s *UberEatsService) AcceptOrder(ctx context.Context, merchantID, orderID string) error {

	// ---------------------------------------------------------
	// ÉTAPE 1 : Récupérer le Token
	// ---------------------------------------------------------
	token, err := s.GetValidToken(ctx)
	if err != nil {
		return fmt.Errorf("token error: %v", err)
	}

	// ---------------------------------------------------------
	// ÉTAPE 2 : Récupérer les infos (Lecture Seule)
	// ---------------------------------------------------------
	// On déclare les variables ici pour les utiliser après la fermeture de la transaction de lecture
	var store *Store // Assure-toi que le type Store est bien accessible ici (sinon models.Store)
	var orderMeta *UberOrderMetadata
	var autoPrepTime int

	// On utilise une fonction anonyme pour isoler la transaction de lecture
	// Cela garantit que la connexion est relâchée AVANT l'appel API
	err = func() error {

		// 2.a Récupérer le store
		store, err = s.repo.GetStoreData(ctx, merchantID)
		if err != nil {
			return fmt.Errorf("store error: %v", err)
		}

		// 2.b Récupérer les métadonnées de la commande
		orderMeta, err = s.repo.GetOrderMetadata(ctx, orderID)
		if err != nil {
			return fmt.Errorf("metadata error: %v", err)
		}

		// 2.c Calculer le temps auto si nécessaire
		if store.EstimatedPreparationTime == 0 {
			// On ignore l'erreur ici car on a une valeur par défaut, ou tu peux gérer l'erreur si critique
			calcTime, errCalc := s.repo.CalculateAutoPrepTime(ctx, merchantID, orderID)
			if errCalc == nil {
				autoPrepTime = calcTime
			}
		}

		return nil
	}()

	if err != nil {
		return err
	}

	// ---------------------------------------------------------
	// ÉTAPE 3 : Logique Métier (Pur Go, pas de DB)
	// ---------------------------------------------------------
	prepTime := store.EstimatedPreparationTime

	// Si le temps du store est 0 (Auto), on prend le calculé, sinon on garde le fixe
	if prepTime == 0 && autoPrepTime > 0 {
		prepTime = autoPrepTime
	}

	// Logique de clamp PHP : max(5, min($time, 59))
	prepTime = int(math.Max(5, math.Min(float64(prepTime), 59)))

	// Calcul date et Timezone
	loc, err := time.LoadLocation(store.Timezone)
	if err != nil {
		loc = time.UTC
	}
	pickupAt := time.Now().In(loc).Add(time.Duration(prepTime) * time.Minute)
	readyForPickupTime := pickupAt.Format(time.RFC3339) // Format attendu par Uber

	// ---------------------------------------------------------
	// ÉTAPE 4 : Appel API Uber (LENT - HORS Transaction DB)
	// ---------------------------------------------------------
	req := UberAcceptRequest{
		ReadyForPickupTime: readyForPickupTime,
		ExternalID:         fmt.Sprintf("%v", orderID),    // Utilise %v pour gérer string ou int
		AcceptedBy:         fmt.Sprintf("%v", merchantID), // Utilise %v pour gérer string ou int
	}

	// C'est ici que ça bloquait avant. Maintenant, aucune connexion DB n'est active ici.
	if err := s.client.AcceptOrder(ctx, orderMeta.BrandOrderID, token, req); err != nil {
		return fmt.Errorf("uber api error: %v", err)
	}

	// ---------------------------------------------------------
	// ÉTAPE 5 : Mise à jour finale (Transaction d'écriture courte)
	// ---------------------------------------------------------

	if err := s.repo.UpdateOrderAccepted(ctx, orderID, prepTime); err != nil {
		return err
	}

	// Commit final rapide
	return nil
}

func (s *UberEatsService) GetOrderByURL(ctx context.Context, url string) (*ueModels.UberOrder, error) {

	token, err := s.GetValidToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("token error: %v", err)
	}

	order, err := s.client.GetOrderByURL(context.Background(), url, token)
	if err != nil {
		return nil, err
	}

	return order, nil
}

// DenyOrder refuse une commande de manière optimisée (sans bloquer la DB)
func (s *UberEatsService) DenyOrder(ctx context.Context, merchantID, orderID, reasonID, reasonType, comment string) error {
	log := logger.FromContext(ctx)

	// ---------------------------------------------------------
	// ÉTAPE 2 : Récupérer les métadonnées (Lecture courte)
	// ---------------------------------------------------------
	var brandOrderID string

	token, err := s.GetValidToken(ctx)
	if err != nil {
		return err
	}

	// On utilise une fonction anonyme pour la lecture seule
	err = func() error {

		meta, err := s.repo.GetOrderMetadata(ctx, orderID)
		if err != nil {
			return err
		}
		brandOrderID = meta.BrandOrderID
		return nil
	}()

	if err != nil {
		return err
	}

	// ---------------------------------------------------------
	// ÉTAPE 3 : Appel API (HORS TRANSACTION DB)
	// ---------------------------------------------------------
	// Si l'appel échoue, on ne rollback rien car rien n'a été écrit
	apiErr := s.client.DenyOrder(ctx, brandOrderID, token, reasonType, reasonID, comment)

	if apiErr != nil {
		// Logique de fallback PHP : Sync state si erreur API
		// On le fait de manière asynchrone ou synchrone selon ton besoin,
		// mais IMPORTANT : s.RecoverOrderState doit gérer ses propres transactions.
		log.Error("Uber deny failed, trying to recover state", zap.Error(apiErr))

		// Attention : RecoverOrderState ne doit pas être appelé dans une Tx existante
		s.RecoverOrderState(ctx, orderID)

		return apiErr
	}

	// ---------------------------------------------------------
	// ÉTAPE 4 : Succès API -> Mise à jour DB (Ecriture courte)
	// ---------------------------------------------------------

	if err := s.repo.SetOrderStatusDenied(ctx, orderID); err != nil {
		return err
	}

	// Notification (peut être faite après le commit ou via un bus d'event)
	// s.notifications.Send(...)

	return nil
}

// CancelOrder logique métier (très similaire à Deny)
func (s *UberEatsService) CancelOrder(ctx context.Context, merchantID, orderID, reasonID, reasonType, comment string) error {
	log := logger.FromContext(ctx)

	err := func() error {
		token, err := s.GetValidToken(ctx)
		if err != nil {
			return err
		}

		meta, err := s.repo.GetOrderMetadata(ctx, orderID)
		if err != nil {
			return err
		}

		if err := s.client.CancelOrder(ctx, meta.BrandOrderID, token, reasonType, reasonID, comment); err != nil {
			return err
		}

		if err := s.repo.SetOrderStatusCanceled(ctx, orderID); err != nil {
			return err
		}
		return nil
	}()

	if err != nil {
		log.Error(err.Error())
		s.RecoverOrderState(ctx, orderID)
		return err
	}

	return nil
}

// SetOrderReady logique métier
func (s *UberEatsService) SetOrderReady(ctx context.Context, userID, merchantID, orderID string, updateStock bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. On récupère les infos SANS transaction pour libérer la DB au plus vite
	token, err := s.GetValidToken(ctx)
	if err != nil {
		return err
	}

	meta, err := s.repo.GetOrderMetadata(ctx, orderID)
	if err != nil {
		return err
	}

	// 2. Appel API Uber (Zéro connexion DB ouverte ici !)
	if err := s.client.SetOrderReady(ctx, meta.BrandOrderID, token); err != nil {
		// Log l'erreur mais ne bloque pas la DB
		log.Printf("[Uber] Erreur API pour %s: %v", meta.BrandOrderID, err)
		s.RecoverOrderState(ctx, orderID)
		return err
	}

	if updateStock {
		// s.orderLifeCycle.RemoveStock(userID, merchantID, orderID)
	}

	return nil
}

// RecoverOrderState est un wrapper helper pour appeler FinishOrderIfDoesNotExist avec les bons params
func (s *UberEatsService) RecoverOrderState(ctx context.Context, orderID string) {

	token, _ := s.GetValidToken(ctx)
	meta, _ := s.repo.GetOrderMetadata(ctx, orderID)

	if token != "" && meta != nil {
		s.FinishOrderIfDoesNotExist(ctx, token, meta.BrandOrderID)
	}
}

// FinishOrderIfDoesNotExist (La logique de synchro)
func (s *UberEatsService) FinishOrderIfDoesNotExist(ctx context.Context, token string, uberOrderID string) error {

	// 1. GET Order details
	details, err := s.client.GetOrderDetails(uberOrderID, token)

	// 2. Gestion cas 404
	if err != nil && err.Error() == "order_not_found" {
		if err := s.repo.HandleOrderNotFound(ctx, uberOrderID); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err // Autre erreur API
	}

	// 3. Mapping des états (Switch case du PHP)
	var (
		state            = StateClosed
		brandStatus      = StatusCompleted
		merchantApproval = "ACCEPTED"
		deletionReasonID sql.NullInt64 // Nullable
	)

	switch details.Order.State {
	case "ACCEPTED":
		state = StateOpen
		brandStatus = StatusAccepted
	case "HANDED_OFF":
		state = StateClosed
		brandStatus = StatusEnRoute
	case "FAILED":
		switch details.Order.FailureInfo.Reason {
		case "ACCEPT_TIMED_OUT":
			deletionReasonID = sql.NullInt64{Int64: 41, Valid: true}
			brandStatus = StatusDenied
			merchantApproval = "DENIED"
		case "DELIVERY_FAILED":
			deletionReasonID = sql.NullInt64{Int64: 39, Valid: true}
			brandStatus = StatusDeliveryFailed
		case "CANCELED":
			deletionReasonID = sql.NullInt64{Int64: 39, Valid: true}
			brandStatus = StatusCanceled
		default: // POS_DENIED, UNKNOWN
			deletionReasonID = sql.NullInt64{Int64: 40, Valid: true}
			brandStatus = StatusDenied
			merchantApproval = "DENIED"
		}
	default: // SUCCEEDED
		brandStatus = StatusCompleted
	}

	// 4. Update DB
	if err := s.repo.SyncOrderState(ctx, uberOrderID, brandStatus, state, merchantApproval, deletionReasonID); err != nil {
		return err
	}

	return nil
}

// UberEatsBYOCStatusUpdate met à jour le statut de livraison
func (s *UberEatsService) UberEatsBYOCStatusUpdate(ctx context.Context, merchantID, orderID string, status string) error {
	token, err := s.GetValidToken(ctx)
	if err != nil {
		return err
	}

	// On a besoin de l'ID externe Uber Eats ici. En PHP tu utilisais juste order_id dans l'URL.
	// Mais l'URL PHP est : .../orders/".$order_id."/...
	// Si $order_id est l'ID interne, il faut le convertir en externe ?
	// *Hypothèse*: Ton PHP utilise $order_id directement dans l'URL, supposons que c'est l'ID Uber passé en paramètre
	// OU que tu as besoin de mapper. Dans le doute, je suis ton code PHP qui passe order_id direct.
	orderIDStr := fmt.Sprintf("%s", orderID)

	if err := s.client.UpdateBYOCStatus(ctx, orderIDStr, token, status); err != nil {
		// Le PHP retourne status -1 si 404, ici on retourne l'erreur
		return err
	}

	return nil
}

// UpdateBusyModeTime active le mode occupé (délai supplémentaire)
func (s *UberEatsService) UpdateBusyModeTime(ctx context.Context, merchantID string, delayMinutes int, untilMinutes int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	store, err := s.repo.GetStoreData(ctx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken(ctx) // Ou utiliser store.BearerToken via un refresh check

	// Calcul des dates (Timezone Aware)
	loc, err := time.LoadLocation(store.Timezone)
	if err != nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	newDate := now.Add(time.Duration(untilMinutes) * time.Minute)

	// Format RFC3339 pour l'API
	busyModeDateStr := newDate.Format(time.RFC3339)
	delaySeconds := delayMinutes * 60

	// Payload
	req := UberPrepTimeRequest{
		DelayConfig: &DelayConfig{
			DelayUntil:    busyModeDateStr,
			DelayDuration: delaySeconds,
		},
	}

	if err := s.client.UpdateStorePrepTime(ctx, store.StoreID, token, req); err != nil {
		return err
	}

	// Update DB
	if err := s.repo.UpdateBusyModeData(ctx, store.StoreID, newDate, delayMinutes); err != nil {
		return err
	}

	return nil
}

// UpdateReadyForPickupTime met à jour le temps de préparation standard
func (s *UberEatsService) UpdateReadyForPickupTime(ctx context.Context, merchantID string, timeVal int, isAuto bool) error {

	// Si AUTO, on met juste à jour la BDD (selon ton code PHP)
	if isAuto {
		if err := s.repo.UpdatePreparationTime(ctx, merchantID, timeVal, true); err != nil {
			return err
		}
		return nil
	}

	// Sinon, appel API
	store, err := s.repo.GetStoreData(ctx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken(ctx)

	prepTimeSeconds := timeVal * 60
	req := UberPrepTimeRequest{
		DefaultPrepTime: &prepTimeSeconds,
	}

	if err := s.client.UpdateStorePrepTime(ctx, store.StoreID, token, req); err != nil {
		return err
	}

	// Update DB (isAuto = false)
	if err := s.repo.UpdatePreparationTime(ctx, merchantID, timeVal, false); err != nil {
		return err
	}

	return nil
}

// CloseStoreTemporary ferme le magasin temporairement
func (s *UberEatsService) CloseStoreTemporary(ctx context.Context, merchantID string, delayMinutes int) error {

	store, err := s.repo.GetStoreData(ctx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken(ctx)

	// Time calculation
	loc, err := time.LoadLocation(store.Timezone)
	if err != nil {
		loc = time.UTC
	}

	newDate := time.Now().In(loc).Add(time.Duration(delayMinutes) * time.Minute)
	offlineUntilStr := newDate.Format(time.RFC3339)

	req := UberStoreStatusRequest{
		IsOfflineUntil: offlineUntilStr,
		Status:         "OFFLINE",
		Reason:         "Cannot take orders",
	}

	if err := s.client.UpdateStoreStatus(ctx, store.StoreID, token, req); err != nil {
		return err
	}

	if err := s.repo.UpdateStoreClosure(ctx, store.StoreID, newDate); err != nil {
		return err
	}

	return nil
}

// ToggleItemAvailability active ou désactive un produit
func (s *UberEatsService) ToggleItemAvailability(ctx context.Context, merchantID, itemID string, available bool) error {
	// Récup Store ID, BasicAuth et Timezone
	store, err := s.repo.GetStoreInfoForMenu(ctx, merchantID)
	if err != nil {
		return err
	}

	// Note: Ici j'utilise le token brut de la BDD comme ton PHP ("bearer_token is not null").
	// Idéalement on utiliserait GetValidToken(tx) pour rafraîchir si besoin.

	token, err := s.GetValidToken(ctx)

	var suspendTimestamp *int64

	// Si available == false (donc "0" en PHP), on suspend pour 30 min
	if !available {
		loc, err := time.LoadLocation(store.Timezone)
		if err != nil {
			loc = time.UTC
		}

		ts := time.Now().In(loc).Add(30 * time.Minute).Unix()
		suspendTimestamp = &ts
	}

	// Construction Payload complexe
	suspensionDetail := SuspensionDetail{
		SuspendUntil: suspendTimestamp,
		Reason:       "Sold out",
	}

	req := UberItemSuspensionRequest{
		SuspensionInfo: SuspensionInfo{
			Suspension: suspensionDetail,
			Overrides: []OverrideContext{
				{
					ContextType:  "MODIFIER_GROUP",
					ContextValue: itemID,
					Suspension:   suspensionDetail,
				},
			},
		},
	}

	if err := s.client.UpdateItemState(ctx, store.StoreID, itemID, token, req); err != nil {
		return err
	}

	return nil
}

// GetMenu wrapper simple
func (s *UberEatsService) GetMenu(ctx context.Context, merchantID string) (map[string]interface{}, error) {
	store, err := s.repo.GetStoreData(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	token, err := s.GetValidToken(ctx)
	if err != nil {
		return nil, err
	}

	return s.client.GetMenu(store.StoreID, token)
}

// SyncMenu pousse un menu interne vers l'API Uber Eats
func (s *UberEatsService) SyncMenu(ctx context.Context, merchantID string, menu interface{}) error {
	store, err := s.repo.GetStoreData(ctx, merchantID)
	if err != nil {
		return err
	}

	token, err := s.GetValidToken(ctx)
	if err != nil {
		return err
	}

	return s.client.SyncMenu(ctx, store.StoreID, token, menu)
}

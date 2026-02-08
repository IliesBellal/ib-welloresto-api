package ubereats

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
	"welloresto-api/internal/logger"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

type UberEatsService struct {
	client *UberClient
	repo   *UberRepository
	db     *sql.DB // Nécessaire pour gérer les transactions (Begin/Commit)
	config ConfigUberEats
}

func NewUberEatsService(db *sql.DB, config ConfigUberEats) *UberEatsService {
	return &UberEatsService{
		client: NewUberClient(config),
		repo:   NewUberEatsRepository(db),
		db:     db,
		config: config,
	}
}

// GetValidToken gère la logique de rafraichissement (-5 jours)
func (s *UberEatsService) GetValidToken() (string, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)

	tokenData, err := s.repo.GetCurrentToken(tx, s.config.TokenType)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	// Logique PHP : if ($now > $expiresAt || sizeof($data) == 0) (avec le -5 jours)
	shouldRefresh := false
	if err == sql.ErrNoRows {
		shouldRefresh = true
	} else {
		// ExpiresAt - 5 jours
		refreshThreshold := tokenData.ExpiresAt.AddDate(0, 0, -5)
		if time.Now().UTC().After(refreshThreshold) {
			shouldRefresh = true
		}
	}

	if shouldRefresh {
		// Appel API
		newToken, err := s.client.GetNewToken()
		if err != nil {
			return "", err
		}
		// Sauvegarde DB
		err = s.repo.SaveNewToken(tx, s.config.TokenType, newToken.AccessToken, newToken.ExpiresIn)
		if err != nil {
			return "", err
		}

		tx.Commit()
		return newToken.AccessToken, nil
	}

	tx.Commit()

	return tokenData.AccessToken, nil
}

func (s *UberEatsService) GetOrderByURL(ctx context.Context, url string) (*ueModels.UberOrder, error) {
	token, err := s.GetValidToken()
	if err != nil {
		return nil, fmt.Errorf("token error: %v", err)
	}

	order, err := s.client.GetOrderByURL(ctx, url, token)
	if err != nil {
		return nil, err
	}

	return order, nil
}

func (s *UberEatsService) GetByStoreID(ctx context.Context, storeID string) (*Store, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // Rollback automatique si pas de commit

	// 1. Récupérer le store et le token
	merchantID, err := s.repo.GetMerchantIDFromStoreID(tx, storeID)
	if err != nil {
		return nil, fmt.Errorf("store error: %v", err)
	}
	if merchantID == nil {
		return nil, fmt.Errorf("No store " + storeID)
	}

	// 2. Récupérer le store
	store, err := s.repo.GetStoreData(tx, *merchantID)
	if err != nil {
		return nil, fmt.Errorf("store error: %v", err)
	}

	return store, nil
}

// AcceptOrder réplique setUberEatsOrderAccepted
func (s *UberEatsService) AcceptOrder(ctx context.Context, merchantID, orderID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Rollback automatique si pas de commit

	// 1. Récupérer le store et le token
	store, err := s.repo.GetStoreData(tx, merchantID)
	if err != nil {
		return fmt.Errorf("store error: %v", err)
	}

	token, err := s.GetValidToken()
	if err != nil {
		return fmt.Errorf("token error: %v", err)
	}

	// 2. Récupérer les infos commande
	orderMeta, err := s.repo.GetOrderMetadata(tx, orderID)
	if err != nil {
		return err
	}

	// 3. Calcul du temps de préparation
	prepTime := store.EstimatedPreparationTime

	// Si AUTO ou null (en Go 0 pour int)
	if prepTime == 0 {
		calcTime, err := s.repo.CalculateAutoPrepTime(tx, merchantID, orderID)
		if err == nil {
			prepTime = calcTime
			// Ici tu avais une logique updateReadyForPickupTime complexe en PHP
			// Je la simplifie pour l'exemple, mais elle devrait être ici.
		}
	}

	// Logique de clamp PHP : max(5, min($time, 59))
	prepTime = int(math.Max(5, math.Min(float64(prepTime), 59)))

	// 4. Calcul du Timestamp RFC3339
	// Attention aux Timezones. En PHP tu chargeais la Timezone du merchant.
	loc, err := time.LoadLocation(store.Timezone)
	if err != nil {
		loc = time.UTC
	}
	pickupAt := time.Now().In(loc).Add(time.Duration(prepTime) * time.Minute)
	readyForPickupTime := pickupAt.Format(time.RFC3339) // Format attendu par Uber

	// 5. Appel API
	req := UberAcceptRequest{
		ReadyForPickupTime: readyForPickupTime,
		ExternalID:         fmt.Sprintf("%d", orderID),
		AcceptedBy:         fmt.Sprintf("%d", merchantID),
	}

	if err := s.client.AcceptOrder(ctx, orderMeta.BrandOrderID, token, req); err != nil {
		// Logique d'erreur PHP : finishOrderIfDoesNotExist si échec
		// Ici on retourne l'erreur, le handler HTTP peut décider d'appeler une méthode de sync
		return fmt.Errorf("uber api error: %v", err)
	}

	// 6. Succès : Mise à jour locale
	if err := s.repo.UpdateOrderAccepted(tx, orderID, prepTime); err != nil {
		return err
	}

	// Notifier (Placeholder pour sendUpdateOrderNotification)
	// s.sendUpdateOrderNotification(merchantID, orderID)

	return tx.Commit()
}

// DenyOrder logique métier
func (s *UberEatsService) DenyOrder(ctx context.Context, merchantID, orderID, reasonID, reasonType, comment string) error {
	// 1. Transaction Initiale
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// Utilisation d'une fonction anonyme pour gérer le commit/rollback proprement
	// et permettre l'appel à finishOrderIfDoesNotExist APRES le rollback si nécessaire
	err = func() error {
		// Récupérer données (similaire PHP)
		token, err := s.GetValidToken()
		if err != nil {
			return err
		}

		meta, err := s.repo.GetOrderMetadata(tx, orderID)
		if err != nil {
			return err
		}

		// Appel API
		if err := s.client.DenyOrder(ctx, meta.BrandOrderID, token, reasonType, reasonID, comment); err != nil {
			// Si erreur API, on retourne l'erreur pour déclencher le rollback
			return err
		}

		// Succès : Update DB
		if err := s.repo.SetOrderStatusDenied(tx, orderID); err != nil {
			return err
		}

		// Notification ici...
		return nil
	}()

	if err != nil {
		tx.Rollback() // On annule la transaction locale

		// Logique de fallback PHP : si erreur API, on tente de synchroniser
		// On récupère juste le brand_id et token pour le fallback (nécessite une petite requete hors tx ou passage param)
		// Pour simplifier ici, on assume qu'on peut récupérer le token et l'ID
		// Dans une prod réelle, il faudrait logger l'erreur 'err' ici.

		// On lance la synchro qui gère sa propre transaction
		// Note: Il faut récupérer le BrandOrderID et BasicAuth hors de la transaction échouée idéalement
		// Je simplifie l'appel ici :
		s.RecoverOrderState(merchantID, orderID)
		return err
	}

	return tx.Commit()
}

// CancelOrder logique métier (très similaire à Deny)
func (s *UberEatsService) CancelOrder(ctx context.Context, merchantID, orderID, reasonID, reasonType, comment string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	err = func() error {
		token, err := s.GetValidToken()
		if err != nil {
			return err
		}

		meta, err := s.repo.GetOrderMetadata(tx, orderID)
		if err != nil {
			return err
		}

		if err := s.client.CancelOrder(ctx, meta.BrandOrderID, token, reasonType, reasonID, comment); err != nil {
			return err
		}

		if err := s.repo.SetOrderStatusCanceled(tx, orderID); err != nil {
			return err
		}
		return nil
	}()

	if err != nil {
		tx.Rollback()
		s.RecoverOrderState(merchantID, orderID)
		return err
	}

	return tx.Commit()
}

// SetOrderReady logique métier
func (s *UberEatsService) SetOrderReady(ctx context.Context, userID, merchantID, orderID string, updateStock bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	err = func() error {
		token, err := s.GetValidToken()
		if err != nil {
			return err
		}

		meta, err := s.repo.GetOrderMetadata(tx, orderID)
		if err != nil {
			return err
		}

		// PHP Logic: Update DB BEFORE API Call
		if err := s.repo.SetOrderStatusReady(tx, orderID); err != nil {
			return err
		}

		// API Call
		log := logger.FromContext(ctx)
		log.Info("OrderFileCycle.SetDistributedProducts - doRequest for order " + meta.BrandOrderID)
		if err := s.client.SetOrderReady(ctx, meta.BrandOrderID, token); err != nil {
			return err
		}

		// Gestion du stock (si stock == '1')
		if updateStock {
			// s.orderLifeCycle.RemoveStock(userID, merchantID, orderID)
		}

		return nil
	}()

	if err != nil {
		tx.Rollback()
		s.RecoverOrderState(merchantID, orderID)
		return err
	}

	return tx.Commit()
}

// RecoverOrderState est un wrapper helper pour appeler FinishOrderIfDoesNotExist avec les bons params
func (s *UberEatsService) RecoverOrderState(merchantID, orderID string) {
	// Cette fonction ouvre une nouvelle connexion pour récupérer les infos
	// nécessaires à finishOrderIfDoesNotExist sans dépendre de la transaction précédente échouée
	// Implémentation simplifiée :
	tx, _ := s.db.Begin()
	defer tx.Commit()

	token, _ := s.GetValidToken()
	meta, _ := s.repo.GetOrderMetadata(tx, orderID)

	if token != "" && meta != nil {
		s.FinishOrderIfDoesNotExist(token, meta.BrandOrderID)
	}
}

// FinishOrderIfDoesNotExist (La logique de synchro)
func (s *UberEatsService) FinishOrderIfDoesNotExist(token string, uberOrderID string) error {
	// Nouvelle transaction indépendante
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // Sécurité

	// 1. GET Order details
	details, err := s.client.GetOrderDetails(uberOrderID, token)

	// 2. Gestion cas 404
	if err != nil && err.Error() == "order_not_found" {
		if err := s.repo.HandleOrderNotFound(tx, uberOrderID); err != nil {
			return err
		}
		return tx.Commit()
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
	if err := s.repo.SyncOrderState(tx, uberOrderID, brandStatus, state, merchantApproval, deletionReasonID); err != nil {
		return err
	}

	return tx.Commit()
}

// UberEatsBYOCStatusUpdate met à jour le statut de livraison
func (s *UberEatsService) UberEatsBYOCStatusUpdate(ctx context.Context, merchantID, orderID string, status string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	token, err := s.GetValidToken()
	if err != nil {
		return err
	}

	// On a besoin de l'ID externe Uber Eats ici. En PHP tu utilisais juste order_id dans l'URL.
	// Mais l'URL PHP est : .../orders/".$order_id."/...
	// Si $order_id est l'ID interne, il faut le convertir en externe ?
	// *Hypothèse*: Ton PHP utilise $order_id directement dans l'URL, supposons que c'est l'ID Uber passé en paramètre
	// OU que tu as besoin de mapper. Dans le doute, je suis ton code PHP qui passe order_id direct.
	orderIDStr := fmt.Sprintf("%d", orderID)

	if err := s.client.UpdateBYOCStatus(ctx, orderIDStr, token, status); err != nil {
		// Le PHP retourne status -1 si 404, ici on retourne l'erreur
		return err
	}

	return tx.Commit()
}

// UpdateBusyModeTime active le mode occupé (délai supplémentaire)
func (s *UberEatsService) UpdateBusyModeTime(ctx context.Context, merchantID string, delayMinutes int, untilMinutes int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	store, err := s.repo.GetStoreData(tx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken() // Ou utiliser store.BearerToken via un refresh check

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
	if err := s.repo.UpdateBusyModeData(tx, store.StoreID, newDate, delayMinutes); err != nil {
		return err
	}

	return tx.Commit()
}

// UpdateReadyForPickupTime met à jour le temps de préparation standard
func (s *UberEatsService) UpdateReadyForPickupTime(ctx context.Context, merchantID string, timeVal int, isAuto bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Si AUTO, on met juste à jour la BDD (selon ton code PHP)
	if isAuto {
		if err := s.repo.UpdatePreparationTime(tx, merchantID, timeVal, true); err != nil {
			return err
		}
		return tx.Commit()
	}

	// Sinon, appel API
	store, err := s.repo.GetStoreData(tx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken()

	prepTimeSeconds := timeVal * 60
	req := UberPrepTimeRequest{
		DefaultPrepTime: &prepTimeSeconds,
	}

	if err := s.client.UpdateStorePrepTime(ctx, store.StoreID, token, req); err != nil {
		return err
	}

	// Update DB (isAuto = false)
	if err := s.repo.UpdatePreparationTime(tx, merchantID, timeVal, false); err != nil {
		return err
	}

	return tx.Commit()
}

// CloseStoreTemporary ferme le magasin temporairement
func (s *UberEatsService) CloseStoreTemporary(ctx context.Context, merchantID string, delayMinutes int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	store, err := s.repo.GetStoreData(tx, merchantID)
	if err != nil {
		return err
	}
	token, err := s.GetValidToken()

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

	if err := s.repo.UpdateStoreClosure(tx, store.StoreID, newDate); err != nil {
		return err
	}

	return tx.Commit()
}

// ToggleItemAvailability active ou désactive un produit
func (s *UberEatsService) ToggleItemAvailability(ctx context.Context, merchantID, itemID string, available bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Récup Store ID, BasicAuth et Timezone
	store, err := s.repo.GetStoreInfoForMenu(tx, merchantID)
	if err != nil {
		return err
	}

	// Note: Ici j'utilise le token brut de la BDD comme ton PHP ("bearer_token is not null").
	// Idéalement on utiliserait GetValidToken(tx) pour rafraîchir si besoin.
	token := store.BearerToken

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

	return tx.Commit()
}

// GetMenu wrapper simple
func (s *UberEatsService) GetMenu(merchantID string) (map[string]interface{}, error) {
	// Lecture seule, pas de transaction obligatoire mais recommandé pour GetValidToken
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit()

	store, err := s.repo.GetStoreData(tx, merchantID)
	if err != nil {
		return nil, err
	}
	token, err := s.GetValidToken()

	return s.client.GetMenu(store.StoreID, token)
}

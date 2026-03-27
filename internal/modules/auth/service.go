package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

type AuthService struct {
	repo  AuthRepository
	redis *redis.Client
	email mailer.Service
	sms   sms.Service
}

func NewAuthService(r AuthRepository, redis *redis.Client, email mailer.Service, sms sms.Service) AuthService {
	return AuthService{repo: r, redis: redis, email: email, sms: sms}
}

func (s *AuthService) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	if s.redis == nil {
		return s.repo.GetUserByToken(ctx, token) // Pas de cache : on va direct à la BDD
	}
	log := logger.FromContext(ctx)
	cacheKey := models.UserCachePrefix + token

	// --- ÉTAPE 1 : Chercher dans Redis ---
	cached, found, err := s.redis.Get(ctx, cacheKey)
	if err != nil {
		// Redis est en erreur : on log mais on continue vers la BDD
		// L'API reste fonctionnelle même si Redis a un problème
		log.Warn("Warning Redis Get: " + err.Error())
	}

	if found {
		// Cache hit ! On désérialise le JSON et on retourne directement
		var user UserLoginRow
		if err := json.Unmarshal([]byte(cached), &user); err == nil {
			log.Info("🧠🙋🏻‍♂️ User found in Redis cache 🙋🏻‍♂️🧠")
			return &user, nil // ← on n'a pas touché à la BDD
		}
	}
	log.Info("🧠🚫 User not found in Redis cache 🚫🧠")

	loggedUser, err := s.repo.GetUserByToken(ctx, token)
	if err == nil && loggedUser != nil {
		// Note: Ces deux appels sont volontairement omis car le contexte n'est pas retourné
		// Les valeurs sont injectées via le middleware Auth directement dans le contexte
		// context.WithValue(ctx, models.ContextUserID, loggedUser.UserID)
		// context.WithValue(ctx, models.ContextMerchantID, loggedUser.MerchantID)
	}

	// --- ÉTAPE 3 : Stocker dans Redis pour les prochains appels ---
	serialized, err := json.Marshal(loggedUser)
	if err == nil {
		if err := s.redis.Set(ctx, cacheKey, string(serialized), models.UserCacheTTL); err != nil {
			// Erreur de cache : on log mais on retourne quand même le user
			log.Warn("Warning Redis Set: " + err.Error())
		} else {
			log.Info("🧠📌 User saved in Redis cache 📌🧠")
		}
	}

	return loggedUser, err
}

func convertApp(app string) string {
	switch strings.ToUpper(app) {
	case "0", "WR_RECEPTION":
		return "WR_RECEPTION"
	case "1", "WR_DELIVERY":
		return "WR_DELIVERY"
	case "2", "WR_WAITER":
		return "WR_WAITER"
	default:
		return "WR_RECEPTION"
	}
}

// InvalidateUserCache — à appeler quand l'utilisateur modifie ses infos
// Par exemple : changement de rôle, désactivation du compte, etc.
func (s *AuthService) InvalidateUserCache(ctx context.Context, token string) error {
	return s.redis.Delete(ctx, models.UserCachePrefix+token)
}

func (s *AuthService) isMFAVerificationRequired(ctx context.Context, user *UserLoginRow) bool {
	if s.redis == nil {
		return false
	}

	if user.MFAType == nil || *user.MFAType == "" {
		return false
	}

	if user.MFAStatus == nil || *user.MFAStatus != models.MFAStatusVerified {
		return true
	}

	if user.MFAVerifiedAt == nil || *user.MFAVerifiedAt == "" {
		return true
	}

	// 1. Correction du layout : utiliser time.RFC3339 pour gérer le "T" et le "Z"
	lastVerifiedAt, err := time.Parse(time.RFC3339, *user.MFAVerifiedAt)
	if err != nil {
		// Si le format en DB est vraiment "YYYY-MM-DD HH:MM:SS" sans le T,
		// on peut tenter un second parsing ou logger l'erreur
		logger.FromContext(ctx).Warn("Cannot parse mfa date: " + err.Error())
		return false
	}

	// 1.2. Logique de comparaison :
	// On définit la limite (ex: 5 minutes en arrière pour tes tests)
	limit := time.Now().UTC().Add(-5 * time.Minute)

	// Si la date de dernière vérification est AVANT la limite, c'est trop vieux
	if lastVerifiedAt.Before(limit) {
		return true // Redemander le MFA
	}

	// 2. Vérifier si l'IP a changé
	/*
	   if user.LastIP != currentIP {
	       return true
	   }*/

	return false
}

func (s *AuthService) Login(ctx context.Context, payload LoginRequestPayload, token string, isBackoffice bool) (map[string]interface{}, error) {
	appID := convertApp(payload.App)
	username := payload.Username + payload.Email

	user, err := s.repo.Login(ctx, username, payload.Password, token)
	if err != nil {
		// On retourne une erreur propre si le mdp est faux ("invalid_credentials" renvoyé par le repo)
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	if !user.Enabled {
		return map[string]interface{}{
			"status":  "account_disabled", // 3
			"enabled": "false",
		}, nil
	}

	// Vérification des droits Mobile
	switch appID {
	case "WR_RECEPTION":
		if !user.Rights.AccessReception {
			return map[string]interface{}{"status": "user_not_allowed", "enabled": "User can't access this app"}, nil
		}
	case "WR_DELIVERY":
		if !user.Rights.AccessDelivery || !user.AllowDeliveryAccount {
			return map[string]interface{}{"status": "user_not_allowed", "enabled": "User can't access this app"}, nil
		}
	case "WR_WAITER":
		if !user.Rights.AccessWaiter || !user.AllowWaiterAccount {
			return map[string]interface{}{"status": "user_not_allowed", "enabled": "User can't access this app"}, nil
		}
	}

	// ==============================================================
	// LOGIQUE MFA (Uniquement si Backoffice ET MFA activé)
	// ==============================================================
	if isBackoffice && s.isMFAVerificationRequired(ctx, user) {
		s.SendMFACode(ctx, user, token, false)
	}

	// 3. Valider la session en base de données
	err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending)
	if err != nil {
		logger.FromContext(ctx).Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
		return nil, errors.New("erreur interne lors de la validation")
	}

	// ==============================================================

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	// JSON EXACT (Identique à ton code original)
	return map[string]interface{}{
		"status":           "1",
		"device_cash_desk": nil,
		"enabled":          "true",

		"name":                  user.Name,
		"first_name":            user.FirstName,
		"last_name":             user.LastName,
		"userId":                user.UserID,
		"user_mail":             user.Email,
		"user_tel":              user.Tel,
		"open_cash_drawer":      user.Rights.OpenCashDrawer,
		"terms_of_use_accepted": user.TermsOfUseAccepted,
		"admin":                 user.Rights.Admin,

		"merchantId":                          user.MerchantID,
		"merchant_id":                         user.MerchantID,
		"merchantName":                        user.MerchantName,
		"business_name":                       user.MerchantName,
		"merchantTel":                         user.MerchantTel,
		"merchant_tel":                        user.MerchantTel,
		"delivery_fees":                       user.DeliveryFees,
		"delivery_fees_limit":                 user.DeliveryFeesLimit,
		"kitchen_show_only_paid":              user.KitchenShowOnlyPaid,
		"allow_waiter_account":                user.AllowWaiterAccount,
		"print_merchant_cash_report":          user.Rights.PrintMerchantCashReport,
		"merchantAd":                          user.MerchantAddress,
		"merchant_address":                    user.MerchantAddress,
		"merchant_lat":                        user.MerchantLat,
		"delivery_distance_limit":             user.DeliveryDistanceLimit,
		"kitchen_distribution_mode":           user.KitchenDistributionMode,
		"production_display_mode":             user.ProductionDisplayMode,
		"pager_number_required":               user.PagerNumberRequired,
		"cash_register_required_for_ordering": user.CashRegisterRequiredForOrdering,
		"merchant_lng":                        user.MerchantLng,
		"timezone":                            user.TimeZone,

		"SNOSettings": map[string]interface{}{
			"activated": user.SNOActivated,
		},

		"integration_uber_eats": map[string]interface{}{
			"store_id":                   user.UEStoreID.String,
			"estimated_preparation_time": user.UEPrepTime.String,
			"delay_until":                user.UEDelayUntil,
			"delay_duration":             user.UEDelayDuration.Int64,
			"closed_until":               user.UEClosedUntil,
		},

		"integration_uber_direct": map[string]interface{}{
			"customer_id": user.UDCustomerID.String,
		},

		"integration_deliveroo": map[string]interface{}{
			"location_id": user.DrooLocationID.String,
		},

		"scannorder_ready":              user.ScanNOrderReady,
		"manage_on_site":                user.ManageOnSite,
		"manage_take_away":              user.ManageTakeAway,
		"manage_delivery":               user.ManageDelivery,
		"stock_management":              user.StockManagement,
		"hr_management":                 user.HrManagement,
		"service_required_for_ordering": user.ServiceRequiredForOrdering,
		"safety_stock_active":           user.DisableSafetyStock,
		"warning_new_order_not_paid":    user.WarningNewOrderNotPaid,

		"currency":          user.Currency,
		"is_open":           user.IsOpen,
		"pin_code":          user.PinCode.String,
		"merchant_web_site": user.WebSite.String,
		"token":             user.Token,
		"profile_picture":   user.ProfilePicture.String,

		"merchants": merchants,
	}, nil
}

func (s *AuthService) LoginOld(ctx context.Context, payload LoginRequestPayload, token string) (map[string]interface{}, error) {

	appID := convertApp(payload.App)

	var username string
	var err error

	username = payload.Username + payload.Email

	user, err := s.repo.Login(ctx, username, payload.Password, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return map[string]interface{}{
			"status":  "user_not_found", // 0
			"enabled": "no user found",
		}, nil
	}

	if !user.Enabled {
		return map[string]interface{}{
			"status":  "account_disabled", //3
			"enabled": "false",
		}, nil
	}

	switch appID {
	case "WR_RECEPTION":
		if !user.Rights.AccessReception {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case "WR_DELIVERY":
		if !user.Rights.AccessDelivery || !user.AllowDeliveryAccount {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case "WR_WAITER":
		if !user.Rights.AccessWaiter || !user.AllowWaiterAccount {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	}

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	// JSON EXACT
	return map[string]interface{}{
		"status":           "1",
		"device_cash_desk": nil, // à implémenter plus tard
		"enabled":          "true",

		"name":                  user.Name,
		"first_name":            user.FirstName,
		"last_name":             user.LastName,
		"userId":                user.UserID,
		"user_mail":             user.Email,
		"user_tel":              user.Tel,
		"open_cash_drawer":      user.Rights.OpenCashDrawer,
		"terms_of_use_accepted": user.TermsOfUseAccepted,
		"admin":                 user.Rights.Admin,

		"merchantId":                          user.MerchantID,
		"merchant_id":                         user.MerchantID,
		"merchantName":                        user.MerchantName,
		"business_name":                       user.MerchantName,
		"merchantTel":                         user.MerchantTel,
		"merchant_tel":                        user.MerchantTel,
		"delivery_fees":                       user.DeliveryFees,
		"delivery_fees_limit":                 user.DeliveryFeesLimit,
		"kitchen_show_only_paid":              user.KitchenShowOnlyPaid,
		"allow_waiter_account":                user.AllowWaiterAccount,
		"print_merchant_cash_report":          user.Rights.PrintMerchantCashReport,
		"merchantAd":                          user.MerchantAddress,
		"merchant_address":                    user.MerchantAddress,
		"merchant_lat":                        user.MerchantLat,
		"delivery_distance_limit":             user.DeliveryDistanceLimit,
		"kitchen_distribution_mode":           user.KitchenDistributionMode,
		"production_display_mode":             user.ProductionDisplayMode,
		"pager_number_required":               user.PagerNumberRequired,
		"cash_register_required_for_ordering": user.CashRegisterRequiredForOrdering,
		"merchant_lng":                        user.MerchantLng,
		"timezone":                            user.TimeZone,
		//"merchantLogo":                        user.MerchantLogo.String,

		"SNOSettings": map[string]interface{}{
			"activated": user.SNOActivated,
		},

		"integration_uber_eats": map[string]interface{}{
			"store_id":                   user.UEStoreID.String,
			"estimated_preparation_time": user.UEPrepTime.String,
			"delay_until":                user.UEDelayUntil,
			"delay_duration":             user.UEDelayDuration.Int64,
			"closed_until":               user.UEClosedUntil,
		},

		"integration_uber_direct": map[string]interface{}{
			"customer_id": user.UDCustomerID.String,
		},

		"integration_deliveroo": map[string]interface{}{
			"location_id": user.DrooLocationID.String,
		},

		"scannorder_ready":              user.ScanNOrderReady,
		"manage_on_site":                user.ManageOnSite,
		"manage_take_away":              user.ManageTakeAway,
		"manage_delivery":               user.ManageDelivery,
		"stock_management":              user.StockManagement,
		"hr_management":                 user.HrManagement,
		"service_required_for_ordering": user.ServiceRequiredForOrdering,
		"safety_stock_active":           user.DisableSafetyStock,
		"warning_new_order_not_paid":    user.WarningNewOrderNotPaid,

		"currency":          user.Currency,
		"is_open":           user.IsOpen,
		"pin_code":          user.PinCode.String,
		"merchant_web_site": user.WebSite.String,
		"token":             user.Token,
		"profile_picture":   user.ProfilePicture.String,

		"merchants": merchants,
	}, nil
}

func (s *AuthService) CheckAppVersion(ctx context.Context, token, versionCodeString, app string) (map[string]interface{}, error) {

	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	versionCode, err := strconv.Atoi(versionCodeString)
	if err != nil {
		return nil, errors.New("invalid version number")
	}

	// Business logic
	result, err := s.repo.CheckAppVersion(ctx, versionCode, app, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *AuthService) SaveDeviceToken(ctx context.Context, token, deviceToken, deviceID, app string) (map[string]string, error) {

	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	err = s.repo.SaveDevice(ctx, user.UserID, user.MerchantID, app, deviceID, deviceToken)
	if err != nil {
		return map[string]string{
			"status": "-3",
			"error":  err.Error(),
		}, nil
	}

	return map[string]string{"status": "1"}, nil
}

func (s *AuthService) SendMFACode(ctx context.Context, user *UserLoginRow, token string, fallbackToSMS bool) error {
	log := logger.FromContext(ctx)

	// 1. Générer le code à 6 chiffres
	otp, err := helpers.GenerateOTP()
	if err != nil {
		log.Error("Erreur lors de la génération de l'OTP: " + err.Error())
		return errors.New("impossible de générer le code de sécurité")
	}

	// 2. Stocker le code en clair dans Redis (lié au token de la session en cours)
	cacheKey := models.MFACachePrefix + token

	// On utilise ton wrapper Redis existant (adapter la signature si besoin)
	err = s.redis.Set(ctx, cacheKey, otp, models.MFACacheTTL)
	if err != nil {
		log.Error("Erreur Redis lors de la sauvegarde de l'OTP: " + err.Error())
		return errors.New("erreur interne du serveur")
	}
	log.Info("🔑 OTP généré et stocké dans Redis pour le user " + user.UserID)

	// 3. Envoyer le code
	if fallbackToSMS {
		if user.Tel == "" {
			return errors.New("aucun numéro de téléphone associé à ce compte")
		}
		// TODO: Remplacer par l'appel à ton module SMS réel
		log.Info("Envoi du code par SMS au " + user.Tel)
		go s.sms.SendMfaOTP(user.Tel, otp)
	} else {
		if user.Email == "" {
			return errors.New("aucune adresse email associée à ce compte")
		}
		// TODO: Remplacer par l'appel à ton module Email réel
		log.Info("Envoi du code par Email à " + user.Email)
		data := mailer.MfaOTPData{
			UserName:  user.FirstName + ", " + user.LastName,
			OTP:       otp,
			UserEmail: user.Email,
		}
		go s.email.SendMfaOTP(data)
	}

	return nil // Retourne l'erreur du module d'envoi si nécessaire
}

// VerifyMFA vérifie l'OTP saisi par l'utilisateur
func (s *AuthService) VerifyMFA(ctx context.Context, token string, codeSaisi string) error {
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrInvalidToken
	}

	log := logger.FromContext(ctx)
	cacheKey := models.MFACachePrefix + token

	// 1. Récupérer le code dans Redis
	storedCode, found, err := s.redis.Get(ctx, cacheKey)
	if err != nil || !found {
		return errors.New("code expiré ou inexistant, veuillez vous reconnecter")
	}

	// 2. Comparaison en clair
	if storedCode != codeSaisi {
		return models.ErrOTPMismatch
	}

	// 3. Valider la session en base de données
	err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusVerified)
	if err != nil {
		log.Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
		return errors.New("erreur interne lors de la validation")
	}

	// 4. Nettoyage de Redis
	// On supprime l'OTP pour qu'il ne soit plus utilisable
	_ = s.redis.Delete(ctx, cacheKey)
	// IMPORTANT : On supprime le cache utilisateur pour forcer ton middleware
	// à recharger les droits (et donc le nouveau mfa_status) à la prochaine requête
	_ = s.redis.Delete(ctx, models.UserCachePrefix+token)

	log.Info("✅ MFA vérifié avec succès pour le token")
	return nil
}

// FallbackSMS génère un nouvel OTP et l'envoie par SMS
func (s *AuthService) FallbackSMS(ctx context.Context, token string) error {
	// 1. Récupérer l'utilisateur pour avoir son numéro (via la fonction existante)
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return errors.New("session invalide ou expirée")
	}

	err = s.SendMFACode(ctx, user, token, true)

	return err
}

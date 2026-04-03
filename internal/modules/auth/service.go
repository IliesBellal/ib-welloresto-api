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

	"golang.org/x/sync/singleflight"
)

type AuthService struct {
	repo    AuthRepository
	redis   *redis.Client
	email   mailer.Service
	sms     sms.Service
	sfGroup singleflight.Group
}

func NewAuthService(r AuthRepository, redis *redis.Client, email mailer.Service, sms sms.Service) AuthService {
	return AuthService{repo: r, redis: redis, email: email, sms: sms}
}

func (s *AuthService) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	if s.redis == nil {
		return s.repo.GetUserByToken(ctx, token)
	}

	log := logger.FromContext(ctx)
	cacheKey := models.UserCachePrefix + token

	// --- ÉTAPE 1 : Chercher dans Redis (rapide, on garde tel quel) ---
	cached, found := s.redis.Get(ctx, cacheKey)
	if found {
		var user UserLoginRow
		if err := json.Unmarshal([]byte(cached), &user); err == nil {
			log.Info("🧠🙋🏻‍♂️ User " + user.Name + " (" + user.UserID + ") found in Redis cache 🙋🏻‍♂️🧠")
			return &user, nil
		}
	}

	// --- ÉTAPE 2 : Singleflight pour éviter le "Cache Stampede" ---
	// On utilise le 'token' comme clé pour que seuls les appels au même user soient groupés
	val, err, shared := s.sfGroup.Do(token, func() (interface{}, error) {
		// Cette partie n'est exécutée qu'UNE SEULE FOIS pour N requêtes simultanées
		log.Info("🧠🚫 User not found in Redis cache, fetching from DB (Singleflight) 🚫🧠")

		loggedUser, err := s.repo.GetUserByToken(ctx, token)
		if err != nil {
			return nil, err
		}

		// On en profite pour remplir le cache immédiatement
		serialized, _ := json.Marshal(loggedUser)
		s.redis.Set(ctx, cacheKey, string(serialized), models.UserCacheTTL)

		return loggedUser, nil
	})

	if err != nil {
		return nil, err
	}

	if shared {
		log.Info("🤝 Request shared via Singleflight for token: " + token)
	}

	return val.(*UserLoginRow), nil
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
	deleted := s.redis.Delete(ctx, models.UserCachePrefix+token)
	if !deleted {
		logger.FromContext(ctx).Warn("Failed to delete user cache for token: " + token)
	}
	return nil
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
	limit := time.Now().UTC().Add(-1 * MFAExpiration)

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

func (s *AuthService) canSendMFAOTP(ctx context.Context, user *UserLoginRow) bool {
	if s.redis == nil {
		return false
	}

	if user.MFAOTPSentAt == nil || *user.MFAOTPSentAt == "" {
		return true
	}

	// 1. Correction du layout : utiliser time.RFC3339 pour gérer le "T" et le "Z"
	lastSentAt, err := time.Parse(time.RFC3339, *user.MFAOTPSentAt)
	if err != nil {
		// Si le format en DB est vraiment "YYYY-MM-DD HH:MM:SS" sans le T,
		// on peut tenter un second parsing ou logger l'erreur
		logger.FromContext(ctx).Warn("Cannot parse mfa date: " + err.Error())
		return false
	}

	// 1.2. Logique de comparaison :
	// On définit le cooldown
	limit := time.Now().UTC().Add(-1 * OTPResendCooldown)

	// Si la date de dernier envoie est AVANT la limite, c'est bon
	if lastSentAt.Before(limit) {
		return true // Ne pas renvoyer
	}

	return false
}

func (s *AuthService) Login(ctx context.Context, payload LoginRequestPayload, token string, isBackoffice bool) (map[string]interface{}, error) {
	//appID := convertApp(payload.App)
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
	/*
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
	*/
	// ==============================================================
	// LOGIQUE MFA (Uniquement si Backoffice ET MFA activé)
	// ==============================================================
	if s.isMFAVerificationRequired(ctx, user) {

		if isBackoffice {

			// 3. Valider la session en base de données
			err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending)
			if err != nil {
				logger.FromContext(ctx).Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
				return nil, errors.New("erreur interne lors de la validation")
			}

			if s.canSendMFAOTP(ctx, user) {
				s.SendMFACode(ctx, user, token, false)
			}
		}
	} else {
		s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusVerified)
	}

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
		"mfa_status":                          user.MFAStatus,
		"mfa_type":                            user.MFAType,

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

	if !s.canSendMFAOTP(ctx, user) {
		return models.ErrOTPWaitTime
	}

	// 1. Générer le code à 6 chiffres
	otp, err := helpers.GenerateOTP()
	if err != nil {
		log.Error("Erreur lors de la génération de l'OTP: " + err.Error())
		return errors.New("impossible de générer le code de sécurité")
	}

	// 2. Stocker le code en clair dans Redis (lié au token de la session en cours)
	cacheKey := helpers.GetMFACacheKey(token)

	// On utilise ton wrapper Redis existant (adapter la signature si besoin)
	saved := s.redis.Set(ctx, cacheKey, otp, models.OTPCacheTTL)
	if !saved {
		log.Error("Erreur Redis lors de la sauvegarde de l'OTP: " + err.Error())
		return errors.New("erreur interne du serveur")
	}
	log.Warn("🔑 OTP généré et stocké dans Redis pour le user " + user.UserID + " the code is: " + otp + " 🔑")
	log.Info(cacheKey)

	// 3. Envoyer le code
	if fallbackToSMS {
		if user.Tel == "" {
			return errors.New("aucun numéro de téléphone associé à ce compte")
		}
		// TODO: Remplacer par l'appel à ton module SMS réel
		log.Info("Envoi du code par SMS au " + user.Tel)
		go s.sms.SendOTP(user.Tel, otp)
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
		go s.email.SendOTP(data)
	}

	s.repo.MarkAsOTPSent(ctx, user.UserID)

	return nil // Retourne l'erreur du module d'envoi si nécessaire
}

// VerifyMFA vérifie l'OTP saisi par l'utilisateur
func (s *AuthService) VerifyMFA(ctx context.Context, token string, codeSaisi string) error {
	user, err := s.repo.GetUserByToken(ctx, token)
	log := logger.FromContext(ctx)
	if err != nil {
		log.Error("Erreur lors de la récupération de l'utilisateur pour MFA: " + err.Error())
		return err
	}
	if user == nil {
		log.Error("Utilisateur non trouvé pour le token lors de la vérification MFA")
		return models.ErrInvalidToken
	}

	cacheKey := helpers.GetMFACacheKey(token)
	log.Info(cacheKey)

	// 1. Récupérer le code dans Redis
	storedCode, found := s.redis.Get(ctx, cacheKey)
	if !found {
		log.Error("Codes not matching, stored: " + storedCode + " - checked: " + codeSaisi)
		return models.ErrMFAExpired
	}

	// 2. Comparaison en clair
	if storedCode != codeSaisi {
		log.Error("Codes not matching, stored: " + storedCode + " - checked: " + codeSaisi)
		return models.ErrOTPMismatch
	}

	// 3. Valider la session en base de données
	err = s.repo.MarkAsMFAVerified(ctx, user.UserID)
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

// SendVerificationCode génère un OTP pour valider un email ou un téléphone
// mode: "EMAIL" ou "SMS"
func (s *AuthService) SendVerificationCode(ctx context.Context, token, mode string) error {
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return errors.New("session invalide ou expirée")
	}

	otp, err := helpers.GenerateOTP()
	if err != nil {
		return errors.New("impossible de générer le code de vérification")
	}

	// Clé Redis temporaire (ex: verify_email:TOKEN)
	cacheKey := helpers.GetVerificationCacheKey(mode, user.Token)

	// On stocke x minutes
	saved := s.redis.Set(ctx, cacheKey, otp, models.OTPCacheTTL)
	if !saved {
		return models.ErrRedisNotAvailable
	}

	if strings.ToUpper(mode) == "EMAIL" {
		s.email.SendOTP(mailer.MfaOTPData{
			UserName:  user.FirstName + ", " + user.LastName,
			OTP:       otp,
			UserEmail: user.Email,
		})
	} else if strings.ToUpper(mode) == "SMS" || strings.ToUpper(mode) == "TEL" {
		s.sms.SendOTP(user.Tel, otp)
	}

	return nil
}

// ConfirmVerification valide le code et met à jour la DB
func (s *AuthService) ConfirmVerification(ctx context.Context, token string, mode string, codeSaisi string) error {
	if strings.ToUpper(mode) == "MFA" {
		return s.VerifyMFA(ctx, token, codeSaisi)
	}
	cacheKey := helpers.GetVerificationCacheKey(mode, token)

	storedCode, found := s.redis.Get(ctx, cacheKey)
	if !found || storedCode != codeSaisi {
		return errors.New("code invalide ou expiré")
	}

	// Suppression du code consommé
	_ = s.redis.Delete(ctx, cacheKey)

	// Mise à jour en base de données via le repository
	return s.repo.MarkAsVerified(ctx, token, mode)
}

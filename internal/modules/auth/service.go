package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/security"

	"golang.org/x/sync/singleflight"
)

// authCache is the set of Redis operations used by AuthService.
// Defined as an interface so tests can inject an in-memory stub.
// *redis.Client satisfies it.
type authCache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
	Delete(ctx context.Context, key string) bool
}

type AuthService struct {
	repo    AuthRepository
	redis   authCache
	email   mailer.Service
	sms     sms.Service
	pepper  string
	sfGroup singleflight.Group

	failedLogins *failedLoginGuard

	// resetBaseURL is the back-office URL a password-reset link points to.
	// Empty means no reset email is sent (see docs/PASSWORD_RESET.md).
	resetBaseURL string
}

func NewAuthService(r AuthRepository, rc *redis.Client, email mailer.Service, sms sms.Service, pepper string, resetBaseURL string) *AuthService {
	var cache authCache
	if rc != nil {
		cache = rc
	}
	return &AuthService{repo: r, redis: cache, email: email, sms: sms, pepper: pepper, resetBaseURL: resetBaseURL, failedLogins: newFailedLoginGuard()}
}

// lockoutState is serialised as JSON in Redis under PINLockoutPrefix+anchorToken.
type lockoutState struct {
	Count       int   `json:"count"`
	LockedUntil int64 `json:"locked_until"` // Unix seconds; 0 = not locked
}

func (s *AuthService) UpdateMFAStatus(ctx context.Context, userID string, status string) error {
	return s.repo.UpdateMFAStatus(ctx, userID, status)
}

// StartMFAChallenge est appelée par le middleware quand une requête du back-office
// est refusée faute de MFA valide : elle passe le statut en pending et envoie le
// code par email, comme le fait Login. Sans elle, un refresh de page (token déjà
// émis, donc pas de Login) ouvrait la popup sans qu'aucun code ne soit parti.
//
// Le back-office lance plusieurs requêtes en parallèle et chacune reçoit ce refus,
// et le user lu depuis le cache Redis porte un mfa_otp_sent_at périmé : le cooldown
// ne protège donc pas ici. Chaque envoi écrase le code précédent, donc on n'envoie
// que s'il n'y a pas déjà un code en cours de validité pour ce token (le bouton
// « Renvoyer » reste disponible pour en redemander un après OTPResendCooldown), et
// on regroupe les appels simultanés d'un même user via singleflight.
func (s *AuthService) StartMFAChallenge(ctx context.Context, user *UserLoginRow) {
	if s.redis == nil {
		return
	}

	// L'annulation de la requête appelante (refresh du navigateur) ne doit pas
	// interrompre l'envoi partagé par les autres requêtes de la rafale.
	ctx = context.WithoutCancel(ctx)
	log := logger.FromContext(ctx)

	s.sfGroup.Do("mfa_challenge:"+user.UserID, func() (interface{}, error) {
		if err := s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending); err != nil {
			log.Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
		}

		if _, live := s.redis.Get(ctx, helpers.GetMFACacheKey(user.Token)); live {
			return nil, nil
		}
		if !s.canSendMFAOTP(ctx, user) {
			return nil, nil
		}
		if err := s.SendMFACode(ctx, user, false); err != nil {
			log.Error("Envoi du code MFA impossible pour le user " + user.UserID + ": " + err.Error())
		}
		return nil, nil
	})
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
		// Guard: a "null" value (from a previous buggy write or an evicted PIN session)
		// unmarshals without error but yields a zero-value struct — reject it as a miss.
		if err := json.Unmarshal([]byte(cached), &user); err == nil && user.UserID != "" {
			log.Info("🧠🙋🏻‍♂️ Hit user " + user.LastName + " " + user.FirstName + " (" + user.UserID + ") for merchant " + user.MerchantID + " 🙋🏻‍♂️🧠")
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

		// Only cache when the user was found — never write "null" for unknown tokens.
		if loggedUser != nil {
			serialized, _ := json.Marshal(loggedUser)
			s.redis.Set(ctx, cacheKey, string(serialized), models.UserCacheTTL)
		}

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

// AuthenticatePIN validates a PIN against the merchant of the anchor token,
// then delegates to Login with the employee's permanent token.
// The response is identical to /auth/login by construction.
func (s *AuthService) AuthenticatePIN(ctx context.Context, anchorToken, pin string) (*LoginResponse, error) {
	anchor, err := s.GetUserByToken(ctx, anchorToken)
	if err != nil {
		return nil, err
	}
	if anchor == nil {
		return nil, models.ErrInvalidToken
	}

	if delay := s.checkLockout(ctx, anchorToken); delay > 0 {
		return nil, &PINLockoutError{DelaySeconds: int(delay.Seconds())}
	}

	pinHash := security.HashPIN(pin, s.pepper)
	employee, err := s.repo.GetUserByPIN(ctx, anchor.MerchantID, pinHash)
	if err != nil {
		return nil, err
	}
	if employee == nil {
		s.incrementLockout(ctx, anchorToken)
		return nil, models.ErrUserNotFound
	}

	s.resetLockout(ctx, anchorToken)
	// Login finds the employee by token (loggedByToken path — no password check).
	// isBackoffice=false: MFA trigger skipped; MarkLastLoginAt runs in the non-MFA else branch.
	return s.Login(ctx, LoginRequestPayload{}, employee.Token, false)
}

// SetPINSelf sets the PIN for the caller (self-service).
// The caller's merchantID and userID come from their authenticated session, never from the body.
func (s *AuthService) SetPINSelf(ctx context.Context, merchantID, callerUserID, pin string) error {
	if len(pin) != PINLength {
		return ErrPINInvalidLength
	}
	h := security.HashPIN(pin, s.pepper)
	conflict, err := s.repo.CheckPINConflict(ctx, merchantID, h, callerUserID)
	if err != nil {
		return err
	}
	if conflict {
		return ErrPINConflict
	}
	return s.repo.SetPINHash(ctx, merchantID, callerUserID, &h)
}

// ResetPIN clears the PIN of a target employee (admin operation).
// Sets pin_hash to NULL; the employee must use /auth/pin/set to create a new one.
func (s *AuthService) ResetPIN(ctx context.Context, merchantID, targetUserID string) error {
	return s.repo.SetPINHash(ctx, merchantID, targetUserID, nil)
}

// SetPasswordForGoogleAccount lets a Google-origin account (no password yet)
// define one — LOT A Semaine 2, Chantier 8. The POS/kiosk run on shared
// tablets with no Google account; an owner who signed up via Google
// otherwise has no way to unlock their own till. Screen-triggering (forced
// on first POS access) is semaine 3 — this is only the endpoint.
func (s *AuthService) SetPasswordForGoogleAccount(ctx context.Context, callerUserID, newPassword string) error {
	if err := helpers.ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := helpers.HashUserPassword(newPassword)
	if err != nil {
		return err
	}
	matched, err := s.repo.SetPasswordForGoogleAccount(ctx, callerUserID, hash)
	if err != nil {
		return err
	}
	if !matched {
		return ErrAccountNotEligibleForPasswordSet
	}
	return nil
}

// NeedsPasswordSet is LOT A Semaine 3, Chantier 14's screen-triggering half
// of the above — GET /v1/auth/password/needs-set, called by the back-office
// once per session (first POS access) to decide whether to force the
// password-set screen before letting a Google-origin owner into the till.
func (s *AuthService) NeedsPasswordSet(ctx context.Context, callerUserID string) (bool, error) {
	return s.repo.NeedsPasswordSet(ctx, callerUserID)
}

func (s *AuthService) checkLockout(ctx context.Context, anchorToken string) time.Duration {
	val, found := s.redis.Get(ctx, models.PINLockoutPrefix+anchorToken)
	if !found {
		return 0
	}
	var state lockoutState
	if err := json.Unmarshal([]byte(val), &state); err != nil || state.LockedUntil == 0 {
		return 0
	}
	remaining := time.Until(time.Unix(state.LockedUntil, 0))
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (s *AuthService) incrementLockout(ctx context.Context, anchorToken string) {
	key := models.PINLockoutPrefix + anchorToken
	var state lockoutState
	if val, found := s.redis.Get(ctx, key); found {
		json.Unmarshal([]byte(val), &state) //nolint:errcheck
	}
	state.Count++

	if state.Count >= PINMaxAttempts {
		exponent := (state.Count - PINMaxAttempts) / 5
		if exponent > 4 {
			exponent = 4 // cap at 480s
		}
		duration := PINLockoutBase * time.Duration(1<<uint(exponent))
		state.LockedUntil = time.Now().Add(duration).Unix()
	}

	data, _ := json.Marshal(state)
	s.redis.Set(ctx, key, string(data), models.PINLockoutTTL)
}

func (s *AuthService) resetLockout(ctx context.Context, anchorToken string) {
	s.redis.Delete(ctx, models.PINLockoutPrefix+anchorToken)
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

func (s *AuthService) IsMFAVerificationRequired(ctx context.Context, user *UserLoginRow) bool {
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

// LoginFrom : Login appelé par la route HTTP, avec le seuil d'échecs par
// adresse pour les connexions par mot de passe (voir failedLoginGuard).
// clientIP doit venir de helpers.ProxyClientIP (non falsifiable).
func (s *AuthService) LoginFrom(ctx context.Context, clientIP string, payload LoginRequestPayload, token string, isBackoffice bool) (*LoginResponse, error) {
	if token != "" {
		return s.Login(ctx, payload, token, isBackoffice)
	}
	reservedAt := time.Now()
	if !s.failedLogins.reserve(clientIP, reservedAt) {
		return nil, models.ErrRateLimited
	}
	resp, err := s.Login(ctx, payload, token, isBackoffice)
	if !errors.Is(err, models.ErrUserNotFound) {
		s.failedLogins.release(clientIP, reservedAt, time.Now())
	}
	return resp, err
}

func (s *AuthService) Login(ctx context.Context, payload LoginRequestPayload, token string, isBackoffice bool) (*LoginResponse, error) {
	//appID := convertApp(payload.App)
	username := payload.Username + payload.Email

	user, err := s.repo.Login(ctx, username, payload.Password, token)
	if token == "" && errors.Is(err, models.ErrUserNotFound) {
		waitAfterFailedLogin(ctx)
	}
	if errors.Is(err, models.ErrAccountDisabled) {
		// Bons identifiants, compte désactivé et sans lien actif : même
		// réponse que le cas !user.Enabled ci-dessous.
		return newLoginStatusResponse("account_disabled", "false"), nil
	}
	if err != nil {
		// On retourne une erreur propre si le mdp est faux ("invalid_credentials" renvoyé par le repo)
		// ou no_active_merchant si tous ses liens users_rights ont été désactivés.
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	if !user.Enabled {
		return newLoginStatusResponse("account_disabled", "false"), nil
	}

	// ==============================================================
	// LOGIQUE MFA (Uniquement si Backoffice ET MFA activé)
	// ==============================================================
	if s.IsMFAVerificationRequired(ctx, user) {

		if isBackoffice {

			// 3. Valider la session en base de données
			err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending)
			if err != nil {
				logger.FromContext(ctx).Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
				return nil, errors.New("erreur interne lors de la validation")
			}

			if s.canSendMFAOTP(ctx, user) {
				s.SendMFACode(ctx, user, false)
			}

			pendingStatus := models.MFAStatusPending
			user.MFAStatus = &pendingStatus
		}
	} else {
		if err := s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusVerified); err != nil {
			return nil, err
		}
		verifiedStatus := models.MFAStatusVerified
		user.MFAStatus = &verifiedStatus
		if err := s.repo.MarkLastLoginAt(ctx, user.UserID); err != nil {
			return nil, err
		}
	}

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	return buildLoginResponse(user, merchants), nil
}

func newLoginStatusResponse(status string, enabled string) *LoginResponse {
	return &LoginResponse{
		Status:         status,
		DeviceCashDesk: nil,
		Enabled:        enabled,
		Session: &LoginSessionResponse{
			Enabled: enabled == "true",
		},
	}
}

func buildLoginResponse(user *UserLoginRow, merchants []MerchantRow) *LoginResponse {
	uberEats := LoginUberEatsIntegrationResponse{
		StoreID:                  user.UEStoreID.String,
		EstimatedPreparationTime: user.UEPrepTime.String,
		DelayUntil:               user.UEDelayUntil,
		DelayDuration:            user.UEDelayDuration.Int64,
		ClosedUntil:              user.UEClosedUntil,
		CommissionRate:           user.UECommissionRate.Float64,
	}

	uberDirect := LoginUberDirectIntegrationResponse{
		CustomerID: user.UDCustomerID.String,
	}

	deliveroo := LoginDeliverooIntegrationResponse{
		LocationID:     user.DrooLocationID.String,
		CommissionRate: user.DrooCommissionRate.Float64,
	}

	access := &LoginAccessResponse{
		Admin: user.HasAdminRole(),
		Apps: LoginAccessAppsResponse{
			Reception: user.Rights.AccessReception,
		},
		// RBAC lot 6 (§0) : la plupart de ces champs viennent désormais de
		// Has() (route automatiquement vers le rôle assigné quand role_id est
		// renseigné, sinon retombe sur la colonne booléenne historique — voir
		// UserLoginRow.HasMenuAccess et consorts). PrintMerchantCashReport
		// n'a pas d'équivalent dans le catalogue et reste lu directement sur
		// la colonne historique.
		Permissions: LoginAccessPermissionsResponse{
			PrintMerchantCashReport: user.Rights.PrintMerchantCashReport,
			OpenCashDrawer:          user.CanOpenCashDrawer(),
			ManageMenu:              user.HasMenuAccess(),
			ManagePlannings:         user.HasPlanningAccess(),
			ManageUsers:             user.HasUserManagementAccess(),
			ManageSettings:          user.HasSettingsAccess(),
			ManageHACCP:             user.HasHACCPAccess(),
			ViewReports:             user.HasReportsViewAccess(),
			ViewFinancials:          user.HasFinancialsViewAccess(),
			ManageCustomers:         user.HasCustomerManagementAccess(),
		},
	}

	capabilities := &LoginCapabilitiesResponse{
		Modules: LoginCapabilityModulesResponse{
			Menu:       user.HasMenuAccess(),
			Planning:   user.HasPlanningAccess() && user.PlanningEnabled,
			Users:      user.HasUserManagementAccess(),
			Settings:   user.HasSettingsAccess(),
			HACCP:      user.HasHACCPAccess() && user.HACCPEnabled,
			Bookings:   user.BookingsEnabled,
			Kiosks:     user.KiosksEnabled,
			Delivery:   user.DeliveryEnabled,
			Reports:    user.HasReportsViewAccess(),
			Financials: user.HasFinancialsViewAccess(),
			Customers:  user.HasCustomerManagementAccess(),
			Stock:      user.StockEnabled,
			HR:         user.HrManagement,
			ScanNOrder: user.ScanNOrderEnabled,
		},
		OrderTypes: LoginCapabilityOrderTypesResponse{
			OnSite:   user.ManageOnSite,
			TakeAway: user.ManageTakeAway,
			Delivery: user.ManageDelivery,
		},
		Actions: LoginCapabilityActionsResponse{
			OpenCashDrawer: user.CanOpenCashDrawer(),
			// RBAC lot 12 : CanPrintCashReport() (Rights.Admin ||
			// Rights.PrintMerchantCashReport) est décommissionnée — ce qu'elle
			// gardait est ouvert à tous pour l'instant. Le champ JSON reste
			// émis (wello_resto_flutter et wello-back-office le parsent tous
			// les deux, même si aucun des deux ne s'en sert encore pour
			// gater une action — cf. docs/decisions.md) avec une valeur
			// constante en attendant la mise à jour de ces clients, plutôt
			// que d'être retiré du contrat de login.
			PrintMerchantCashReport: true,
			ManageMenu:              user.HasMenuAccess(),
			ManagePlannings:         user.HasPlanningAccess() && user.PlanningEnabled,
			ManageUsers:             user.HasUserManagementAccess(),
			ManageSettings:          user.HasSettingsAccess(),
			ManageHACCP:             user.HasHACCPAccess() && user.HACCPEnabled,
			ViewReports:             user.HasReportsViewAccess(),
			ViewFinancials:          user.HasFinancialsViewAccess(),
			ManageCustomers:         user.HasCustomerManagementAccess(),
		},
		Integrations: LoginCapabilityIntegrationsResponse{
			UberEats:   user.UEStoreID.Valid && user.UEStoreID.String != "",
			UberDirect: user.UDCustomerID.Valid && user.UDCustomerID.String != "",
			Deliveroo:  user.DrooLocationID.Valid && user.DrooLocationID.String != "",
			ScanNOrder: user.SNOActivated && user.ScanNOrderEnabled,
		},
	}

	// Deprecated compatibility payload for existing clients.
	// Do not use these flat fields in new code; migrate consumers to session, merchant, access, integrations and capabilities.
	legacy := &LoginLegacyFields{
		//Name:                            user.Name,
		FirstName:                       user.FirstName,
		LastName:                        user.LastName,
		UserID:                          user.UserID,
		UserMail:                        user.Email,
		UserTel:                         user.Tel,
		OpenCashDrawer:                  user.Rights.OpenCashDrawer,
		TermsOfUseAccepted:              user.TermsOfUseAccepted,
		Admin:                           user.Rights.Admin,
		MerchantID:                      user.MerchantID,
		MerchantIDLegacy:                user.MerchantID,
		MerchantName:                    user.MerchantName,
		BusinessName:                    user.MerchantName,
		MerchantTel:                     user.MerchantTel,
		MerchantTelLegacy:               user.MerchantTel,
		DeliveryFees:                    user.DeliveryFees,
		DeliveryFeesLimit:               user.DeliveryFeesLimit,
		KitchenShowOnlyPaid:             user.KitchenShowOnlyPaid,
		PrintCashReport:                 user.Rights.PrintMerchantCashReport,
		MerchantAd:                      user.MerchantAddress,
		MerchantAddress:                 user.MerchantAddress,
		MerchantLat:                     user.MerchantLat,
		DeliveryDistanceLimit:           user.DeliveryDistanceLimit,
		KitchenDistributionMode:         user.KitchenDistributionMode,
		ProductionDisplayMode:           user.ProductionDisplayMode,
		PagerNumberRequired:             user.PagerNumberRequired,
		CashRegisterRequiredForOrdering: user.CashRegisterRequiredForOrdering,
		MerchantLng:                     user.MerchantLng,
		TimeZone:                        user.TimeZone,
		MFAStatus:                       user.MFAStatus,
		MFAType:                         user.MFAType,
		IntegrationUberEats:             uberEats,
		IntegrationUberDirect:           uberDirect,
		IntegrationDeliveroo:            deliveroo,
		ManageOnSite:                    user.ManageOnSite,
		ManageTakeAway:                  user.ManageTakeAway,
		ManageDelivery:                  user.ManageDelivery,
		StockManagement:                 user.StockManagement,
		HRManagement:                    user.HrManagement,
		ServiceRequiredForOrdering:      user.ServiceRequiredForOrdering,
		SafetyStockActive:               user.DisableSafetyStock,
		WarningNewOrderNotPaid:          user.WarningNewOrderNotPaid,
		Currency:                        user.Currency,
		IsOpen:                          user.IsOpen,
		MerchantWebSite:                 user.WebSite.String,
		Token:                           user.Token,
		ProfilePicture:                  user.ProfilePicture.String,
		Merchants:                       merchants,
		SNOSettings:                     LoginSNOSettingsResponse{Activated: user.SNOActivated},
	}

	// Le recipient n'a de sens que si une vérification MFA est en attente
	// (sinon on n'a envoyé aucun code, rien à afficher), et le seul canal géré
	// pour l'instant à la connexion est l'email (mfa_type "email_sms").
	var mfaRecipient string
	if user.MFAStatus != nil && *user.MFAStatus == models.MFAStatusPending &&
		user.MFAType != nil && *user.MFAType == "email_sms" {
		mfaRecipient = helpers.MaskEmail(user.Email)
	}

	return &LoginResponse{
		Status:         "1",
		DeviceCashDesk: nil,
		Enabled:        "true",
		Session: &LoginSessionResponse{
			Enabled:      true,
			MerchantID:   user.MerchantID,
			Token:        user.Token,
			MFAStatus:    user.MFAStatus,
			MFAType:      user.MFAType,
			MFARecipient: mfaRecipient,
			Merchants:    merchants,
		},
		User: &LoginUserResponse{
			ID: user.UserID,
			//Name:               user.Name,
			FirstName:          user.FirstName,
			LastName:           user.LastName,
			Email:              user.Email,
			Tel:                user.Tel,
			TermsOfUseAccepted: user.TermsOfUseAccepted,
			ProfilePicture:     user.ProfilePicture.String,
		},
		Merchant: &LoginMerchantResponse{
			ID:           user.MerchantID,
			Name:         user.MerchantName,
			BusinessName: user.MerchantName,
			Tel:          user.MerchantTel,
			Address:      user.MerchantAddress,
			Lat:          user.MerchantLat,
			Lng:          user.MerchantLng,
			TimeZone:     user.TimeZone,
			WebSite:      user.WebSite.String,
			SIRET:        strings.TrimSpace(user.MerchantSIRET.String),
			VATNumber:    strings.TrimSpace(user.MerchantVATNumber.String),
			Currency:     user.Currency,
			IsOpen:       user.IsOpen,
			Settings: LoginMerchantSettingsResponse{
				DeliveryFees:                    user.DeliveryFees,
				DeliveryFeesLimit:               user.DeliveryFeesLimit,
				DeliveryDistanceLimit:           user.DeliveryDistanceLimit,
				ManageOnSite:                    user.ManageOnSite,
				ManageTakeAway:                  user.ManageTakeAway,
				ManageDelivery:                  user.ManageDelivery,
				KitchenShowOnlyPaid:             user.KitchenShowOnlyPaid,
				KitchenDistributionMode:         user.KitchenDistributionMode,
				ProductionDisplayMode:           user.ProductionDisplayMode,
				PagerNumberRequired:             user.PagerNumberRequired,
				POSAutoLockEnabled:              user.POSAutoLockEnabled,
				POSAutoLockDelayMinutes:         user.POSAutoLockDelayMinutes,
				ServiceRequiredForOrdering:      user.ServiceRequiredForOrdering,
				CashRegisterRequiredForOrdering: user.CashRegisterRequiredForOrdering,
				WarningNewOrderNotPaid:          user.WarningNewOrderNotPaid,
				DisableSafetyStock:              user.DisableSafetyStock,
				POSUpsellEnabled:                user.POSUpsellEnabled,
				POSCoversCountRequired:          user.POSCoversCountRequired,
				MobilePaymentEnabled:            user.MobilePaymentEnabled,
				CustomerFormRequirements:        customerFormRequirementsRawMessage(user.CustomerFormRequirements),
			},
		},
		Access:       access,
		Permissions:  user.Permissions,
		Capabilities: capabilities,
		Integrations: &LoginIntegrationsResponse{
			UberEats:   uberEats,
			UberDirect: uberDirect,
			Deliveroo:  deliveroo,
		},
		SNOSettings: &LoginSNOSettingsResponse{Activated: user.SNOActivated},
		Legacy:      legacy,
	}
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

const maxDeviceAppVersionLen = 20

func (s *AuthService) SaveDeviceToken(ctx context.Context, token string, req SaveDeviceTokenRequest) (map[string]string, error) {

	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	err = s.repo.SaveDevice(ctx, user.UserID, user.MerchantID, req.App, req.DeviceID, req.DeviceToken)
	if err != nil {
		return map[string]string{
			"status": "-3",
			"error":  err.Error(),
		}, nil
	}

	// Version de l'appli : informative, donc best-effort. Un échec (colonnes
	// absentes avant la migration 184, par exemple) est journalisé mais ne doit
	// jamais faire échouer l'enregistrement du token FCM ci-dessus.
	if appVersion := truncateRunes(strings.TrimSpace(req.AppVersion), maxDeviceAppVersionLen); appVersion != "" {
		if err := s.repo.UpdateDeviceAppVersion(ctx, req.DeviceID, appVersion, req.VersionCode); err != nil {
			logger.FromContext(ctx).Warn("Failed to save device app version: " + err.Error())
		}
	}

	return map[string]string{"status": "1"}, nil
}

// ListDeviceVersions liste les appareils du merchant du token et leur version.
func (s *AuthService) ListDeviceVersions(ctx context.Context, token string) ([]DeviceVersion, error) {
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	return s.repo.ListDeviceVersions(ctx, user.MerchantID)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (s *AuthService) SendMFACode(ctx context.Context, user *UserLoginRow, fallbackToSMS bool) error {
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
	cacheKey := helpers.GetMFACacheKey(user.Token)

	// On utilise ton wrapper Redis existant (adapter la signature si besoin)
	saved := s.redis.Set(ctx, cacheKey, otp, models.OTPCacheTTL)
	if !saved {
		log.Error("Erreur Redis lors de la sauvegarde de l'OTP pour le user " + user.UserID)
		return errors.New("erreur interne du serveur")
	}
	s.resetOTPAttempts(ctx, cacheKey)
	log.Info("OTP généré et stocké dans Redis pour le user " + user.UserID)

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

	// 1. Récupérer le code dans Redis
	storedCode, found := s.redis.Get(ctx, cacheKey)
	if !found {
		log.Warn("MFA OTP absent ou expiré pour le user " + user.UserID)
		return models.ErrMFAExpired
	}

	// 2. Comparaison en temps constant
	if subtle.ConstantTimeCompare([]byte(storedCode), []byte(codeSaisi)) != 1 {
		log.Warn("MFA OTP incorrect pour le user " + user.UserID)
		if s.otpAttemptFailed(ctx, cacheKey) {
			return models.ErrMFAExpired
		}
		return models.ErrOTPMismatch
	}

	// 3. Consommer l'OTP dès que le code est bon : il ne doit plus être rejouable,
	// même si l'écriture en base échoue juste après.
	_ = s.redis.Delete(ctx, cacheKey)
	s.resetOTPAttempts(ctx, cacheKey)

	// 4. Valider la session en base de données
	if err := s.repo.MarkAsMFAVerified(ctx, user.UserID); err != nil {
		log.Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
		return errors.New("erreur interne lors de la validation")
	}
	// Le MFA est déjà validé en base : un échec ici ne doit pas faire échouer la vérification.
	if err := s.repo.MarkLastLoginAt(ctx, user.UserID); err != nil {
		log.Error("Erreur lors de la mise à jour de la dernière connexion (MFA déjà validé): " + err.Error())
	}

	// 5. Invalider le cache utilisateur pour forcer le middleware à recharger le
	// nouveau mfa_status. Le statut MFA est porté par le user alors que le cache est
	// indexé par token : on purge donc le token courant ET ceux de tous ses
	// rattachements marchands, après l'écriture en base pour ne pas re-cacher un état périmé.
	s.redis.Delete(ctx, models.UserCachePrefix+token)
	tokens, err := s.repo.ListRightsTokensForUser(ctx, user.UserID)
	if err != nil {
		log.Warn("Impossible de lister les tokens du user " + user.UserID + " pour purger le cache: " + err.Error())
	}
	for _, other := range tokens {
		if other != token {
			s.redis.Delete(ctx, models.UserCachePrefix+other)
		}
	}

	log.Info("✅ MFA vérifié avec succès pour le token")
	return nil
}

// FallbackSMS génère un nouvel OTP et l'envoie par SMS.
// Retourne le numéro masqué auquel le code a été envoyé.
func (s *AuthService) FallbackSMS(ctx context.Context, token string) (string, error) {
	// 1. Récupérer l'utilisateur pour avoir son numéro (via la fonction existante)
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return "", errors.New("session invalide ou expirée")
	}

	if err := s.SendMFACode(ctx, user, true); err != nil {
		return "", err
	}

	return helpers.MaskPhone(user.Tel), nil
}

// SendVerificationCode génère un OTP pour valider un email ou un téléphone
// mode: "EMAIL" ou "SMS"
// Retourne le destinataire masqué auquel le code a été envoyé.
func (s *AuthService) SendVerificationCode(ctx context.Context, token, mode string) (string, error) {
	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return "", errors.New("session invalide ou expirée")
	}

	// Le MFA a son propre circuit (clé Redis lue par VerifyMFA, cooldown, colonne
	// mfa_otp_sent_at) : le bouton « Renvoyer le code » de la popup MFA passe par ici.
	if strings.ToUpper(mode) == "MFA" {
		if err := s.SendMFACode(ctx, user, false); err != nil {
			return "", err
		}
		return helpers.MaskEmail(user.Email), nil
	}

	// Délai avant renvoi, comme pour le MFA : sans lui, chaque appel envoyait un
	// SMS facturé.
	sentKey := models.VerificationSentPrefix + strings.ToUpper(mode) + ":" + user.Token
	if _, found := s.redis.Get(ctx, sentKey); found {
		return "", models.ErrOTPWaitTime
	}

	otp, err := helpers.GenerateOTP()
	if err != nil {
		return "", errors.New("impossible de générer le code de vérification")
	}

	// Clé Redis temporaire (ex: verify_email:TOKEN)
	cacheKey := helpers.GetVerificationCacheKey(mode, user.Token)

	// On stocke x minutes
	saved := s.redis.Set(ctx, cacheKey, otp, models.OTPCacheTTL)
	if !saved {
		return "", models.ErrRedisNotAvailable
	}
	s.resetOTPAttempts(ctx, cacheKey)
	s.redis.Set(ctx, sentKey, "1", OTPResendCooldown)

	var recipient string
	if strings.ToUpper(mode) == "EMAIL" {
		recipient = helpers.MaskEmail(user.Email)
		s.email.SendOTP(mailer.MfaOTPData{
			UserName:  user.FirstName + ", " + user.LastName,
			OTP:       otp,
			UserEmail: user.Email,
		})
	} else if strings.ToUpper(mode) == "SMS" || strings.ToUpper(mode) == "TEL" {
		recipient = helpers.MaskPhone(user.Tel)
		s.sms.SendOTP(user.Tel, otp)
	}

	return recipient, nil
}

// ConfirmVerification valide le code et met à jour la DB
func (s *AuthService) ConfirmVerification(ctx context.Context, token string, mode string, codeSaisi string) error {
	if strings.ToUpper(mode) == "MFA" {
		return s.VerifyMFA(ctx, token, codeSaisi)
	}
	cacheKey := helpers.GetVerificationCacheKey(mode, token)

	// Mêmes codes d'erreur que le MFA (avant : erreur 500 sans code), pour que
	// le back-office distingue « code faux » de « code à redemander ».
	storedCode, found := s.redis.Get(ctx, cacheKey)
	if !found {
		return models.ErrMFAExpired
	}
	if subtle.ConstantTimeCompare([]byte(storedCode), []byte(codeSaisi)) != 1 {
		if s.otpAttemptFailed(ctx, cacheKey) {
			return models.ErrMFAExpired
		}
		return models.ErrOTPMismatch
	}

	// Suppression du code consommé
	_ = s.redis.Delete(ctx, cacheKey)
	s.resetOTPAttempts(ctx, cacheKey)

	// Mise à jour en base de données via le repository
	return s.repo.MarkAsVerified(ctx, token, mode)
}

// waitAfterFailedLogin retient la réponse d'une connexion par mot de passe
// échouée (identifiant inconnu ou mot de passe faux) pendant failedLoginDelay :
// une attaque par essais successifs devient très lente, sans jamais bloquer le
// compte (décision Ilies : pas de verrouillage). Les essais lancés en parallèle
// ne sont pas freinés par ce délai. Une requête abandonnée par le client libère
// l'attente aussitôt.
func waitAfterFailedLogin(ctx context.Context) {
	timer := time.NewTimer(failedLoginDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// otpAttemptFailed compte un code faux pour l'OTP stocké sous cacheKey. Au
// models.OTPMaxAttempts-ième, l'OTP est supprimé et la fonction renvoie true :
// sans cette limite, un code à 6 chiffres se devine par force brute pendant
// sa durée de vie.
func (s *AuthService) otpAttemptFailed(ctx context.Context, cacheKey string) bool {
	key := models.OTPAttemptsPrefix + cacheKey
	n := 0
	if raw, found := s.redis.Get(ctx, key); found {
		n, _ = strconv.Atoi(raw)
	}
	n++
	if n >= models.OTPMaxAttempts {
		_ = s.redis.Delete(ctx, cacheKey)
		_ = s.redis.Delete(ctx, key)
		return true
	}
	s.redis.Set(ctx, key, strconv.Itoa(n), models.VerificationCacheTTL)
	return false
}

func (s *AuthService) resetOTPAttempts(ctx context.Context, cacheKey string) {
	_ = s.redis.Delete(ctx, models.OTPAttemptsPrefix+cacheKey)
}

// customerFormRequirementsRawMessage converts the raw JSON bytes scanned from
// merchant_parameters.customer_form_requirements into a *json.RawMessage, or
// nil if the column was NULL/empty.
func customerFormRequirementsRawMessage(raw []byte) *json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	msg := json.RawMessage(raw)
	return &msg
}

// ---------------------------------------------------------------------------
// Password reset ("mot de passe oublié") — see docs/PASSWORD_RESET.md
// ---------------------------------------------------------------------------

// PasswordResetIssue carries what the caller needs to deliver a reset link.
// ClearToken exists only here and in the email that is about to be sent: it is
// never persisted (only its sha256 is) and must never be logged or returned in
// an HTTP response.
type PasswordResetIssue struct {
	User       PasswordResetUser
	ClearToken string
	ExpiresAt  time.Time
}

// hashResetToken maps a clear reset token to what is stored in the database.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequestPasswordReset issues a reset token for a login (username or email).
//
// Returns (nil, nil) whenever no link should be sent: unknown account, disabled
// account, no email on file, or per-account rate limit reached. The caller MUST
// respond identically in all of those cases — any observable difference turns
// this endpoint into an account-enumeration oracle.
func (s *AuthService) RequestPasswordReset(ctx context.Context, login, clientIP string) (*PasswordResetIssue, error) {
	log := logger.FromContext(ctx)

	user, err := s.repo.GetUserForPasswordReset(ctx, login)
	if err != nil {
		return nil, err
	}
	if user == nil {
		log.Info("🔑 Password reset requested for an unknown or ineligible login — no email sent")
		return nil, nil
	}

	// Rate limit per account, enforced in SQL so it survives a Redis outage.
	since := time.Now().UTC().Add(-time.Hour)
	count, err := s.repo.CountPasswordResetsSince(ctx, user.UserID, since)
	if err != nil {
		return nil, err
	}
	if count >= PasswordResetMaxPerHour {
		log.Warn("🔑 Password reset rate limit reached for user " + user.UserID + " — no email sent")
		return nil, nil
	}

	clearToken, err := helpers.GenerateToken(PasswordResetTokenBytes)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().UTC().Add(PasswordResetTTL)
	id := helpers.GeneratePrefixedID(helpers.PasswordResetIDPrefix)

	if err := s.repo.InsertPasswordReset(ctx, id, user.UserID, hashResetToken(clearToken), expiresAt, clientIP); err != nil {
		return nil, err
	}

	return &PasswordResetIssue{User: *user, ClearToken: clearToken, ExpiresAt: expiresAt}, nil
}

// ConfirmPasswordReset consumes a reset token and applies the new password.
//
// The new password is validated BEFORE the token is consumed: a rejected
// password must not burn a single-use link.
func (s *AuthService) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	log := logger.FromContext(ctx)

	if strings.TrimSpace(token) == "" {
		return ErrInvalidResetToken
	}
	if err := helpers.ValidatePassword(newPassword); err != nil {
		return err
	}

	userID, err := s.repo.ConsumePasswordResetToken(ctx, hashResetToken(token))
	if err != nil {
		return err
	}

	hash, err := helpers.HashUserPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, userID, hash); err != nil {
		return err
	}

	// Sign the user out everywhere. Rotating users_rights.token in the database
	// is the part that actually works: purging Redis alone would be undone by
	// the next request, since GetUserByToken falls back to `WHERE ur.token = ?`.
	oldTokens, err := s.repo.RotateRightsTokensForUser(ctx, userID)
	if err != nil {
		// The password IS already changed at this point. Returning an error here
		// would tell the user it failed and push them to retry with a token that
		// is now spent. Surface it loudly in the logs instead.
		log.Error("🔑 Password reset succeeded for user " + userID + " but session rotation FAILED — existing sessions remain valid: " + err.Error())
		return nil
	}

	if s.redis != nil {
		for _, old := range oldTokens {
			s.redis.Delete(ctx, models.UserCachePrefix+old)
		}
	}

	log.Info("🔑 Password reset completed for user " + userID + " — " + strconv.Itoa(len(oldTokens)) + " session(s) invalidated")
	return nil
}

// SendPasswordResetLink is the full POST /auth/forgot-password use case:
// per-IP throttle, token issuance, then the email.
//
// It returns an error only for genuine server failures. Every "no link sent"
// outcome — unknown account, throttled IP, rate-limited account, missing
// PASSWORD_RESET_BASE_URL — returns nil, because the handler must answer the
// same thing in all cases (account-enumeration).
func (s *AuthService) SendPasswordResetLink(ctx context.Context, login, clientIP string) error {
	log := logger.FromContext(ctx)

	if s.tooManyResetRequestsFromIP(ctx, clientIP) {
		log.Warn("🔑 Password reset throttled for IP " + clientIP)
		return nil
	}

	issue, err := s.RequestPasswordReset(ctx, login, clientIP)
	if err != nil {
		return err
	}
	if issue == nil {
		return nil
	}

	if strings.TrimSpace(s.resetBaseURL) == "" {
		log.Error("🔑 PASSWORD_RESET_BASE_URL is not configured — reset token issued but NO email sent")
		return nil
	}
	if s.email == nil {
		log.Error("🔑 No mailer configured — reset token issued but NO email sent")
		return nil
	}

	resetURL := s.resetBaseURL
	separator := "?"
	if strings.Contains(resetURL, "?") {
		separator = "&"
	}
	resetURL += separator + "token=" + url.QueryEscape(issue.ClearToken)

	s.email.SendPasswordReset(mailer.PasswordResetData{
		UserEmail: issue.User.Email,
		FirstName: issue.User.FirstName,
		ResetURL:  resetURL,
		ExpiresIn: int(PasswordResetTTL.Minutes()),
	})

	// Deliberately logs the user id, never the token or the URL.
	log.Info("🔑 Password reset link sent to user " + issue.User.UserID)
	return nil
}

// tooManyResetRequestsFromIP is a best-effort per-IP counter in Redis.
//
// Read-modify-write rather than an atomic INCR (the cache wrapper exposes only
// Get/Set/Delete), so it can undercount under concurrency — acceptable because
// this is a throttle, not the security boundary: the per-account limit lives in
// SQL. A Redis outage disables it entirely and the flow keeps working.
func (s *AuthService) tooManyResetRequestsFromIP(ctx context.Context, clientIP string) bool {
	if s.redis == nil || strings.TrimSpace(clientIP) == "" {
		return false
	}

	key := models.PasswordResetIPThrottlePrefix + clientIP

	count := 0
	if raw, found := s.redis.Get(ctx, key); found {
		count, _ = strconv.Atoi(raw)
	}
	if count >= models.PasswordResetIPThrottleMax {
		return true
	}

	s.redis.Set(ctx, key, strconv.Itoa(count+1), models.PasswordResetIPThrottleTTL)
	return false
}

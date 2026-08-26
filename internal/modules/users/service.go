package users

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/notification"
	planningemployees "welloresto-api/internal/modules/planning/employees"
	"welloresto-api/internal/modules/ubereats"
)

// GeofenceRadiusMeters is the maximum distance from the delivery address at which
// a position update auto-transitions the current stop from en_route to arrived.
const GeofenceRadiusMeters = 300.0

type UsersService struct {
	userRepo            *UsersRepository
	redis               *redisclient.Client
	audit               auditpkg.AuditService
	memberEmployee      memberEmployeeFacade
	notificationService *notification.NotificationService
	uberSvc             *ubereats.UberEatsService
}

var ErrInvalidPhoneFormat = errors.New("invalid_phone_format")

// byocLocationShareThrottle is the minimum delay between two location shares sent to
// Uber Eats for the same order, to avoid hammering their API on closely-spaced GPS pings.
const byocLocationShareThrottle = 8 * time.Second

func NewUsersService(u *UsersRepository, auditService auditpkg.AuditService, redis *redisclient.Client, notificationService *notification.NotificationService, uberSvc *ubereats.UberEatsService) *UsersService {
	return &UsersService{
		userRepo:            u,
		redis:               redis,
		audit:               auditService,
		memberEmployee:      newMemberEmployeeFacade(u.database),
		notificationService: notificationService,
		uberSvc:             uberSvc,
	}
}

type memberEmployeeFacade interface {
	GetActiveEmployeeByUserID(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error)
	CreateEmployee(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error)
	LinkEmployeeUser(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error)
	UpdateEmployee(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error)
}

func (s *UsersService) GetUserLocation(ctx context.Context, token, targetUserID string) (*models.OrderUser, error) {
	// 1. Validate token
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return nil, errors.New("invalid token")
	}

	// 2. Retrieve location
	return s.userRepo.GetUserLocation(ctx, user.MerchantID, targetUserID)
}

func (s *UsersService) SetUserLocation(ctx context.Context, token string, req models.UpdateLocationRequest) error {
	// 1. Validate token
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return errors.New("invalid token")
	}

	req.UserID = user.UserID

	// Le suivi de position est une prestation du module Livraison. Sans
	// souscription (mode livraison standard), on ne persiste ni la position
	// courante ni l'historique, et ni le geofence d'arrivee ni le relais de
	// position vers Uber BYOC ne se declenchent.
	//
	// No-op silencieux plutot que 403 : c'est deja le comportement quand le
	// livreur n'a pas de session active (voir le `sessionID == ""` ci-dessous),
	// et un 403 ferait echouer en boucle les apps coursier deja deployees.
	if ctxUser, ctxErr := middleware.UserFromContext(ctx); ctxErr == nil && !ctxUser.DeliveryEnabled {
		return nil
	}

	// 3. Delivery-specific: position history + arrival geofence, only if the caller
	// has an active delivery session.
	sessionID, currentOrderID, err := s.userRepo.GetActiveDeliverySessionForUser(ctx, user.MerchantID, user.UserID)
	if err != nil {
		return err
	}
	if sessionID == "" {
		return nil
	}

	// 2. Update the caller's current position (applies to all authenticated staff)
	if err := s.userRepo.SetUserLocation(ctx, req); err != nil {
		return err
	}

	if err := s.userRepo.InsertDeliveryPosition(ctx, user.UserID, sessionID, req.Lat, req.Lng, req.Heading, req.Accuracy, req.Speed); err != nil {
		return err
	}

	if currentOrderID == "" {
		return nil
	}

	stopStatus, destLat, destLng, ok, brand, brandOrderID, err := s.userRepo.GetDeliveryStopDestination(ctx, sessionID, currentOrderID)
	if err != nil {
		return err
	}

	// Relay the driver's position to Uber Eats for BYOC (self-delivery) orders, at
	// every position update while the stop is in progress - throttled via Redis so a
	// burst of GPS pings doesn't hammer Uber's API. Best-effort: never blocks or fails
	// this request.
	if brand == models.BrandUberEats && brandOrderID != nil && (stopStatus == "en_route" || stopStatus == "arrived") {
		s.shareDriverLocationWithUber(user.MerchantID, *brandOrderID, req.Lat, req.Lng)
	}

	if !ok || stopStatus != "en_route" {
		return nil
	}

	if haversineMeters(req.Lat, req.Lng, destLat, destLng) > GeofenceRadiusMeters {
		return nil
	}

	arrived, err := s.userRepo.MarkStopArrived(ctx, sessionID, currentOrderID)
	if err != nil {
		return err
	}
	if arrived {
		// The delivery_sessions module's own MarkDeliveryStopArrived (manual path)
		// relays "arriving" to Uber Eats - this automatic geofence-triggered arrival
		// bypasses that service entirely, so it must notify Uber itself.
		// notifyUberBYOCArriving takes our *internal* order id (currentOrderID), not
		// brandOrderID - UberEatsBYOCStatusUpdate resolves the brand id itself.
		if brand == models.BrandUberEats {
			s.notifyUberBYOCArriving(user.MerchantID, currentOrderID)
		}
		if s.notificationService != nil {
			_ = s.notificationService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")
		}
	}

	return nil
}

// shareDriverLocationWithUber forwards the driver's current position to Uber Eats for a
// BYOC order, throttled to at most one call per byocLocationShareThrottle per order.
func (s *UsersService) shareDriverLocationWithUber(merchantID, brandOrderID string, lat, lng float64) {
	if s.uberSvc == nil {
		return
	}

	if s.redis != nil {
		throttleKey := "ubereats:byoc:loc:" + brandOrderID
		if _, found := s.redis.Get(context.Background(), throttleKey); found {
			return
		}
		_ = s.redis.Set(context.Background(), throttleKey, "1", byocLocationShareThrottle)
	}

	go func(mID, oID string, lt, lg float64) {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.uberSvc.ShareDriverLocation(ctxTimeout, mID, oID, lt, lg); err != nil {
			logger.FromContext(ctxTimeout).Error("uber BYOC location share failed for order " + oID + ": " + err.Error())
		}
	}(merchantID, brandOrderID, lat, lng)
}

// notifyUberBYOCArriving relays the geofence-triggered automatic arrival to Uber Eats.
// orderID is our *internal* order id - UberEatsBYOCStatusUpdate resolves the Uber brand
// order id itself (unlike ShareDriverLocation, which needs the brand id directly).
func (s *UsersService) notifyUberBYOCArriving(merchantID, orderID string) {
	if s.uberSvc == nil {
		return
	}

	go func(mID, oID string) {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.uberSvc.UberEatsBYOCStatusUpdate(ctxTimeout, mID, oID, ubereats.StatusBYOCArriving); err != nil {
			logger.FromContext(ctxTimeout).Error("uber BYOC arriving status update failed for order " + oID + ": " + err.Error())
		}
	}(merchantID, orderID)
}

// haversineMeters returns the great-circle distance between two lat/lng points, in meters.
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusMeters = 6371000.0

	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}

// HashPassword and validateNewPassword delegate to internal/helpers so the
// password policy and bcrypt cost stay identical across every entry point —
// this module, and the forgot-password flow in internal/modules/auth (which
// cannot import this package: users' own tests import auth, so the reverse
// dependency would create an import cycle at test build time).
func HashPassword(password string) (string, error) {
	return helpers.HashUserPassword(password)
}

func validateNewPassword(password string) error {
	return helpers.ValidatePassword(password)
}

func (s *UsersService) UpdatePassword(ctx context.Context, token string, oldPass string, newPass string) (string, error) {

	// 1. Load user
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", models.ErrUnauthorized
	}

	// 2. Validate new password
	if err := validateNewPassword(newPass); err != nil {
		return "", err
	}

	// 3. Hash password
	hash, err := HashPassword(newPass)
	if err != nil {
		return "", err
	}

	// 2. Compare old password
	// Nécessite que tous les passwords soient des hash
	// désactivé pour le moment
	/*
		if !CheckPasswordHash(oldPass, user.Password) {
			return fmt.Errorf("invalid_old_password")
		}
	*/

	// 4. Save
	newToken, err := s.userRepo.UpdatePassword(ctx, user.UserID, user.MerchantID, hash)
	if err != nil {
		return "", err
	}

	s.redis.Delete(ctx, models.UserCachePrefix+token)

	// UpdatePassword only rotates the caller's current merchant link. Their
	// sessions on other merchants must go too — the password is global. The
	// current link is excluded so the caller keeps the session they are using
	// (newToken). See docs/PASSWORD_RESET.md (decision D10).
	otherTokens, err := s.userRepo.RotateRightsTokensExcept(ctx, user.UserID, user.MerchantID)
	if err != nil {
		// The password is already changed — log rather than fail the request.
		logger.FromContext(ctx).Error("update_password: password changed for user " + user.UserID + " but rotating other merchant sessions FAILED — they remain valid: " + err.Error())
	}
	for _, other := range otherTokens {
		s.redis.Delete(ctx, models.UserCachePrefix+other)
	}

	return newToken, nil
}

func (s *UsersService) GetProfile(ctx context.Context) (*models.UserProfileResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	return s.userRepo.GetUserProfile(ctx, user.UserID)
}

func (s *UsersService) UpdateProfile(ctx context.Context, req *models.UpdateUserProfileRequest) (*models.UserProfileResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if req.Phone != nil {
		countryCode := ""
		if req.Country != nil && strings.TrimSpace(*req.Country) != "" {
			countryCode = *req.Country
		} else {
			countryCode, err = s.userRepo.GetMerchantCountryCode(ctx, user.MerchantID)
			if err != nil {
				return nil, err
			}
		}

		formatted, err := helpers.FormatToE164(*req.Phone, countryCode)
		if err != nil {
			return nil, ErrInvalidPhoneFormat
		}

		req.Phone = &formatted
	}

	if err := s.userRepo.UpdateUserProfile(ctx, user.UserID, req); err != nil {
		return nil, err
	}

	return s.userRepo.GetUserProfile(ctx, user.UserID)
}

func (s *UsersService) GetNotifications(ctx context.Context) (*UserNotificationsData, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	notifications := make([]UserNotification, 0, 3)

	rupturesCount, ruptureNames, err := s.userRepo.GetOutOfStockComponents(ctx, user.MerchantID, 3)
	if err != nil {
		return nil, err
	}

	if rupturesCount > 0 {
		title := fmt.Sprintf("%d ruptures de stock", rupturesCount)
		if rupturesCount == 1 {
			title = "1 rupture de stock"
		}

		description := strings.Join(ruptureNames, ", ")
		notifications = append(notifications, UserNotification{
			ID:          "stock_rupture",
			Type:        "STOCK_RUPTURE",
			Title:       title,
			Description: description,
			Severity:    "danger",
			ActionLabel: "Voir les stocks",
		})
	}

	verificationStatus, err := s.userRepo.GetUserVerificationStatus(ctx, user.UserID)
	if err != nil {
		return nil, err
	}

	if !verificationStatus.EmailVerified {
		notifications = append(notifications, UserNotification{
			ID:          "email_unverified",
			Type:        "EMAIL_UNVERIFIED",
			Title:       "Email non verifie",
			Description: "Verifiez votre adresse email pour securiser votre compte.",
			Severity:    "warning",
			ActionLabel: "Verifier",
		})
	}

	if strings.TrimSpace(verificationStatus.Phone) == "" || !verificationStatus.PhoneVerified {
		notifications = append(notifications, UserNotification{
			ID:          "phone_unverified",
			Type:        "PHONE_UNVERIFIED",
			Title:       "Numero de telephone non verifie",
			Description: "Ajoutez et verifiez votre numero pour activer les alertes SMS.",
			Severity:    "info",
			ActionLabel: "Ajouter",
		})
	}

	return &UserNotificationsData{Notifications: notifications}, nil
}

package users

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/notification"
	planningemployees "welloresto-api/internal/modules/planning/employees"
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
}

var ErrInvalidPhoneFormat = errors.New("invalid_phone_format")

func NewUsersService(u *UsersRepository, auditService auditpkg.AuditService, redis *redisclient.Client, notificationService *notification.NotificationService) *UsersService {
	return &UsersService{
		userRepo:            u,
		redis:               redis,
		audit:               auditService,
		memberEmployee:      newMemberEmployeeFacade(u.database),
		notificationService: notificationService,
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

	stopStatus, destLat, destLng, ok, err := s.userRepo.GetDeliveryStopDestination(ctx, sessionID, currentOrderID)
	if err != nil {
		return err
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
	if arrived && s.notificationService != nil {
		_ = s.notificationService.SendNotificationAsync(user.MerchantID, sessionID, "UPDATE_DELIVERY_SESSION")
	}

	return nil
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

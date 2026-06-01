package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"

	"golang.org/x/crypto/bcrypt"
)

type UsersService struct {
	userRepo *UsersRepository
	audit    auditpkg.AuditService
}

var ErrInvalidPhoneFormat = errors.New("invalid_phone_format")

func NewUsersService(u *UsersRepository, auditService auditpkg.AuditService) *UsersService {
	return &UsersService{
		userRepo: u,
		audit:    auditService,
	}
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

	// 2. Retrieve location
	return s.userRepo.SetUserLocation(ctx, req)
}

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", models.ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func validateNewPassword(password string) error {
	if len(password) < 8 {
		return models.ErrInvalidInputPasswordTooShort
	}
	return nil
}

func (s *UsersService) UpdatePassword(ctx context.Context, token string, oldPass string, newPass string) error {

	// 1. Load user
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return models.ErrUnauthorized
	}

	// 2. Validate new password
	if err := validateNewPassword(newPass); err != nil {
		return err
	}

	// 3. Hash password
	hash, err := HashPassword(newPass)
	if err != nil {
		return err
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
	return s.userRepo.UpdatePassword(ctx, user.UserID, user.MerchantID, hash)
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

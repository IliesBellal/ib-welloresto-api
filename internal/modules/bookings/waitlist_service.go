package bookings

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
)

// ---------------------------------------------------------------------------
// Cœur partagé (staff + public)
// ---------------------------------------------------------------------------

// createWaitlistEntry valide, applique la politique (waitlist_enabled +
// waitlist_max_size), résout le client par téléphone (créé si absent) et insère
// l'entrée. id/token = clé primaire crypto-aléatoire, opaque et non devinable,
// réutilisée comme token public de consultation/désinscription.
func (s *BookingsService) createWaitlistEntry(ctx context.Context, merchantID string, req CreateWaitlistRequest) (*WaitlistEntry, error) {
	name := strings.TrimSpace(req.CustomerName)
	phone := strings.TrimSpace(req.CustomerPhone)
	if name == "" || phone == "" {
		return nil, models.ErrInvalidInput
	}
	if req.PartySize <= 0 {
		return nil, models.ErrInvalidInput
	}

	settings, err := s.repo.GetBookingSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if !settings.WaitlistEnabled {
		return nil, models.ErrWaitlistDisabled
	}

	if settings.WaitlistMaxSize > 0 {
		active, err := s.repo.CountActiveWaitlist(ctx, merchantID)
		if err != nil {
			return nil, err
		}
		if active >= settings.WaitlistMaxSize {
			return nil, models.ErrWaitlistFull
		}
	}

	customerID, err := s.repo.FindOrCreateCustomerByPhone(ctx, merchantID, name, phone)
	if err != nil {
		return nil, err
	}

	id, err := helpers.GenerateToken(24)
	if err != nil {
		return nil, err
	}

	entry := &WaitlistEntry{
		ID:            id,
		MerchantID:    merchantID,
		CustomerID:    customerID,
		PartySize:     req.PartySize,
		CustomerName:  name,
		CustomerPhone: phone,
		Notes:         req.Notes,
		Status:        bookingcore.WaitlistWaiting,
	}
	if err := s.repo.InsertWaitlistEntry(ctx, entry); err != nil {
		return nil, err
	}

	return s.repo.GetWaitlistEntry(ctx, merchantID, id)
}

// ---------------------------------------------------------------------------
// Staff (auth bearer, merchant depuis le contexte)
// ---------------------------------------------------------------------------

func (s *BookingsService) ListWaitlist(ctx context.Context, token string) ([]WaitlistEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListWaitlist(ctx, user.MerchantID)
}

// CreateWaitlistManual inscrit un walk-in au comptoir (staff).
func (s *BookingsService) CreateWaitlistManual(ctx context.Context, token string, req CreateWaitlistRequest) (*WaitlistEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.createWaitlistEntry(ctx, user.MerchantID, req)
}

func (s *BookingsService) SeatWaitlistEntry(ctx context.Context, token, id string) (*WaitlistEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.transitionWaitlist(ctx, user.MerchantID, id, bookingcore.WaitlistSeated)
}

func (s *BookingsService) CancelWaitlistEntry(ctx context.Context, token, id string) (*WaitlistEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.transitionWaitlist(ctx, user.MerchantID, id, bookingcore.WaitlistCancelled)
}

func (s *BookingsService) DeleteWaitlistEntry(ctx context.Context, token, id string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	return s.repo.DeleteWaitlistEntry(ctx, user.MerchantID, id)
}

// transitionWaitlist n'autorise seat/cancel que depuis un état actif
// (waiting|notified) puis renvoie l'entrée à jour.
func (s *BookingsService) transitionWaitlist(ctx context.Context, merchantID, id, target string) (*WaitlistEntry, error) {
	entry, err := s.repo.GetWaitlistEntry(ctx, merchantID, id)
	if err != nil {
		return nil, err
	}
	if entry.Status != bookingcore.WaitlistWaiting && entry.Status != bookingcore.WaitlistNotified {
		return nil, models.ErrInvalidInput
	}
	if err := s.repo.SetWaitlistStatus(ctx, merchantID, id, target); err != nil {
		return nil, err
	}
	return s.repo.GetWaitlistEntry(ctx, merchantID, id)
}

// ---------------------------------------------------------------------------
// Public (sans auth — merchant résolu par l'appelant, token = id)
// ---------------------------------------------------------------------------

// CreateWaitlistEntryPublic est appelé par le flux public /rsv ; le client est
// résolu (créé si absent) par téléphone dans createWaitlistEntry.
func (s *BookingsService) CreateWaitlistEntryPublic(ctx context.Context, merchantID string, req CreateWaitlistRequest) (*WaitlistEntry, error) {
	return s.createWaitlistEntry(ctx, merchantID, req)
}

// GetWaitlistEntryPublic consulte une entrée via son token (= id).
func (s *BookingsService) GetWaitlistEntryPublic(ctx context.Context, merchantID, token string) (*WaitlistEntry, error) {
	return s.repo.GetWaitlistEntry(ctx, merchantID, token)
}

// CancelWaitlistEntryPublic permet au client de se désinscrire via son token.
func (s *BookingsService) CancelWaitlistEntryPublic(ctx context.Context, merchantID, token string) (*WaitlistEntry, error) {
	return s.transitionWaitlist(ctx, merchantID, token, bookingcore.WaitlistCancelled)
}

package bookings

import (
	"context"
	"strings"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
)

// NoShowBooking marque une réservation no_show (depuis le POS) puis, si la
// liste d'attente est active, notifie le premier client en attente.
func (s *BookingsService) NoShowBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusNoShow); err != nil {
		return nil, models.ErrInvalidInput
	}

	if err := s.repo.SetBookingState(ctx, user.MerchantID, bookingID, bookingcore.StatusNoShow); err != nil {
		return nil, err
	}

	if s.events != nil {
		if err := s.events.Log(ctx, bookingevents.Event{
			MerchantID: user.MerchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeNoShow,
			Source:     bookingevents.SourcePOS,
			Actor:      user.UserID,
		}); err != nil && s.log != nil {
			s.log.Warn("booking no_show event log failed", zap.Error(err))
		}
	}

	// Notification temps réel POS (WebSocket + FCM).
	s.notifyPOS(user.MerchantID, bookingID, notification.NotificationTypeBookingNoShow)

	// Réattribution automatique du créneau libéré.
	if settings, err := s.repo.GetBookingSettings(ctx, user.MerchantID); err == nil {
		s.reattributeFirstWaiting(ctx, user.MerchantID, settings)
	}

	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

// ExpireWaitlistNotifications expire les entrées notified dont le délai est
// dépassé (statut expired) puis notifie le suivant pour chaque marchand
// concerné. Appelée par le cron (préparé, non activé).
func (s *BookingsService) ExpireWaitlistNotifications(ctx context.Context) (int64, error) {
	expired, err := s.repo.ListExpiredNotifiedWaitlist(ctx)
	if err != nil {
		return 0, err
	}

	var count int64
	affectedMerchants := make(map[string]bool)
	for _, e := range expired {
		if err := s.repo.SetWaitlistStatus(ctx, e.MerchantID, e.ID, bookingcore.WaitlistExpired); err != nil {
			if s.log != nil {
				s.log.Warn("waitlist expiration failed", zap.String("waitlist_id", e.ID), zap.Error(err))
			}
			continue
		}
		count++
		if s.events != nil {
			_ = s.events.Log(ctx, bookingevents.Event{
				MerchantID: e.MerchantID,
				WaitlistID: e.ID,
				EventType:  bookingevents.TypeWaitlistExpired,
				Source:     bookingevents.SourceSystem,
				Actor:      "SYSTEM",
			})
		}
		affectedMerchants[e.MerchantID] = true
	}

	for merchantID := range affectedMerchants {
		settings, err := s.repo.GetBookingSettings(ctx, merchantID)
		if err != nil {
			continue
		}
		s.reattributeFirstWaiting(ctx, merchantID, settings)
	}

	return count, nil
}

// reattributeFirstWaiting notifie la plus ancienne entrée waiting si la liste
// d'attente est active. No-op silencieux sinon.
func (s *BookingsService) reattributeFirstWaiting(ctx context.Context, merchantID string, settings *BookingSettings) {
	if settings == nil || !settings.WaitlistEnabled {
		return
	}
	entry, err := s.repo.GetFirstWaitingEntry(ctx, merchantID)
	if err != nil || entry == nil {
		return
	}
	if err := s.notifyWaitlistEntry(ctx, merchantID, entry, settings); err != nil && s.log != nil {
		s.log.Warn("waitlist reattribution notify failed", zap.String("waitlist_id", entry.ID), zap.Error(err))
	}
}

// notifyWaitlistEntry passe l'entrée en notified puis envoie la notification
// "table disponible" via le service de communication centralisé (email
// systématique si l'adresse est connue, SMS si sms_enabled), puis journalise.
func (s *BookingsService) notifyWaitlistEntry(ctx context.Context, merchantID string, entry *WaitlistEntry, settings *BookingSettings) error {
	expiry := settings.WaitlistSlotExpiryMinutes
	if expiry <= 0 {
		expiry = 15
	}

	if err := s.repo.MarkWaitlistNotified(ctx, merchantID, entry.ID, expiry); err != nil {
		return err
	}

	merchantName, _ := s.repo.GetMerchantBusinessName(ctx, merchantID)
	if strings.TrimSpace(merchantName) == "" {
		merchantName = "le restaurant"
	}

	var email string
	if entry.CustomerID != nil {
		email, _ = s.repo.GetCustomerEmail(ctx, merchantID, *entry.CustomerID)
	}

	if s.comm != nil {
		s.comm.SendWaitlistAvailable(ctx, bookingcomm.WaitlistMessage{
			MerchantName:  merchantName,
			CustomerName:  entry.CustomerName,
			CustomerEmail: email,
			CustomerPhone: entry.CustomerPhone,
			PartySize:     entry.PartySize,
			ExpiryMinutes: expiry,
			SMSEnabled:    settings.SMSEnabled,
		})
	}

	if s.events != nil {
		_ = s.events.Log(ctx, bookingevents.Event{
			MerchantID: merchantID,
			WaitlistID: entry.ID,
			EventType:  bookingevents.TypeWaitlistNotified,
			Source:     bookingevents.SourceSystem,
			Actor:      "SYSTEM",
			Metadata:   map[string]interface{}{"expiry_minutes": expiry, "sms_enabled": settings.SMSEnabled},
		})
	}

	return nil
}

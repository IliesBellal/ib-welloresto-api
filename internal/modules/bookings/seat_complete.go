package bookings

import (
	"context"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
)

// SeatBooking marque une resa confirmed comme installee (transition
// manuelle depuis le POS — le pont automatique depuis l'ouverture de
// commande vit dans order_life_cycle, cf. hook auto-seat).
func (s *BookingsService) SeatBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusSeated); err != nil {
		return nil, models.ErrInvalidInput
	}

	if err := s.repo.SetBookingState(ctx, user.MerchantID, bookingID, bookingcore.StatusSeated); err != nil {
		return nil, err
	}

	if s.events != nil {
		if err := s.events.Log(ctx, bookingevents.Event{
			MerchantID: user.MerchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeBookingSeated,
			Source:     bookingevents.SourcePOS,
			Actor:      user.UserID,
		}); err != nil && s.log != nil {
			s.log.Warn("booking seat event log failed", zap.Error(err))
		}
	}

	s.notifyPOS(user.MerchantID, bookingID, notification.NotificationTypeUpdateBooking)

	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

// CompleteBooking marque une resa seated comme terminee (transition
// manuelle depuis le POS — le pont automatique depuis la cloture de
// commande vit dans order_life_cycle, cf. hook auto-complete).
func (s *BookingsService) CompleteBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusCompleted); err != nil {
		return nil, models.ErrInvalidInput
	}

	if err := s.repo.SetBookingState(ctx, user.MerchantID, bookingID, bookingcore.StatusCompleted); err != nil {
		return nil, err
	}

	if s.events != nil {
		if err := s.events.Log(ctx, bookingevents.Event{
			MerchantID: user.MerchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeBookingCompleted,
			Source:     bookingevents.SourcePOS,
			Actor:      user.UserID,
		}); err != nil && s.log != nil {
			s.log.Warn("booking complete event log failed", zap.Error(err))
		}
	}

	s.notifyPOS(user.MerchantID, bookingID, notification.NotificationTypeUpdateBooking)

	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

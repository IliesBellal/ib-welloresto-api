package brevo_sms_reply

import (
	"context"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/modules/bookingevents"

	"go.uber.org/zap"
)

type Service struct {
	repo   *Repository
	events *bookingevents.Repository
	log    *zap.Logger
}

func NewService(repo *Repository, events *bookingevents.Repository, log *zap.Logger) *Service {
	return &Service{repo: repo, events: events, log: log}
}

// ProcessReply retrouve la réservation active liée au numéro et applique
// l'intention (reconfirmation / annulation). Best-effort : toute erreur est
// journalisée, l'appelant répond 200 quoi qu'il arrive.
func (s *Service) ProcessReply(ctx context.Context, phone, text string) {
	if strings.TrimSpace(phone) == "" {
		return
	}

	intent := ParseIntent(text)
	if intent == IntentIgnore {
		if s.log != nil {
			s.log.Info("brevo sms reply ignored", zap.String("body", text))
		}
		return
	}

	normalized := helpers.NormalizePhoneNumber(phone, "FR")
	booking, err := s.repo.FindActiveBookingByPhone(ctx, normalized)
	if err != nil {
		if s.log != nil {
			s.log.Error("brevo sms reply: lookup failed", zap.Error(err))
		}
		return
	}
	if booking == nil {
		if s.log != nil {
			s.log.Info("brevo sms reply: no active booking for phone")
		}
		return
	}

	switch intent {
	case IntentReconfirm:
		if err := s.repo.Reconfirm(ctx, booking.MerchantID, booking.BookingID); err != nil {
			if s.log != nil {
				s.log.Error("brevo sms reply: reconfirm failed", zap.Error(err))
			}
			return
		}
		s.logEvent(ctx, booking, bookingevents.TypeSMSReconfirmed)

	case IntentCancel:
		if err := s.repo.CancelByCustomer(ctx, booking.MerchantID, booking.BookingID); err != nil {
			if s.log != nil {
				s.log.Error("brevo sms reply: cancel failed", zap.Error(err))
			}
			return
		}
		s.logEvent(ctx, booking, bookingevents.TypeSMSCancelled)
	}
}

func (s *Service) logEvent(ctx context.Context, booking *ActiveBooking, eventType string) {
	if s.events == nil {
		return
	}
	_ = s.events.Log(ctx, bookingevents.Event{
		MerchantID: booking.MerchantID,
		BookingID:  booking.BookingID,
		EventType:  eventType,
		Source:     bookingevents.SourceSMS,
		Actor:      "CUSTOMER",
	})
}

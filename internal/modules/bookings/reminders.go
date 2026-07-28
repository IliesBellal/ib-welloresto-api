package bookings

import (
	"context"
	"strings"
	"time"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"

	"go.uber.org/zap"
)

// defaultReminderWindowHours est le fallback hardcodé (pas de champ settings
// dédié pour l'instant, cf. Phase 5 Bloc 4) : on rappelle les réservations
// confirmed dont le créneau tombe dans les 24 prochaines heures.
const defaultReminderWindowHours = 24

// SendBookingReminders envoie le rappel avant service aux réservations
// confirmed dans la fenêtre à venir qui n'ont pas encore été rappelées, pose
// reminder_sent_at pour éviter les doublons, et journalise chaque envoi.
// Appelée par le cron (préparé, activation sélective en Bloc 4).
func (s *BookingsService) SendBookingReminders(ctx context.Context) (int64, error) {
	toRemind, err := s.repo.ListBookingsForReminder(ctx, defaultReminderWindowHours)
	if err != nil {
		return 0, err
	}

	var sent int64
	for _, b := range toRemind {
		loc, err := time.LoadLocation(b.Timezone)
		if err != nil || loc == nil {
			loc = time.UTC
		}
		start, err := time.Parse("2006-01-02 15:04:05", b.StartDate)
		if err != nil {
			continue
		}
		start = start.In(loc)

		if s.comm != nil && (strings.TrimSpace(b.CustomerEmail) != "" || strings.TrimSpace(b.CustomerPhone) != "") {
			s.comm.SendReminder(ctx, bookingcomm.BookingMessage{
				BookingID:     b.BookingID,
				MerchantSlug:  b.MerchantSlug,
				MerchantName:  b.MerchantName,
				CustomerName:  b.CustomerName,
				CustomerEmail: b.CustomerEmail,
				CustomerPhone: b.CustomerPhone,
				BookingNumber: b.BookingNumber,
				DateLabel:     bookingcore.FormatDateLabelFR(start),
				TimeLabel:     start.Format("15:04"),
				PartySize:     b.PartySize,
				SMSEnabled:    b.SMSEnabled,
			})
		}

		if err := s.repo.MarkReminderSent(ctx, b.BookingID); err != nil {
			if s.log != nil {
				s.log.Warn("booking reminder: mark sent failed", zap.String("booking_id", b.BookingID), zap.Error(err))
			}
			continue
		}
		sent++

		if s.events != nil {
			_ = s.events.Log(ctx, bookingevents.Event{
				MerchantID: b.MerchantID,
				BookingID:  b.BookingID,
				EventType:  bookingevents.TypeBookingReminder,
				Source:     bookingevents.SourceSystem,
				Actor:      "SYSTEM",
			})
		}
	}

	return sent, nil
}

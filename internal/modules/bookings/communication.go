package bookings

import (
	"context"
	"strings"
	"time"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
)

// buildBookingMessage assemble un bookingcomm.BookingMessage à partir d'un
// Booking complet (Merchant + Customer déjà résolus par GetBookingByID) et
// des settings du marchand (slug public, sms_enabled).
func (s *BookingsService) buildBookingMessage(booking *Booking, settings *BookingSettings) bookingcomm.BookingMessage {
	loc, err := time.LoadLocation(booking.Merchant.Timezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	start := time.Unix(booking.BookingDateFrom, 0).In(loc)

	var name, email, phone string
	if booking.Customer.CustomerName != nil {
		name = *booking.Customer.CustomerName
	}
	if booking.Customer.CustomerEmail != nil {
		email = *booking.Customer.CustomerEmail
	}
	if booking.Customer.CustomerTel != nil {
		phone = *booking.Customer.CustomerTel
	}

	slug := ""
	smsEnabled := false
	if settings != nil {
		slug = settings.Code
		smsEnabled = settings.SMSEnabled
	}

	return bookingcomm.BookingMessage{
		MerchantSlug:  slug,
		MerchantName:  booking.Merchant.BusinessName,
		CustomerName:  name,
		CustomerEmail: email,
		CustomerPhone: phone,
		BookingNumber: booking.BookingNumber,
		DateLabel:     bookingcore.FormatDateLabelFR(start),
		TimeLabel:     start.Format("15:04"),
		PartySize:     booking.PartySize,
		SMSEnabled:    smsEnabled,
	}
}

// notifyBookingMessage envoie un message de communication (confirmation,
// annulation...) pour une réservation staff, en résolvant les settings du
// marchand pour le slug public et sms_enabled. No-op si comm n'est pas câblé,
// si la réservation est nil, ou si elle n'a ni email ni téléphone.
func (s *BookingsService) notifyBookingMessage(ctx context.Context, merchantID string, booking *Booking, send func(context.Context, bookingcomm.BookingMessage)) {
	if s.comm == nil || booking == nil || send == nil {
		return
	}
	settings, err := s.repo.GetBookingSettings(ctx, merchantID)
	if err != nil {
		settings = nil
	}
	msg := s.buildBookingMessage(booking, settings)
	if strings.TrimSpace(msg.CustomerEmail) == "" && strings.TrimSpace(msg.CustomerPhone) == "" {
		return
	}
	send(ctx, msg)
}

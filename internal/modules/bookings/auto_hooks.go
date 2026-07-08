package bookings

import (
	"context"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"
	"welloresto-api/internal/modules/notification"
)

// AutoSeatForOrder est le pont automatique confirmed -> seated declenche par
// l'ouverture d'une commande sur une ou plusieurs tables (cf.
// order_life_cycle.OrdersLifeCycleService.CreateOrder). Fire-and-forget par
// construction : ne doit jamais faire echouer la creation de la commande —
// l'appelant journalise l'erreur eventuelle sans la propager. No-op
// silencieux si aucune resa confirmed ne correspond (commande sans lien
// reservation, cas normal).
func (s *BookingsService) AutoSeatForOrder(ctx context.Context, merchantID, orderID string, locationIDs []string) error {
	if len(locationIDs) == 0 {
		return nil
	}

	bookingID, err := s.repo.FindConfirmedBookingForAutoSeat(ctx, merchantID, locationIDs)
	if err != nil {
		return err
	}
	if bookingID == "" {
		return nil
	}

	if err := s.repo.SetBookingSeatedWithOrder(ctx, merchantID, bookingID, orderID); err != nil {
		return err
	}

	if s.events != nil {
		_ = s.events.Log(ctx, bookingevents.Event{
			MerchantID: merchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeBookingSeated,
			Source:     bookingevents.SourceSystem,
			Actor:      "SYSTEM",
			Metadata:   map[string]interface{}{"trigger": "order_created", "order_id": orderID},
		})
	}

	s.notifyPOS(merchantID, bookingID, notification.NotificationTypeUpdateBooking)

	return nil
}

// AutoCompleteForOrder est le pont automatique seated -> completed declenche
// par la cloture/livraison d'une commande (cf.
// order_life_cycle.OrdersLifeCycleService.DeliverOrder). Meme contrat
// fire-and-forget qu'AutoSeatForOrder. No-op silencieux si aucune resa
// seated n'est liee a cette commande (commande sans lien reservation, ou
// resa jamais passee par l'etape seated).
func (s *BookingsService) AutoCompleteForOrder(ctx context.Context, merchantID, orderID string) error {
	bookingID, err := s.repo.FindSeatedBookingByOrderID(ctx, merchantID, orderID)
	if err != nil {
		return err
	}
	if bookingID == "" {
		return nil
	}

	if err := s.repo.SetBookingState(ctx, merchantID, bookingID, bookingcore.StatusCompleted); err != nil {
		return err
	}

	if s.events != nil {
		_ = s.events.Log(ctx, bookingevents.Event{
			MerchantID: merchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeBookingCompleted,
			Source:     bookingevents.SourceSystem,
			Actor:      "SYSTEM",
			Metadata:   map[string]interface{}{"trigger": "order_delivered", "order_id": orderID},
		})
	}

	s.notifyPOS(merchantID, bookingID, notification.NotificationTypeUpdateBooking)

	return nil
}

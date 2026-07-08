// notification/notification_models.go

package notification

type NotificationType string

type NotificationPayload map[string]interface{}

type NotificationMessage struct {
	MerchantID int
	OrderID    int
	Type       string
	EntityID   int
	Payload    NotificationPayload
}

const (
	NotificationTypeOrderUpdate = "UPDATE_ORDER"

	// Événements réservation/liste d'attente poussés vers le POS (WS + FCM).
	NotificationTypeNewBooking    = "NEW_BOOKING"
	NotificationTypeUpdateBooking = "UPDATE_BOOKING"
	NotificationTypeNewWaitlist   = "NEW_WAITLIST"
	NotificationTypeBookingNoShow = "BOOKING_NO_SHOW"

	// WSEventKioskStatusChanged : envoyé par le POS ou le back-office vers le
	// hub merchant pour activer/désactiver une borne en temps réel.
	WSEventKioskStatusChanged = "kiosk_status_changed"
	// WSEventKioskUnavailable : envoyé par la borne elle-même quand elle
	// détecte un problème (perte réseau récupérée, erreur critique).
	WSEventKioskUnavailable = "kiosk_unavailable"
)

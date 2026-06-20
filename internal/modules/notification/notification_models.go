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

	// WSEventKioskStatusChanged : envoyé par le POS ou le back-office vers le
	// hub merchant pour activer/désactiver une borne en temps réel.
	WSEventKioskStatusChanged = "kiosk_status_changed"
	// WSEventKioskUnavailable : envoyé par la borne elle-même quand elle
	// détecte un problème (perte réseau récupérée, erreur critique).
	WSEventKioskUnavailable = "kiosk_unavailable"
)

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

	// ─── Événements de synchronisation temps réel (WS uniquement, pas de FCM) ──
	//
	// Ces trois événements sont des *notifications sans état* : ils signalent
	// qu'une donnée a changé, le client va rechercher l'état exact via son
	// endpoint habituel. Aucun état métier ne transite par le WebSocket (voir
	// docs/audits/2026-08-24-websocket-menu-haccp-status.md, décision D2).
	//
	// Le nommage snake_case les distingue de la famille SCREAMING_SNAKE
	// ci-dessus, qui a elle un pendant FCM (NotificationType*).

	// WSEventPOSStatusChanged : le point de vente vient d'être ouvert ou fermé
	// manuellement, pour que tous les devices du merchant se resynchronisent.
	//
	// Le payload transporte le flag brut merchant_parameters.is_open, PAS le
	// statut composé de GET /pos/status (qui croise aussi horaires, jours
	// fériés et congés). Les bascules pilotées par le calendrier ne passent
	// donc pas par cet événement — un client qui affiche le statut composé
	// continue de le rafraîchir par ses propres moyens (décision D3).
	WSEventPOSStatusChanged = "pos_status_changed"
	// WSEventMenuUpdated : le catalogue produits du merchant a changé.
	// Consommé par la borne (wello-kiosk) et le POS. Émis en incrément B.
	WSEventMenuUpdated = "menu_updated"
	// WSEventHACCPUpdated : une saisie HACCP (température, nettoyage,
	// traçabilité, réception) vient d'être enregistrée. Émis en incrément C.
	WSEventHACCPUpdated = "haccp_updated"
)

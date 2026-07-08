package bookingcore

// Statuts de la liste d'attente salle (table booking_waitlist, migration 059).
const (
	WaitlistWaiting   = "waiting"
	WaitlistNotified  = "notified"
	WaitlistSeated    = "seated"
	WaitlistExpired   = "expired"
	WaitlistCancelled = "cancelled"
)

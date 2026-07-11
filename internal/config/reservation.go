package config

// ReservationConfig regroupe les paramètres du module réservation qui ne
// dépendent pas d'un fournisseur externe (contrairement à Brevo, Stripe...).
type ReservationConfig struct {
	// PublicBaseURL sert à construire le lien de gestion envoyé au client
	// (modification/annulation) : {PublicBaseURL}/{slug}/booking/{booking_number}.
	PublicBaseURL string
}

func loadReservationConfig() ReservationConfig {
	return ReservationConfig{
		PublicBaseURL: getEnv("PUBLIC_RESERVATION_BASE_URL", "https://rsv-staging.onrender.com/"),
	}
}

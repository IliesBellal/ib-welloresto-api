package config

// ReservationConfig regroupe les paramètres du module réservation qui ne
// dépendent pas d'un fournisseur externe (contrairement à Brevo, Stripe...).
type ReservationConfig struct {
	// PublicBaseURL sert à construire le lien de gestion envoyé au client
	// (modification/annulation) : {PublicBaseURL}/{slug}/booking/{booking_number}.
	PublicBaseURL string
	// PublicAPIBaseURL : adresse publique de cette API, base des liens de
	// paiement en ligne des comptes clients ({PublicAPIBaseURL}/pay/account/…).
	PublicAPIBaseURL string
}

func loadReservationConfig() ReservationConfig {
	return ReservationConfig{
		PublicBaseURL:    getEnv("PUBLIC_RESERVATION_BASE_URL", "https://rsv-staging.onrender.com/"),
		PublicAPIBaseURL: getEnv("PUBLIC_API_BASE_URL", "https://welloresto-api-staging.onrender.com"),
	}
}

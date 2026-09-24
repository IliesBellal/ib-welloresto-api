package mailer

const (
	BrandLogoURL  = "https://scannorder.welloresto.fr/src/tracking-preview.png"
	SupportEmail  = "support@welloresto.fr"
	InvoiceEmail  = "invoice@welloresto.fr"
	SecurityEmail = "security@welloresto.fr"
)

// RefundData pour charge.refunded
type RefundData struct {
	MerchantName  string // ex: "Burger King"
	MerchantLogo  string // URL du logo
	MerchantColor string // ex: "#E2F2F9" (Optionnel, pour le header)
	Amount        string // ex: "15.50 €"
	Date          string // ex: "12/02/2024"
	CustomerName  string // Nom du client
	RefundReason  string // ex: "requested_by_customer" (traduire en FR)
	PaymentMethod string // ex: "Visa" ou "Link"
	PaymentDetail string // ex: "•••• 4242" ou "FR"
	CardBrand     string // ex: "Visa"
	CardLast4     string // ex: "4242"
	ReceiptURL    string // URL du reçu Stripe
	SupportEmail  string // Email support
}

// PayoutData pour payout.paid
type PayoutData struct {
	MerchantName string
	MerchantLogo string
	Destination  string
	Status       string
	Amount       string // ex: "1450.00 €"
	PayoutDate   string // Date du virement
	ArrivalDate  string // Date estimée sur le compte (arrival_date)
	BankName     string // ex: "BNP Paribas" ou "Stripe Balance"
	AccountLast4 string // ex: "6789"
	PayoutID     string // ex: "po_1Mn..."
	DashboardURL string // Lien vers le dashboard Stripe/Wello
}

// Used when sending an order confirmation
type ScanNOrderConfirmationData struct {
	MerchantName     string // $merchant->business_name
	MerchantLogo     string // $merchant->logo_url
	MerchantCurrency string // $merchant->currency
	OrderTotal       string // Formaté: "15.50" (déjà divisé par 100)
	OrderDate        string // Formaté: "12/02/2024"
	TrackingURL      string // L'URL complète de suivi
	PrivacyURL       string // Lien politique de confidentialité
	TermsURL         string // Lien conditions générales
	SupportEmail     string // "Wello Resto SAS..."
}

type MfaOTPData struct {
	UserName  string
	UserEmail string
	OTP       string
}

type EmailBaseData struct {
	BrandName    string
	BrandLogoURL string
	SupportEmail string
	Year         int
}

// Pour la confirmation de compte
type ConfirmationEmailData struct {
	EmailBaseData
	FirstName       string
	ConfirmationURL string
}

// Pour le code MFA
type MFAMailData struct {
	EmailBaseData
	MFACode   string
	ExpiresIn int // en minutes
}

// PasswordResetData est ce que l'appelant fournit pour un email
// « mot de passe oublié ». ResetURL contient le token en clair : ne jamais
// la journaliser (cf. docs/PASSWORD_RESET.md).
type PasswordResetData struct {
	UserEmail string
	FirstName string
	ResetURL  string
	ExpiresIn int // en minutes
}

// PasswordResetMailData est le modèle passé au template password_reset.html.
type PasswordResetMailData struct {
	EmailBaseData
	FirstName string
	ResetURL  string
	ExpiresIn int // en minutes
}

// InvoiceEmailData pour l'envoi de facture PDF en pièce jointe
type InvoiceEmailData struct {
	MerchantName  string
	CustomerName  string
	ReceiptNumber string
	SupportEmail  string
}

// WaitlistAvailableData pour la notification "une table s'est libérée"
// envoyée au premier de la liste d'attente lors d'un no-show / expiration.
type WaitlistAvailableData struct {
	EmailBaseData
	MerchantName  string
	CustomerName  string
	PartySize     int
	ExpiryMinutes int
}

// BookingMessageData est la donnée partagée par les emails du cycle de vie
// d'une réservation (confirmation, rappel, modification, annulation,
// reconfirmation). ManagementLink est vide si non pertinent pour le type de
// message (ex: annulation) ou si aucune base URL publique n'est configurée.
type BookingMessageData struct {
	EmailBaseData
	MerchantName   string
	CustomerName   string
	BookingNumber  string
	DateLabel      string // date formatée locale, ex "vendredi 12 juillet 2026"
	TimeLabel      string // heure formatée locale, ex "20:00"
	PartySize      int
	ManagementLink string
}

// BookingPostVisitData pour le message post-visite (remerciement + demande
// d'avis — Should du cadrage §6.4, préparé mais non branché dans le cron).
type BookingPostVisitData struct {
	EmailBaseData
	MerchantName string
	CustomerName string
}

// DemoRequestData est le récapitulatif envoyé en interne (jamais au
// visiteur) à chaque soumission de DemoForm.astro sur le site vitrine —
// remplace Web3Forms (stockage hors UE, écarté sur avis juridique). Depuis le
// chantier créneaux engageants (2026-09-24), Slot est un horaire réellement
// réservé (voir demorequest.Slot), plus une simple préférence texte.
type DemoRequestData struct {
	EmailBaseData
	Establishment        string
	EstablishmentAddress string // vide si l'autocomplétion Google Places n'a pas abouti
	RestaurantType       string
	Phone                string
	Situation            string
	Slot                 string // ex: "vendredi 12 juillet 2026 à 9h00"
}

// DemoConfirmationData est envoyé au VISITEUR (contrairement à
// DemoRequestData, interne) une fois son créneau de démo confirmé — avec un
// fichier .ics en pièce jointe (voir demorequest.buildICS) et un lien Google
// Agenda en complément (aucune intégration Google Calendar API nécessaire,
// juste une URL "quick add" — voir docs/decisions-log.md du site vitrine
// pour pourquoi l'intégration complète a été écartée).
type DemoConfirmationData struct {
	EmailBaseData
	Establishment      string
	DateLabel          string // ex: "vendredi 12 juillet 2026"
	TimeLabel          string // ex: "9h00"
	Phone              string // numéro qui sera appelé, rappelé pour confirmation
	GoogleCalendarLink string
}

package mailer

const (
	BrandLogoURL  = "https://scannorder.welloresto.fr/src/tracking-preview.png"
	SupportEmail  = "iliesbellal@gmail.com"
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

// InvoiceEmailData pour l'envoi de facture PDF en pièce jointe
type InvoiceEmailData struct {
	MerchantName  string
	CustomerName  string
	ReceiptNumber string
	SupportEmail  string
}

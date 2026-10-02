package merchantsms

// MarketingSettings regroupe les réglages SMS d'un établissement
// (merchant_marketing_settings) utilisés pour le SMS de suivi de livraison.
type MarketingSettings struct {
	MerchantID       string
	SMSEnabled       bool
	SMSSenderName    string
	TrackingTemplate string
	QRCode           string
	SMSUnitPrice     float64
}

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

// OrderTrackingRef regroupe ce que le SMS de suivi montre au client d'une
// commande : jamais l'order_id interne (docs/SCANNORDER_PUBLIC_ORDER_ID.md).
type OrderTrackingRef struct {
	PublicID string // orders.public_id, dans le lien de suivi
	OrderNum string // numéro de retrait, pour le marqueur {order_id} du modèle
}

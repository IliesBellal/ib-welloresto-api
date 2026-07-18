package messaggio

type MarketingSettings struct {
	MerchantID       string
	SMSEnabled       bool
	MessaggioLogin   string
	MessaggioFrom    string
	TrackingTemplate string
	QRCode           string
	SMSUnitPrice     float64
}

type SMSMessage struct {
	Phone   string
	Content string
}

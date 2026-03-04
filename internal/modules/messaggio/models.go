package messaggio

type MarketingSettings struct {
	MerchantID       int64
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

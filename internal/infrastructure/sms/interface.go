package sms

import "net/http"

// Service defines the interface for sending SMS messages.
// This allows you to mock the SMS service in your unit tests.
type Service interface {
	// SendSMSAsync sends an SMS message asynchronously with a sender ID.
	// senderID is the identifier of the sender (e.g., "Wello", merchant name, etc.)
	// phoneNumber should be in international format (e.g., "+33612345678")
	SendSMSAsync(senderID, phoneNumber, message string)

	// SendOrderConfirmationSMS sends an order confirmation SMS
	SendOrderConfirmationSMS(senderID, phoneNumber string, data OrderConfirmationSMSData)

	// TriggerTestSMS sends a test SMS (for testing purposes)
	TriggerTestSMS(writer http.ResponseWriter, request *http.Request)
}

// OrderConfirmationSMSData holds data for order confirmation SMS
type OrderConfirmationSMSData struct {
	MerchantName string
	OrderID      string
	OrderTotal   string
	TrackingURL  string
}

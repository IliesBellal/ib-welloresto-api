package brevo_sms

import (
	"welloresto-api/internal/infrastructure/sms"
)

// Example usage of BrevoSMS service (for reference)
// This file is for documentation purposes only and should not be imported

func ExampleNewBrevoSMS() {
	// Create configuration from environment variables
	cfg := Config{
		APIKey: "your-brevo-api-key",
	}

	// Instantiate the BrevoSMS service
	smsService := NewBrevoSMS(cfg)

	// Send an order confirmation SMS
	data := sms.OrderConfirmationSMSData{
		MerchantName: "Burger King",
		OrderID:      "ORD-12345",
		OrderTotal:   "15.50 €",
		TrackingURL:  "https://example.com/track/123",
	}

	// The senderID is always provided by the caller
	// This sends the SMS asynchronously (non-blocking)
	smsService.SendOrderConfirmationSMS("Wello", "+33612345678", data)
}

// ExampleSendCustomSMS demonstrates custom SMS sending
func ExampleSendCustomSMS() {
	cfg := Config{
		APIKey: "your-brevo-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	// Send a custom SMS message
	// Remember: senderID must be provided by the caller
	smsService.SendSMSAsync(
		"YourBrand",    // senderID
		"+33612345678", // phoneNumber in international format
		"Hello! Your order #12345 has been confirmed.", // message
	)
}

// ExampleMultipleSenders demonstrates sending SMS from different senders
func ExampleMultipleSenders() {
	cfg := Config{
		APIKey: "your-brevo-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	// Send from different merchants/senders
	smsService.SendSMSAsync("MerchantA", "+33612345678", "Your order from Merchant A is ready!")
	smsService.SendSMSAsync("MerchantB", "+33687654321", "Your order from Merchant B is ready!")
	smsService.SendSMSAsync("Wello", "+33698765432", "General notification from Wello")
}

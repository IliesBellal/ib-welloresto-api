package brevo_mailer

import (
	"welloresto-api/internal/infrastructure/mailer"
)

// Example usage of BrevoMailer service (for reference)
// This file is for documentation purposes only and should not be imported

func ExampleNewBrevoMailer() {
	// Create configuration from environment variables
	cfg := Config{
		APIKey: "your-brevo-api-key",
	}

	// Instantiate the BrevoMailer service
	mailService := NewBrevoMailer(cfg)

	// Send an order confirmation email
	data := mailer.ScanNOrderConfirmationData{
		MerchantName:     "Burger King",
		MerchantLogo:     "https://example.com/logo.png",
		MerchantCurrency: "EUR",
		OrderTotal:       "15.50 €",
		OrderDate:        "12/02/2024",
		TrackingURL:      "https://example.com/track/123",
		PrivacyURL:       "https://example.com/privacy",
		TermsURL:         "https://example.com/terms",
		SupportEmail:     "support@example.com",
	}

	// This sends the email asynchronously (non-blocking)
	mailService.SendOrderConfirmationToCustomer("customer@example.com", data)

	// Send a refund notification
	refundData := mailer.RefundData{
		MerchantName:  "Burger King",
		MerchantLogo:  "https://example.com/logo.png",
		MerchantColor: "#E2F2F9",
		Amount:        "15.50 €",
		Date:          "12/02/2024",
		CustomerName:  "John Doe",
		RefundReason:  "requested_by_customer",
		CardBrand:     "Visa",
		CardLast4:     "4242",
		ReceiptURL:    "https://example.com/receipt/123",
		SupportEmail:  "support@example.com",
	}

	mailService.SendRefundNotification("customer@example.com", refundData)

	// Send a payout notification
	payoutData := mailer.PayoutData{
		MerchantName: "Burger King",
		MerchantLogo: "https://example.com/logo.png",
		Destination:  "Bank Account",
		Status:       "paid",
		Amount:       "1450.00 €",
		PayoutDate:   "12/02/2024",
		ArrivalDate:  "14/02/2024",
		BankName:     "BNP Paribas",
		AccountLast4: "6789",
		PayoutID:     "po_1Mn...",
		DashboardURL: "https://dashboard.example.com",
	}

	mailService.SendPayoutPaidNotification("merchant@example.com", "Merchant Name", payoutData)
}

// ExampleCustomSendAsync demonstrates custom email sending
func ExampleCustomSendAsync() {
	cfg := Config{
		APIKey: "your-brevo-api-key",
	}

	mailService := NewBrevoMailer(cfg)

	customData := map[string]string{
		"name":   "John",
		"amount": "50.00 €",
	}

	// Send a custom email with a specific template
	mailService.SendAsync(
		"from@example.com",
		"to@example.com",
		"Custom Subject",
		"custom_template.html", // Your template name
		customData,
	)
}

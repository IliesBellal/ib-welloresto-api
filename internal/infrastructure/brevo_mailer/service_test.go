package brevo_mailer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/infrastructure/mailer"
)

// MockBrevoServer mocks the Brevo email API
func mockBrevoEmailServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		if r.Header.Get("api-key") != "test-api-key" {
			t.Errorf("Expected valid api-key header")
		}

		// Return success response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"messageId": "123456789"}`))
	}))
}

// TestSendAsync tests the SendAsync method
func TestSendAsync(t *testing.T) {
	// Create a mock server
	server := mockBrevoEmailServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	mailer := NewBrevoMailer(cfg)

	// Test data
	data := map[string]string{
		"name": "John Doe",
	}

	// Send async (this should not block)
	mailer.SendAsync("sender@example.com", "sender@example.com", "recipient@example.com", "Test Subject", "test.html", data)

	// Give goroutine time to execute (in real tests, use proper synchronization)
	// For production, use sync.WaitGroup or channels
}

// TestSendOrderConfirmationToCustomer tests order confirmation email
func TestSendOrderConfirmationToCustomer(t *testing.T) {
	server := mockBrevoEmailServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	mailService := NewBrevoMailer(cfg)

	data := mailer.ScanNOrderConfirmationData{
		MerchantName:     "Test Merchant",
		MerchantLogo:     "https://example.com/logo.png",
		MerchantCurrency: "EUR",
		OrderTotal:       "25.50 €",
		OrderDate:        "12/02/2024",
		TrackingURL:      "https://example.com/track/123",
		PrivacyURL:       "https://example.com/privacy",
		TermsURL:         "https://example.com/terms",
		SupportEmail:     "support@example.com",
	}

	// Send order confirmation
	mailService.SendOrderConfirmationToCustomer("customer@example.com", data)

	// Test passed - no panic means success
}

// TestSendRefundNotification tests refund notification email
func TestSendRefundNotification(t *testing.T) {
	server := mockBrevoEmailServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	mailService := NewBrevoMailer(cfg)

	data := mailer.RefundData{
		MerchantName:  "Test Merchant",
		MerchantLogo:  "https://example.com/logo.png",
		MerchantColor: "#E2F2F9",
		Amount:        "25.50 €",
		Date:          "12/02/2024",
		CustomerName:  "John Doe",
		RefundReason:  "requested_by_customer",
		CardBrand:     "Visa",
		CardLast4:     "4242",
		ReceiptURL:    "https://example.com/receipt/123",
		SupportEmail:  "support@example.com",
	}

	// Send refund notification
	mailService.SendRefundNotification("customer@example.com", data)

	// Test passed - no panic means success
}

// BenchmarkSendAsync benchmarks the SendAsync method
func BenchmarkSendAsync(b *testing.B) {
	server := mockBrevoEmailServer(&testing.T{})
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	mailer := NewBrevoMailer(cfg)

	data := map[string]string{
		"name": "John Doe",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mailer.SendAsync("sender@example.com", "sender@example.com", "recipient@example.com", "Test Subject", "test.html", data)
	}
}

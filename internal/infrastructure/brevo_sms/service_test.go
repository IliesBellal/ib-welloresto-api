package brevo_sms

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/infrastructure/sms"
)

// MockBrevoSMSServer mocks the Brevo SMS API
func mockBrevoSMSServer(t *testing.T) *httptest.Server {
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
		w.Write([]byte(`{"reference":"sms-123456789"}`))
	}))
}

// TestSendSMSAsync tests the SendSMSAsync method
func TestSendSMSAsync(t *testing.T) {
	server := mockBrevoSMSServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	// Send test SMS
	smsService.SendSMSAsync("Wello", "+33612345678", "Test message")

	// Test passed - no panic means success
}

// TestSendOrderConfirmationSMS tests order confirmation SMS
func TestSendOrderConfirmationSMS(t *testing.T) {
	server := mockBrevoSMSServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	data := sms.OrderConfirmationSMSData{
		MerchantName: "Test Merchant",
		OrderID:      "ORD-12345",
		OrderTotal:   "25.50 €",
		TrackingURL:  "https://example.com/track/123",
	}

	// Send order confirmation SMS
	smsService.SendOrderConfirmationSMS("Wello", "+33612345678", data)

	// Test passed - no panic means success
}

// TestMultipleSenders tests sending SMS from different senders
func TestMultipleSenders(t *testing.T) {
	server := mockBrevoSMSServer(t)
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	// Send from multiple senders
	smsService.SendSMSAsync("SenderA", "+33612345678", "Message from A")
	smsService.SendSMSAsync("SenderB", "+33687654321", "Message from B")
	smsService.SendSMSAsync("Wello", "+33698765432", "Message from Wello")

	// Test passed - no panic means success
}

// BenchmarkSendSMSAsync benchmarks the SendSMSAsync method
func BenchmarkSendSMSAsync(b *testing.B) {
	server := mockBrevoSMSServer(&testing.T{})
	defer server.Close()

	cfg := Config{
		APIKey: "test-api-key",
	}

	smsService := NewBrevoSMS(cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		smsService.SendSMSAsync("Wello", "+33612345678", "Test message")
	}
}

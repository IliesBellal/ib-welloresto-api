package brevo_mailer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"
)

// Config holds Brevo API configuration
type Config struct {
	APIKey string
}

// BrevoMailer is the implementation of mailer.Service using Brevo API
type BrevoMailer struct {
	apiKey     string
	httpClient *http.Client
}

// NewBrevoMailer creates a new instance of the BrevoMailer service
func NewBrevoMailer(cfg Config) mailer.Service {
	return &BrevoMailer{
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{},
	}
}

// SendAsync sends an email in a separate Goroutine (non-blocking).
// templateName must match the filename in the templates/ folder (e.g., "payout_notification.html").
// data is the struct or map containing variables to inject into the HTML.
func (b *BrevoMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	go func() {
		html, err := b.renderTemplate(templateName, data)
		if err != nil {
			log.Printf("ERROR rendering template %s: %v", templateName, err)
			return
		}

		err = b.sendEmailViaBrevo(fromName, fromEmail, to, subject, html)
		if err != nil {
			log.Printf("ERROR sending email via Brevo to %s: %v", to, err)
		} else {
			log.Printf("Email sent successfully via Brevo to %s", to)
		}
	}()
}

// SendOrderConfirmationToCustomer sends an order confirmation email
func (b *BrevoMailer) SendOrderConfirmationToCustomer(to string, data mailer.ScanNOrderConfirmationData) {
	b.SendAsync(data.MerchantName, mailer.InvoiceEmail, to, "Confirmation de votre commande", "scannorder_payout.html", data)
}

// SendOrderConfirmationToCustomer sends an order confirmation email
func (b *BrevoMailer) SendOTP(data mailer.MfaOTPData) {
	email_data := mailer.MFAMailData{
		EmailBaseData: mailer.EmailBaseData{
			BrandName:    "Wello Resto",
			Year:         time.Now().Year(),
			SupportEmail: mailer.SupportEmail,
			BrandLogoURL: mailer.BrandLogoURL,
		},
		MFACode:   data.OTP,
		ExpiresIn: int(models.OTPCacheTTL.Minutes()),
	}
	b.SendAsync("Wello Resto - Security", mailer.SecurityEmail, data.UserEmail, "Votre code de vérification MFA", "otp.html", email_data)
}

// SendRefundNotification sends a refund notification email
func (b *BrevoMailer) SendRefundNotification(email string, data mailer.RefundData) {
	b.SendAsync("Wello Resto", mailer.InvoiceEmail, email, "Remboursement", "customer_refund.html", data)
}

// SendPayoutPaidNotification sends a payout notification email
func (b *BrevoMailer) SendPayoutPaidNotification(email string, name string, payout mailer.PayoutData) {
	go func() {
		html, renderErr := b.renderTemplate("payout_notification.html", payout)
		if renderErr != nil {
			log.Printf("ERROR rendering payout template: %v", renderErr)
			return
		}

		err := b.sendEmailViaBrevo("Wello Resto", mailer.InvoiceEmail, email, "WR ScanNOrder - Virement en cours", html)
		if err != nil {
			log.Printf("ERROR sending payout email via Brevo to %s: %v", email, err)
		} else {
			log.Printf("Payout email sent successfully via Brevo to %s", email)
		}
	}()
}

// TriggerTestEmail sends a test email
func (b *BrevoMailer) TriggerTestEmail(writer http.ResponseWriter, request *http.Request) {
	data := mailer.ScanNOrderConfirmationData{
		OrderTotal:       "12.50 €",
		MerchantCurrency: "EUR",
		MerchantName:     "Brasserie Du Midi",
		OrderDate:        "22/08/1997",
		TrackingURL:      "http://dashboard",
	}

	b.SendOrderConfirmationToCustomer("iliesbellal@gmail.com", data)
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("Test email triggered"))
}

// sendEmailViaBrevo sends the actual email via Brevo API
func (b *BrevoMailer) sendEmailViaBrevo(from_name, from_email, to, subject, htmlContent string) error {
	// Prepare the request payload
	payload := map[string]interface{}{
		"sender": map[string]string{
			"name":  from_name,
			"email": from_email, // Using the configured From address
		},
		"to": []map[string]string{
			{
				"email": to,
			},
		},
		"subject":     subject,
		"htmlContent": htmlContent,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", b.apiKey)

	// Send the request
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("brevo API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// renderTemplate parses and executes a template using the mailer package's embedded templates
func (b *BrevoMailer) renderTemplate(templateName string, data interface{}) (string, error) {
	// Use the helper function from the mailer package which has access to the embedded FS
	return mailer.RenderTemplate(templateName, data)
}

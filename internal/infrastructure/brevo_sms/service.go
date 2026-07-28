package brevo_sms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"welloresto-api/internal/infrastructure/sms"
)

// Config holds Brevo API configuration
type Config struct {
	APIKey string
}

// BrevoSMS is the implementation of sms.Service using Brevo API
type BrevoSMS struct {
	apiKey     string
	httpClient *http.Client
}

type sendSMSResponse struct {
	MessageID interface{} `json:"messageId"`
}

// NewBrevoSMS creates a new instance of the BrevoSMS service
func NewBrevoSMS(cfg Config) sms.Service {
	return &BrevoSMS{
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{},
	}
}

// SendSMSAsync sends an SMS message asynchronously.
// senderID is the identifier of the sender (e.g., "Wello", merchant name, etc.)
// phoneNumber should be in international format (e.g., "+33612345678")
func (b *BrevoSMS) SendSMSAsync(senderID, phoneNumber, message string) {
	go func() {
		_, err := b.sendSMSViaBrevo(senderID, phoneNumber, message)
		if err != nil {
			log.Printf("ERROR sending SMS via Brevo to %s: %v", phoneNumber, err)
		} else {
			log.Printf("SMS sent successfully via Brevo to %s (sender: %s)", phoneNumber, senderID)
		}
	}()
}

// SendSMSAsyncWithMessageID sends an SMS asynchronously and returns the Brevo
// messageId through the callback when available.
func (b *BrevoSMS) SendSMSAsyncWithMessageID(senderID, phoneNumber, message string, onSent func(messageID string)) {
	go func() {
		messageID, err := b.sendSMSViaBrevo(senderID, phoneNumber, message)
		if err != nil {
			log.Printf("ERROR sending SMS via Brevo to %s: %v", phoneNumber, err)
			return
		}
		if onSent != nil {
			onSent(messageID)
		}
		log.Printf("SMS sent successfully via Brevo to %s (sender: %s)", phoneNumber, senderID)
	}()
}

// SendOrderConfirmationSMS sends an order confirmation SMS
func (b *BrevoSMS) SendOrderConfirmationSMS(senderID, phoneNumber string, data sms.OrderConfirmationSMSData) {
	message := fmt.Sprintf(
		"Bonjour,\n\nVotre commande #%s chez %s d'un montant de %s a été confirmée.\n\nSuivez votre commande: %s",
		data.OrderID,
		data.MerchantName,
		data.OrderTotal,
		data.TrackingURL,
	)
	b.SendSMSAsync(senderID, phoneNumber, message)
}

// TriggerTestSMS sends a test SMS
func (b *BrevoSMS) TriggerTestSMS(writer http.ResponseWriter, request *http.Request) {
	data := sms.OrderConfirmationSMSData{
		MerchantName: "Brasserie Du Midi",
		OrderID:      "ORD-12345",
		OrderTotal:   "12.50 €",
		TrackingURL:  "http://dashboard",
	}

	b.SendOrderConfirmationSMS("Wello", "+33609217928", data)
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("Test SMS triggered"))
}

// sendSMSViaBrevo sends the actual SMS via Brevo API
func (b *BrevoSMS) sendSMSViaBrevo(senderID, phoneNumber, message string) (string, error) {
	// Prepare the request payload according to Brevo SMS API
	// Reference: https://developers.brevo.com/reference/sendtransactional-sms
	payload := map[string]interface{}{
		"sender":    senderID,
		"recipient": phoneNumber,
		"content":   message,
		"type":      "transactional", // Type of SMS (transactional or marketing)
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/transactionalSMS/send", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", b.apiKey)

	// Send the request
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// Check response status
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("brevo SMS API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var parsed sendSMSResponse
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		switch v := parsed.MessageID.(type) {
		case string:
			return strings.TrimSpace(v), nil
		case float64:
			return strconv.FormatInt(int64(v), 10), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		}
	}

	return "", nil
}

func (b *BrevoSMS) SendOTP(tel, otp string) {
	message := fmt.Sprintf("Votre code de vérification est le %s\n Un employé de Wello Resto ne vous demandera jamais votre code secret.", otp)

	b.SendSMSAsync("Wello Resto", tel, message)
}

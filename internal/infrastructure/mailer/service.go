package mailer

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
)

// Content holds the email content files.
// go:embed templates/*.html instruction tells Go to include these files in the binary.
//
//go:embed templates/*.html
var templateFS embed.FS

// Config holds SMTP configuration
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // L'adresse "invoice@welloresto.fr"
}

// Service defines the interface for sending emails.
// This allows you to mock the mailer in your unit tests.
type Service interface {
	SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{})
	SendOrderConfirmationToCustomer(to string, data ScanNOrderConfirmationData)
	SendRefundNotification(s string, data RefundData)
	SendPayoutPaidNotification(email string, name string, payout PayoutData)
	SendMfaOTP(data MfaOTPData)

	TriggerTestEmail(writer http.ResponseWriter, request *http.Request)
}

/*
// mailer is the implementation of Service
type mailer struct {
	dialer *gomail.Dialer
	config Config
}

// NewMailer creates a new instance of the mailer service
func NewMailer(cfg Config) Service {
	// Configuration du Dialer SMTP (Hostinger)
	d := gomail.NewDialer(cfg.Host, cfg.Port, cfg.Username, cfg.Password)

	return &mailer{
		dialer: d,
		config: cfg,
	}
}
func (m *mailer) SendPayoutPaidNotification(email string, name string, payout PayoutData) {
	// On lance une Goroutine pour ne pas bloquer l'API (Stripe n'attendra pas !)
	go func() {
		err := m.send("Wello Resto", email, "WR ScanNOrder - Virement en cours", "payout_notification.html", payout)
		if err != nil {
			// ICI: Idéalement, utilisez votre logger structuré (zap, logrus)
			log.Printf("ERROR sending email to %s: %v", email, err)
		} else {
			log.Printf("Email sent successfully to %s", email)
		}
	}()
}

// SendAsync sends an email in a separate Goroutine (non-blocking).
// templateName must match the filename in the templates/ folder (e.g., "payout_notification.html").
// data is the struct or map containing variables to inject into the HTML.
func (m *mailer) SendAsync(from, to, subject, templateName string, data interface{}) {
	// On lance une Goroutine pour ne pas bloquer l'API (Stripe n'attendra pas !)
	go func() {
		err := m.send(from, to, subject, templateName, data)
		if err != nil {
			// ICI: Idéalement, utilisez votre logger structuré (zap, logrus)
			log.Printf("ERROR sending email to %s: %v", to, err)
		} else {
			log.Printf("Email sent successfully to %s", to)
		}
	}()
}

func (m *mailer) SendOrderConfirmationToCustomer(to string, data ScanNOrderConfirmationData) {
	m.SendAsync(data.MerchantName, to, "Confirmation de votre commande", "scannorder_payout.html", data)
}

func (s *mailer) TriggerTestEmail(writer http.ResponseWriter, request *http.Request) {
	data := ScanNOrderConfirmationData{
		OrderTotal:   "12.50 €",
		MerchantLogo: "EUR",
		MerchantName: "Brasserie Du Midi",
		OrderDate:    "22/08/1997",
		TrackingURL:  "http://dashboard",
	}

	s.SendOrderConfirmationToCustomer("iliesbellal@gmail.com", data)
}

type RefundMailData struct {
	Amount   float64
	Currency string
}

func (m *mailer) SendRefundNotification(email string, data RefundData) {
	m.SendAsync("Wello Resto", email, "Remboursement", "customer_refund.html", data)
}

// send contains the actual logic (parsing + sending)
func (m *mailer) send(from, to, subject string, templateName string, data interface{}) error {
	// 1. Parsing du template HTML
	// On utilise templateFS pour lire le fichier inclus dans le binaire
	tmpl, err := template.ParseFS(templateFS, "templates/"+templateName)
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", templateName, err)
	}

	// 2. Injection des données (data) dans le HTML
	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	// 3. Construction du message
	msg := gomail.NewMessage()
	msg.SetHeader("From", msg.FormatAddress(m.config.From, from))
	msg.SetHeader("To", to)
	msg.SetHeader("Subject", subject)
	msg.SetBody("text/html", body.String())

	// 4. Envoi via SMTP
	if err := m.dialer.DialAndSend(msg); err != nil {
		return fmt.Errorf("smtp error: %w", err)
	}

	return nil
}
*/

// RenderTemplate is a public helper function to render templates from the embedded FS
// This allows other services (like BrevoMailer) to reuse the same templates
func RenderTemplate(templateName string, data interface{}) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/"+templateName)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", templateName, err)
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return body.String(), nil
}

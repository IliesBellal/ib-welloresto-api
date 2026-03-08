package brevo

import (
	"log"

	"welloresto-api/internal/config"
	"welloresto-api/internal/infrastructure/brevo_mailer"
	"welloresto-api/internal/infrastructure/brevo_sms"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
)

// Services holds all Brevo-based services
type Services struct {
	MailService mailer.Service
	SMSService  sms.Service
}

// Initialize creates and returns initialized Brevo services
// This function should be called once during application startup
func Initialize(cfg *config.AppConfig) *Services {
	// Validate API key is configured
	if cfg.Brevo.APIKey == "" {
		log.Fatal("BREVO_API_KEY environment variable is not set")
	}

	// Create BrevoMailer service
	mailService := brevo_mailer.NewBrevoMailer(brevo_mailer.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	// Create BrevoSMS service
	smsService := brevo_sms.NewBrevoSMS(brevo_sms.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	log.Println("✓ Brevo services initialized successfully")

	return &Services{
		MailService: mailService,
		SMSService:  smsService,
	}
}

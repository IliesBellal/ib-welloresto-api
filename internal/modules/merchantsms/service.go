// Package merchantsms est le point d'envoi des SMS faits pour le compte d'un
// établissement (suivi de livraison, réservations, liste d'attente, planning,
// confirmation ScanNOrder). Chaque SMS effectivement accepté par Brevo est
// comptabilisé dans merchant_sms_monthly (nombre + coût au prix unitaire de
// l'établissement). Les SMS propres à Wello (OTP, relances de paiement) ne
// passent pas par ici et ne sont pas comptés.
package merchantsms

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/sms"

	"go.uber.org/zap"
)

// defaultSenderName est l'expéditeur utilisé quand l'établissement n'a pas de
// nom d'expéditeur SMS exploitable (Brevo : 11 caractères alphanumériques max).
const (
	defaultSenderName   = "Wello Resto"
	maxSenderNameLength = 11
)

// Sender envoie un SMS de façon synchrone (brevo_sms.BrevoSMS.SendSMS).
type Sender interface {
	SendSMS(senderID, phoneNumber, message string) (messageID string, err error)
}

// SMSService est le contrat consommé par le module delivery_sessions.
type SMSService interface {
	SendOrderTrackingSMS(ctx context.Context, merchantID string, orderID string, customerPhone string) error
}

type Service struct {
	repo   MarketingRepository
	sender Sender
	log    *zap.Logger
	// scannorderBaseURL : SCANNORDER_BASE_URL, base du lien du SMS de suivi
	// (cf. SetScanNOrderBaseURL). Vide = helpers.DefaultScanNOrderBaseURL.
	scannorderBaseURL string
}

// SetScanNOrderBaseURL fixe la base du lien envoyé dans le SMS de suivi.
func (s *Service) SetScanNOrderBaseURL(baseURL string) {
	s.scannorderBaseURL = baseURL
}

func NewService(repo MarketingRepository, sender Sender, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{repo: repo, sender: sender, log: log}
}

// Send envoie un SMS pour le compte de merchantID et le comptabilise s'il a
// été accepté par Brevo. Un échec d'envoi n'est jamais compté ; un échec de
// comptage est journalisé sans faire échouer l'envoi, puisque le SMS est parti.
func (s *Service) Send(ctx context.Context, merchantID, senderID, phoneNumber, message string) (string, error) {
	if s.sender == nil {
		return "", errors.New("merchantsms: no SMS sender configured")
	}
	messageID, err := s.sender.SendSMS(senderID, phoneNumber, message)
	if err != nil {
		return "", err
	}
	s.recordSent(ctx, merchantID)
	return messageID, nil
}

// SendAsync est la variante non bloquante de Send. onSent n'est appelé qu'en
// cas de succès, avec l'identifiant Brevo du message.
func (s *Service) SendAsync(ctx context.Context, merchantID, senderID, phoneNumber, message string, onSent func(messageID string)) {
	// La requête d'origine peut être terminée avant la fin de l'envoi : le
	// comptage ne doit pas hériter de son annulation.
	ctx = context.WithoutCancel(ctx)
	go func() {
		messageID, err := s.Send(ctx, merchantID, senderID, phoneNumber, message)
		if err != nil {
			s.log.Warn("merchantsms: SMS not sent", zap.String("merchant_id", merchantID), zap.Error(err))
			return
		}
		if onSent != nil {
			onSent(messageID)
		}
	}()
}

// SendOrderConfirmationSMS envoie, sans bloquer, la confirmation de commande
// ScanNOrder au client de l'établissement.
func (s *Service) SendOrderConfirmationSMS(ctx context.Context, merchantID, phoneNumber string, data sms.OrderConfirmationSMSData) {
	s.SendAsync(ctx, merchantID, "Wello", phoneNumber, sms.OrderConfirmationMessage(data), nil)
}

var internationalPhonePattern = regexp.MustCompile(`^\+[0-9]{8,15}$`)

func (s *Service) SendOrderTrackingSMS(
	ctx context.Context,
	merchantID string,
	orderID string,
	customerPhone string,
) error {
	settings, err := s.repo.GetMarketingSettings(ctx, merchantID)
	if err != nil || !settings.SMSEnabled {
		return nil
	}

	phone := helpers.NormalizePhoneNumber(customerPhone, "FR")
	if !internationalPhonePattern.MatchString(phone) {
		return fmt.Errorf("invalid phone")
	}

	// orderID est l'id interne : il sert à retrouver la commande, jamais à
	// être montré au client (lien = id public, {order_id} = numéro de retrait).
	ref, err := s.repo.GetOrderTrackingRef(ctx, merchantID, orderID)
	if err != nil {
		return fmt.Errorf("order tracking ref: %w", err)
	}
	trackingURL := helpers.ScanNOrderOrderURL(s.scannorderBaseURL, settings.QRCode, ref.PublicID)
	if trackingURL == "" {
		return fmt.Errorf("order %s has no public id or merchant has no QR code", orderID)
	}

	message := strings.ReplaceAll(
		settings.TrackingTemplate,
		"{tracking_url}",
		trackingURL,
	)

	message = strings.ReplaceAll(message, "{order_id}", ref.OrderNum)

	_, err = s.Send(ctx, merchantID, senderName(settings.SMSSenderName), phone, message)
	return err
}

func (s *Service) recordSent(ctx context.Context, merchantID string) {
	if strings.TrimSpace(merchantID) == "" {
		s.log.Warn("merchantsms: SMS sent without merchant, not counted")
		return
	}
	unitPrice, err := s.repo.GetSMSUnitPrice(ctx, merchantID)
	if err != nil {
		s.log.Warn("merchantsms: SMS unit price unavailable, default price applied", zap.String("merchant_id", merchantID), zap.Error(err))
		unitPrice = defaultSMSUnitPrice
	}
	if err := s.repo.RecordSMSCost(ctx, merchantID, 1, unitPrice); err != nil {
		s.log.Error("merchantsms: failed to count sent SMS", zap.String("merchant_id", merchantID), zap.Error(err))
	}
}

// senderName retient le nom d'expéditeur de l'établissement s'il respecte la
// contrainte Brevo, sinon l'expéditeur Wello par défaut.
func senderName(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" || len([]rune(configured)) > maxSenderNameLength {
		return defaultSenderName
	}
	return configured
}

// Package bookingcomm centralise l'envoi des messages de communication liés
// aux réservations (confirmation, rappel, modification, annulation,
// reconfirmation, liste d'attente, post-visite). Il consomme les services
// Brevo existants (mailer.Service, sms.Service) sans créer de nouveau client
// HTTP, et ne dépend d'aucun module métier (bookings, reservation) pour
// éviter tout cycle d'import — les appelants construisent des structs de
// données primitives (BookingMessage, WaitlistMessage).
//
// Contenus par défaut, non éditables par le restaurateur au lancement
// (cadrage v0.6 §6.4) : email toujours envoyé si une adresse est connue, SMS
// uniquement si le canal est activé (bookings_settings.sms_enabled).
package bookingcomm

import (
	"context"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"

	"go.uber.org/zap"
)

const smsSender = "Wello Resto"

type Service struct {
	mailer  mailer.Service
	sms     sms.Service
	baseURL string
	log     *zap.Logger
}

// New instancie le service. baseURL est la racine publique utilisée pour
// construire le lien de gestion (PUBLIC_RESERVATION_BASE_URL) ; mailer/sms
// peuvent être nil dans les tests unitaires qui n'exercent pas l'envoi.
func New(mail mailer.Service, smsSvc sms.Service, baseURL string, log *zap.Logger) *Service {
	return &Service{mailer: mail, sms: smsSvc, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), log: log}
}

// BookingMessage porte les données primitives nécessaires à l'envoi d'un
// message lié à une réservation. Construit par l'appelant (bookings,
// reservation, tasks) à partir de son propre modèle.
type BookingMessage struct {
	MerchantSlug  string // slug /rsv/{slug} ; vide => pas de lien de gestion
	MerchantName  string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	BookingNumber string
	DateLabel     string // date formatée locale, ex "vendredi 12 juillet 2026"
	TimeLabel     string // heure formatée locale, ex "20:00"
	PartySize     int
	SMSEnabled    bool
}

func (m BookingMessage) managementLink(baseURL string) string {
	if baseURL == "" || strings.TrimSpace(m.MerchantSlug) == "" || strings.TrimSpace(m.BookingNumber) == "" {
		return ""
	}
	return fmt.Sprintf("%s/restaurant/%s/reservation/%s", baseURL, m.MerchantSlug, m.BookingNumber)
}

func (s *Service) emailData(m BookingMessage) mailer.BookingMessageData {
	return mailer.BookingMessageData{
		EmailBaseData:  s.emailBase(),
		MerchantName:   m.MerchantName,
		CustomerName:   m.CustomerName,
		BookingNumber:  m.BookingNumber,
		DateLabel:      m.DateLabel,
		TimeLabel:      m.TimeLabel,
		PartySize:      m.PartySize,
		ManagementLink: m.managementLink(s.baseURL),
	}
}

func (s *Service) emailBase() mailer.EmailBaseData {
	return mailer.EmailBaseData{
		BrandName:    "Wello Resto",
		BrandLogoURL: mailer.BrandLogoURL,
		SupportEmail: mailer.SupportEmail,
		Year:         time.Now().Year(),
	}
}

func (s *Service) sendEmail(m BookingMessage, subject, template string) {
	if s.mailer == nil || strings.TrimSpace(m.CustomerEmail) == "" {
		return
	}
	s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, subject, template, s.emailData(m))
}

func (s *Service) sendSMS(m BookingMessage, text string) {
	if !m.SMSEnabled || s.sms == nil || strings.TrimSpace(m.CustomerPhone) == "" {
		return
	}
	s.sms.SendSMSAsync(smsSender, helpers.NormalizePhoneNumber(m.CustomerPhone, "FR"), text)
}

// SendConfirmation envoie la confirmation immédiate à la création d'une
// réservation confirmed.
func (s *Service) SendConfirmation(ctx context.Context, m BookingMessage) {
	s.sendEmail(m, "Votre réservation est confirmée", "booking_confirmation.html")
	s.sendSMS(m, fmt.Sprintf(
		"Votre reservation chez %s le %s a %s (%d pers.) est confirmee. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	))
}

// SendReminder envoie le rappel avant service (J-1 par défaut, cf. cron).
func (s *Service) SendReminder(ctx context.Context, m BookingMessage) {
	s.sendEmail(m, "Rappel de votre réservation", "booking_reminder.html")
	s.sendSMS(m, fmt.Sprintf(
		"Rappel : reservation chez %s le %s a %s (%d pers.). Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	))
}

// SendModification notifie une modification de réservation (client ou staff).
func (s *Service) SendModification(ctx context.Context, m BookingMessage) {
	s.sendEmail(m, "Votre réservation a été modifiée", "booking_modification.html")
	s.sendSMS(m, fmt.Sprintf(
		"Votre reservation chez %s a ete modifiee : %s a %s (%d pers.). Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	))
}

// SendCancellation notifie une annulation (client, staff ou système).
func (s *Service) SendCancellation(ctx context.Context, m BookingMessage) {
	s.sendEmail(m, "Votre réservation a été annulée", "booking_cancellation.html")
	s.sendSMS(m, fmt.Sprintf(
		"Votre reservation chez %s le %s a %s a ete annulee. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.BookingNumber,
	))
}

// SendReconfirmation demande au client de confirmer sa venue (réponse SMS
// OUI/NON traitée par le webhook internal/webhook/brevo_sms_reply).
func (s *Service) SendReconfirmation(ctx context.Context, m BookingMessage) {
	s.sendEmail(m, "Merci de confirmer votre réservation", "booking_reconfirmation.html")
	s.sendSMS(m, fmt.Sprintf(
		"Confirmez-vous votre reservation chez %s le %s a %s (%d pers.) ? Repondez OUI ou NON. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	))
}

// SendPostVisit envoie un message de remerciement post-visite (Should,
// cadrage §6.4). Préparée mais volontairement non branchée dans le cron
// cette session — email uniquement, pas de variante SMS.
func (s *Service) SendPostVisit(ctx context.Context, m BookingMessage) {
	if s.mailer == nil || strings.TrimSpace(m.CustomerEmail) == "" {
		return
	}
	data := mailer.BookingPostVisitData{
		EmailBaseData: s.emailBase(),
		MerchantName:  m.MerchantName,
		CustomerName:  m.CustomerName,
	}
	s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, "Merci de votre visite", "booking_post_visit.html", data)
}

// WaitlistMessage porte les données pour la notification "table disponible"
// envoyée au premier de la liste d'attente.
type WaitlistMessage struct {
	MerchantName  string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	PartySize     int
	ExpiryMinutes int
	SMSEnabled    bool
}

// SendWaitlistAvailable notifie un client de liste d'attente qu'une table
// s'est libérée. Unique point d'envoi pour ce type de message (remplace les
// appels mailer/sms directs faits en Phase 3).
func (s *Service) SendWaitlistAvailable(ctx context.Context, m WaitlistMessage) {
	if s.mailer != nil && strings.TrimSpace(m.CustomerEmail) != "" {
		data := mailer.WaitlistAvailableData{
			EmailBaseData: s.emailBase(),
			MerchantName:  m.MerchantName,
			CustomerName:  m.CustomerName,
			PartySize:     m.PartySize,
			ExpiryMinutes: m.ExpiryMinutes,
		}
		s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, "Une table s'est libérée", "waitlist_available.html", data)
	}

	if m.SMSEnabled && s.sms != nil && strings.TrimSpace(m.CustomerPhone) != "" {
		text := fmt.Sprintf(
			"Bonne nouvelle ! Une table pour %d personne(s) s'est liberee chez %s. Elle vous est reservee %d min.",
			m.PartySize, m.MerchantName, m.ExpiryMinutes,
		)
		s.sms.SendSMSAsync(smsSender, helpers.NormalizePhoneNumber(m.CustomerPhone, "FR"), text)
	}
}

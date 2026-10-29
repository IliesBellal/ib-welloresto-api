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
	"welloresto-api/internal/ics"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/modules/outbound"

	"go.uber.org/zap"
)

const smsSender = "Wello Resto"

type Service struct {
	mailer      mailer.Service
	sms         sms.Service
	merchantSMS merchantSMSSender
	brands      brandResolver
	baseURL     string
	outbound    outboundRecorder
	log         *zap.Logger
}

// brandResolver lit l'identité (logo, couleur) de l'établissement qui habille
// ses mails clients (merchantbrand.Repository).
type brandResolver interface {
	Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand
}

// merchantSMSSender envoie un SMS pour le compte d'un établissement et le
// comptabilise dans son compteur mensuel (merchantsms.Service).
type merchantSMSSender interface {
	SendAsync(ctx context.Context, merchantID, senderID, phoneNumber, message string, onSent func(messageID string))
}

type outboundRecorder interface {
	RecordOutboundMessageWithContext(ctx context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error
}

type asyncMailerWithMessageID interface {
	SendAsyncWithMessageID(fromName, fromEmail, to, subject, templateName string, data interface{}, onSent func(messageID string))
}

type asyncMailerWithAttachmentsAndMessageID interface {
	SendAsyncWithAttachmentsAndMessageID(fromName, fromEmail, to, subject, templateName string, data interface{}, attachments []mailer.Attachment, onSent func(messageID string))
}

type asyncSMSWithMessageID interface {
	SendSMSAsyncWithMessageID(senderID, phoneNumber, message string, onSent func(messageID string))
}

// New instancie le service. baseURL est la racine publique utilisée pour
// construire le lien de gestion (PUBLIC_RESERVATION_BASE_URL) ; mailer/sms
// peuvent être nil dans les tests unitaires qui n'exercent pas l'envoi.
func New(mail mailer.Service, smsSvc sms.Service, baseURL string, outboundSvc outboundRecorder, log *zap.Logger) *Service {
	return &Service{mailer: mail, sms: smsSvc, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), outbound: outboundSvc, log: log}
}

// SetMerchantSMS fait passer les SMS des messages portant un MerchantID par
// le compteur SMS de l'établissement. Sans lui (tests), sms.Service est utilisé
// directement et rien n'est comptabilisé.
func (s *Service) SetMerchantSMS(merchantSMS merchantSMSSender) {
	s.merchantSMS = merchantSMS
}

// SetBrandResolver habille les mails aux couleurs et au logo de
// l'établissement. Sans lui (tests), le cadre par défaut porte seulement son nom.
func (s *Service) SetBrandResolver(brands brandResolver) {
	s.brands = brands
}

func (s *Service) brand(ctx context.Context, merchantID, merchantName string) mailer.MerchantBrand {
	if s.brands == nil {
		return mailer.NewMerchantBrand(merchantName, "", "", "")
	}
	return s.brands.Resolve(ctx, merchantID, merchantName)
}

// sendSMSText envoie le SMS — compté pour merchantID quand c'est possible —
// puis enregistre le message sortant via onSent (succès uniquement).
func (s *Service) sendSMSText(ctx context.Context, merchantID, normalizedPhone, text string, onSent func(messageID string)) {
	if s.merchantSMS != nil && strings.TrimSpace(merchantID) != "" {
		s.merchantSMS.SendAsync(ctx, merchantID, smsSender, normalizedPhone, text, onSent)
		return
	}
	if trackedSMS, ok := s.sms.(asyncSMSWithMessageID); ok {
		trackedSMS.SendSMSAsyncWithMessageID(smsSender, normalizedPhone, text, onSent)
		return
	}
	s.sms.SendSMSAsync(smsSender, normalizedPhone, text)
}

// BookingMessage porte les données primitives nécessaires à l'envoi d'un
// message lié à une réservation. Construit par l'appelant (bookings,
// reservation, tasks) à partir de son propre modèle.
type BookingMessage struct {
	MerchantID    string // établissement à qui le SMS est comptabilisé
	BookingID     string // identifiant interne réservation (domain_ref_id outbound)
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

	// Fichier d'agenda (.ics) joint à la confirmation, à la modification et à
	// l'annulation. StartAt/EndAt vides : pas de fichier. Confirmed : la
	// réservation est confirmée (modification) ou l'était (annulation) — une
	// demande encore en attente n'a jamais été ajoutée à l'agenda du client.
	StartAt         time.Time
	EndAt           time.Time
	MerchantAddress string
	Confirmed       bool
}

// calendarAction dit quel fichier d'agenda accompagne un mail de réservation.
type calendarAction int

const (
	noCalendar calendarAction = iota
	calendarPublish
	calendarCancel
)

// calendarEvent décrit la réservation pour son fichier .ics. L'UID est stable
// (identifiant de la réservation) et SEQUENCE croît avec le temps : la
// modification ou l'annulation met à jour l'événement déjà ajouté au lieu d'en
// créer un second.
func (s *Service) calendarEvent(m BookingMessage, action calendarAction) ics.Event {
	desc := fmt.Sprintf("Réservation pour %d personne(s), référence %s.", m.PartySize, m.BookingNumber)
	if link := m.managementLink(s.baseURL); link != "" {
		desc += "\nGérer la réservation : " + link
	}
	return ics.Event{
		UID:         m.BookingID + "@reservation.welloresto.fr",
		Sequence:    int(time.Now().Unix() / 60),
		Start:       m.StartAt,
		End:         m.EndAt,
		Summary:     "Réservation chez " + m.MerchantName,
		Description: desc,
		Location:    m.MerchantAddress,
		Cancelled:   action == calendarCancel,
	}
}

// calendarModify : une modification met à jour l'événement de l'agenda, sauf
// si la réservation est repassée en attente (rien à ajouter tant que
// l'établissement ne l'a pas acceptée).
func calendarModify(m BookingMessage) calendarAction {
	if !m.Confirmed {
		return noCalendar
	}
	return calendarPublish
}

// calendarFor renvoie l'action d'agenda effective : aucune sans horaires, sans
// identifiant stable, ou pour une réservation qui n'a jamais été confirmée.
func calendarFor(m BookingMessage, action calendarAction) calendarAction {
	if m.StartAt.IsZero() || !m.EndAt.After(m.StartAt) || strings.TrimSpace(m.BookingID) == "" {
		return noCalendar
	}
	if action != calendarPublish && !m.Confirmed {
		return noCalendar
	}
	return action
}

func (m BookingMessage) managementLink(baseURL string) string {
	if baseURL == "" || strings.TrimSpace(m.MerchantSlug) == "" || strings.TrimSpace(m.BookingNumber) == "" {
		return ""
	}
	return fmt.Sprintf("%s/restaurant/%s/booking/%s", baseURL, m.MerchantSlug, m.BookingNumber)
}

// smsWithManagementLink ajoute au corps du SMS le même lien de gestion que
// celui du bouton "Gérer ma réservation" de l'email (cf. emailData) ; renvoie
// le texte tel quel si le lien n'est pas disponible (slug/booking_number
// manquant, ou PUBLIC_RESERVATION_BASE_URL non configuré).
func (s *Service) smsWithManagementLink(m BookingMessage, text string) string {
	link := m.managementLink(s.baseURL)
	if link == "" {
		return text
	}
	return text + " " + link
}

func (s *Service) emailData(ctx context.Context, m BookingMessage) mailer.BookingMessageData {
	return mailer.BookingMessageData{
		EmailBaseData:  s.emailBase(),
		Brand:          s.brand(ctx, m.MerchantID, m.MerchantName),
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

func (s *Service) sendEmail(ctx context.Context, m BookingMessage, subject, template string, calendar calendarAction) {
	if s.mailer == nil || strings.TrimSpace(m.CustomerEmail) == "" {
		return
	}
	data := s.emailData(ctx, m)
	if calendar = calendarFor(m, calendar); calendar != noCalendar {
		event := s.calendarEvent(m, calendar)
		if calendar == calendarPublish {
			data.CalendarLink = ics.GoogleCalendarLink(event)
		}
		attachment := mailer.Attachment{Name: "reservation.ics", Content: ics.Build(event, "-//WelloResto//Reservation//FR")}
		if trackedMailer, ok := s.mailer.(asyncMailerWithAttachmentsAndMessageID); ok {
			trackedMailer.SendAsyncWithAttachmentsAndMessageID(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, subject, template, data, []mailer.Attachment{attachment}, func(messageID string) {
				s.recordOutbound(ctx, outbound.ChannelEmail, messageID, m.BookingID, m.CustomerEmail)
			})
			return
		}
		s.mailer.SendAsyncWithAttachment(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, subject, template, data, attachment.Content, attachment.Name)
		return
	}
	if trackedMailer, ok := s.mailer.(asyncMailerWithMessageID); ok {
		trackedMailer.SendAsyncWithMessageID(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, subject, template, data, func(messageID string) {
			s.recordOutbound(ctx, outbound.ChannelEmail, messageID, m.BookingID, m.CustomerEmail)
		})
		return
	}
	s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, subject, template, data)
}

func (s *Service) sendSMS(ctx context.Context, m BookingMessage, text string) {
	if !m.SMSEnabled || s.sms == nil || strings.TrimSpace(m.CustomerPhone) == "" {
		return
	}
	normalizedPhone := helpers.NormalizePhoneNumber(m.CustomerPhone, "FR")
	s.sendSMSText(ctx, m.MerchantID, normalizedPhone, text, func(messageID string) {
		s.recordOutbound(ctx, outbound.ChannelSMS, messageID, m.BookingID, normalizedPhone)
	})
}

func (s *Service) recordOutbound(ctx context.Context, channel, providerMessageID, domainRefID, recipient string) {
	if s.outbound == nil || strings.TrimSpace(providerMessageID) == "" || strings.TrimSpace(domainRefID) == "" {
		return
	}
	if err := s.outbound.RecordOutboundMessageWithContext(ctx, channel, "brevo", providerMessageID, "booking", domainRefID, recipient); err != nil && s.log != nil {
		s.log.Warn("bookingcomm outbound tracking failed", zap.String("channel", channel), zap.String("domain_ref_id", domainRefID), zap.Error(err))
	}
}

// SendConfirmation envoie la confirmation immédiate à la création d'une
// réservation confirmed.
func (s *Service) SendConfirmation(ctx context.Context, m BookingMessage) {
	s.sendEmail(ctx, m, "Votre réservation est confirmée", "booking_confirmation.html", calendarPublish)
	s.sendSMS(ctx, m, s.smsWithManagementLink(m, fmt.Sprintf(
		"Votre reservation chez %s le %s a %s (%d pers.) est confirmee. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	)))
}

// SendReminder envoie le rappel avant service (J-1 par défaut, cf. cron).
func (s *Service) SendReminder(ctx context.Context, m BookingMessage) {
	s.sendEmail(ctx, m, "Rappel de votre réservation", "booking_reminder.html", noCalendar)
	s.sendSMS(ctx, m, s.smsWithManagementLink(m, fmt.Sprintf(
		"Rappel : reservation chez %s le %s a %s (%d pers.). Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	)))
}

// SendModification notifie une modification de réservation (client ou staff).
func (s *Service) SendModification(ctx context.Context, m BookingMessage) {
	s.sendEmail(ctx, m, "Votre réservation a été modifiée", "booking_modification.html", calendarModify(m))
	s.sendSMS(ctx, m, s.smsWithManagementLink(m, fmt.Sprintf(
		"Votre reservation chez %s a ete modifiee : %s a %s (%d pers.). Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	)))
}

// SendCancellation notifie une annulation (client, staff ou système).
func (s *Service) SendCancellation(ctx context.Context, m BookingMessage) {
	s.sendEmail(ctx, m, "Votre réservation a été annulée", "booking_cancellation.html", calendarCancel)
	s.sendSMS(ctx, m, fmt.Sprintf(
		"Votre reservation chez %s le %s a %s a ete annulee. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.BookingNumber,
	))
}

// SendReconfirmation demande au client de confirmer sa venue (réponse SMS
// OUI/NON traitée par le webhook internal/webhook/brevo_sms_reply).
func (s *Service) SendReconfirmation(ctx context.Context, m BookingMessage) {
	s.sendEmail(ctx, m, "Merci de confirmer votre réservation", "booking_reconfirmation.html", noCalendar)
	s.sendSMS(ctx, m, s.smsWithManagementLink(m, fmt.Sprintf(
		"Confirmez-vous votre reservation chez %s le %s a %s (%d pers.) ? Repondez OUI ou NON. Ref: %s",
		m.MerchantName, m.DateLabel, m.TimeLabel, m.PartySize, m.BookingNumber,
	)))
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
		Brand:         s.brand(ctx, m.MerchantID, m.MerchantName),
		MerchantName:  m.MerchantName,
		CustomerName:  m.CustomerName,
	}
	s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, "Merci de votre visite", "booking_post_visit.html", data)
}

// WaitlistMessage porte les données pour la notification "table disponible"
// envoyée au premier de la liste d'attente.
type WaitlistMessage struct {
	MerchantID    string // établissement à qui le SMS est comptabilisé
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
			Brand:         s.brand(ctx, m.MerchantID, m.MerchantName),
			MerchantName:  m.MerchantName,
			CustomerName:  m.CustomerName,
			PartySize:     m.PartySize,
			ExpiryMinutes: m.ExpiryMinutes,
		}
		if trackedMailer, ok := s.mailer.(asyncMailerWithMessageID); ok {
			trackedMailer.SendAsyncWithMessageID(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, "Une table s'est libérée", "waitlist_available.html", data, func(messageID string) {
				s.recordOutbound(ctx, outbound.ChannelEmail, messageID, "", m.CustomerEmail)
			})
		} else {
			s.mailer.SendAsync(m.MerchantName, mailer.InvoiceEmail, m.CustomerEmail, "Une table s'est libérée", "waitlist_available.html", data)
		}
	}

	if m.SMSEnabled && s.sms != nil && strings.TrimSpace(m.CustomerPhone) != "" {
		text := fmt.Sprintf(
			"Bonne nouvelle ! Une table pour %d personne(s) s'est liberee chez %s. Elle vous est reservee %d min.",
			m.PartySize, m.MerchantName, m.ExpiryMinutes,
		)
		normalizedPhone := helpers.NormalizePhoneNumber(m.CustomerPhone, "FR")
		s.sendSMSText(ctx, m.MerchantID, normalizedPhone, text, func(messageID string) {
			s.recordOutbound(ctx, outbound.ChannelSMS, messageID, "", normalizedPhone)
		})
	}
}

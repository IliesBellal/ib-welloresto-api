package demorequest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
)

// demoRequestIPThrottlePrefix / Max / Window bound POST
// /v1/public/demo-request — a public route with no auth, callable by every
// visitor of the site vitrine's /contact and /tarifs pages, so it gets the
// same per-IP throttle shape as signup-context/password-reset (see
// redisclient.Client.TooManyRequestsFromIP). Lower ceiling than
// signup-context's 30/h: this form has no legitimate reason to be submitted
// often from a single IP.
const (
	demoRequestIPThrottlePrefix = "demorequest:ipthrottle:"
	demoRequestIPThrottleMax    = 10
	demoRequestIPThrottleWindow = time.Hour
)

type Service struct {
	repo              *Repository
	mailer            mailer.Service
	redis             *redisclient.Client
	notificationEmail string
}

func NewService(repo *Repository, mailer mailer.Service, redis *redisclient.Client, notificationEmail string) *Service {
	return &Service{repo: repo, mailer: mailer, redis: redis, notificationEmail: notificationEmail}
}

// SlotGrid handles GET /v1/public/demo-request/slots — TOUTE la grille
// (generateGridSlots), chaque créneau annoté disponible ou non (délai de
// prévenance, affluence simulée — isSlotBookable — ou réellement réservé,
// Repository.BookedSlotsFrom). Contrairement à l'ancienne AvailableSlots, ne
// retranche plus les créneaux indisponibles de la liste : le tableau du site
// vitrine doit pouvoir les afficher grisés (2026-09-25).
func (s *Service) SlotGrid(ctx context.Context) ([]SlotView, error) {
	now := time.Now()
	grid := generateGridSlots(now)

	booked, err := s.repo.BookedSlotsFrom(ctx, now)
	if err != nil {
		return nil, err
	}

	views := make([]SlotView, len(grid))
	for i, slot := range grid {
		available := isSlotBookable(slot, now) && !booked[slot.Unix()]
		views[i] = SlotView{Start: slot.Format(time.RFC3339), Available: available}
	}
	return views, nil
}

// Create handles POST /v1/public/demo-request — replaces the Web3Forms relay
// (données hors UE, écarté sur avis juridique du fondateur, voir
// wello-resto-vitrine/docs/decisions-log.md) par un envoi Brevo interne, et
// réserve désormais un créneau d'appel exact (chantier créneaux engageants,
// 2026-09-24) plutôt qu'une préférence texte : l'index unique partiel de la
// migration 154 est l'unique garde-fou contre une double réservation
// concurrente (voir Repository.CreateBooking) — aucune synchronisation avec
// l'agenda réel du fondateur, un choix assumé (voir decisions-log.md).
func (s *Service) Create(ctx context.Context, clientIP string, req CreateDemoRequestRequest) error {
	if s.redis.TooManyRequestsFromIP(ctx, demoRequestIPThrottlePrefix, clientIP, demoRequestIPThrottleMax, demoRequestIPThrottleWindow) {
		return models.ErrRateLimited
	}

	// Honeypot rempli : un bot a rempli un champ invisible pour un humain —
	// répondre succès sans rien envoyer, jamais révéler la détection.
	if strings.TrimSpace(req.Website) != "" {
		return nil
	}

	establishment := strings.TrimSpace(req.Establishment)
	phone := strings.TrimSpace(req.Phone)
	email := strings.TrimSpace(req.Email)
	if establishment == "" || phone == "" || email == "" {
		return fmt.Errorf("%w: establishment, phone and email are required", models.ErrInvalidInput)
	}

	slotStart, err := time.Parse(time.RFC3339, strings.TrimSpace(req.SlotStart))
	if err != nil {
		return fmt.Errorf("%w: slot_start must be a valid RFC3339 timestamp", models.ErrInvalidInput)
	}

	// Revalidé côté serveur avec l'horloge et les règles serveur — jamais
	// confiance dans ce que le client prétend avoir choisi dans
	// GET .../slots (l'affichage a pu être périmé, ou la requête forgée).
	if !isCandidateSlot(slotStart, time.Now()) {
		return fmt.Errorf("%w: slot_start is not a currently valid slot", models.ErrSlotUnavailable)
	}

	if s.notificationEmail == "" {
		return fmt.Errorf("demorequest: no notification email configured")
	}

	restaurantType := strings.TrimSpace(req.RestaurantType)
	situation := strings.TrimSpace(req.Situation)
	address := strings.TrimSpace(req.EstablishmentAddress)
	slotEnd := slotStart.Add(time.Duration(slotDurationMinutes) * time.Minute)
	utmSource, utmMedium, utmCampaign := cleanUTM(req.UTMSource), cleanUTM(req.UTMMedium), cleanUTM(req.UTMCampaign)

	if err := s.repo.CreateBooking(ctx, Booking{
		SlotStart:            slotStart,
		SlotEnd:              slotEnd,
		Establishment:        establishment,
		EstablishmentAddress: address,
		RestaurantType:       restaurantType,
		Phone:                phone,
		Email:                email,
		Situation:            situation,
		UTMSource:            utmSource,
		UTMMedium:            utmMedium,
		UTMCampaign:          utmCampaign,
	}); err != nil {
		return err
	}

	// Lien Google Agenda + .ics générés UNE fois et réutilisés pour les deux
	// e-mails (2026-09-25, demande du fondateur : l'ajout au calendrier doit
	// être possible aussi bien pour le client que pour le contact interne
	// Wello Resto) — même évènement, deux destinataires.
	summary := fmt.Sprintf("Démo WelloResto — %s", establishment)
	description := fmt.Sprintf("Appel de démonstration WelloResto avec %s. Nous appellerons le %s.", establishment, phone)
	googleCalendarLink := buildGoogleCalendarLink(slotStart, slotEnd, summary, description)
	ics := buildICS(slotStart, slotEnd, summary, description)

	s.sendInternalNotification(establishment, address, restaurantType, phone, situation, formatOrigin(utmSource, utmMedium, utmCampaign), slotStart, googleCalendarLink, ics)
	s.sendConfirmation(email, establishment, phone, slotStart, googleCalendarLink, ics)

	return nil
}

func (s *Service) sendInternalNotification(establishment, address, restaurantType, phone, situation, origin string, slotStart time.Time, googleCalendarLink string, ics []byte) {
	data := mailer.DemoRequestData{
		EmailBaseData:        emailBaseData(),
		Establishment:        establishment,
		EstablishmentAddress: address,
		RestaurantType:       restaurantType,
		Phone:                phone,
		Situation:            situation,
		Origin:               origin,
		Slot:                 fmt.Sprintf("%s à %s", bookingcore.FormatDateLabelFR(slotStart), slotStart.Format("15:04")),
		GoogleCalendarLink:   googleCalendarLink,
	}
	s.mailer.SendAsyncWithAttachment("WelloResto — Site vitrine", mailer.SupportEmail, s.notificationEmail, "Nouvelle demande de démo — site WelloResto", "demo_request.html", data, ics, "rendez-vous-welloresto.ics")
}

func (s *Service) sendConfirmation(email, establishment, phone string, slotStart time.Time, googleCalendarLink string, ics []byte) {
	data := mailer.DemoConfirmationData{
		EmailBaseData:      emailBaseData(),
		Establishment:      establishment,
		DateLabel:          bookingcore.FormatDateLabelFR(slotStart),
		TimeLabel:          slotStart.Format("15:04"),
		Phone:              phone,
		GoogleCalendarLink: googleCalendarLink,
	}
	s.mailer.SendAsyncWithAttachment("Wello Resto", mailer.SupportEmail, email, "Votre rendez-vous WelloResto est confirmé", "demo_confirmation.html", data, ics, "rendez-vous-welloresto.ics")
}

// maxUTMLength borne chaque paramètre utm_* enregistré : ils viennent d'une
// URL que n'importe qui peut forger, et n'ont aucune raison légitime de
// dépasser quelques dizaines de caractères (ex. dpl_sno_brn_2610).
const maxUTMLength = 100

// cleanUTM normalise un paramètre utm_* reçu du site vitrine : espaces de
// bord retirés, caractères de contrôle supprimés (ils finiraient tels quels
// dans l'e-mail interne), longueur bornée à maxUTMLength runes — jamais au
// milieu d'un caractère UTF-8. "" signifie « absent » (NULL en base).
func cleanUTM(v string) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if runes := []rune(v); len(runes) > maxUTMLength {
		v = string(runes[:maxUTMLength])
	}
	return v
}

// formatOrigin compose la ligne « Origine » de l'e-mail interne, ex.
// "depliant · print · dpl_sno_brn_2610" — seulement les paramètres présents,
// "" si aucun (la ligne n'est alors pas affichée, voir demo_request.html).
func formatOrigin(source, medium, campaign string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{source, medium, campaign} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func emailBaseData() mailer.EmailBaseData {
	return mailer.EmailBaseData{
		BrandName:    "Wello Resto",
		Year:         time.Now().Year(),
		SupportEmail: mailer.SupportEmail,
		BrandLogoURL: mailer.BrandLogoURL,
	}
}

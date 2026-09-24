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

	if err := s.repo.CreateBooking(ctx, Booking{
		SlotStart:            slotStart,
		SlotEnd:              slotEnd,
		Establishment:        establishment,
		EstablishmentAddress: address,
		RestaurantType:       restaurantType,
		Phone:                phone,
		Email:                email,
		Situation:            situation,
	}); err != nil {
		return err
	}

	s.sendInternalNotification(establishment, address, restaurantType, phone, situation, slotStart)
	s.sendConfirmation(email, establishment, phone, slotStart, slotEnd)

	return nil
}

func (s *Service) sendInternalNotification(establishment, address, restaurantType, phone, situation string, slotStart time.Time) {
	data := mailer.DemoRequestData{
		EmailBaseData:        emailBaseData(),
		Establishment:        establishment,
		EstablishmentAddress: address,
		RestaurantType:       restaurantType,
		Phone:                phone,
		Situation:            situation,
		Slot:                 fmt.Sprintf("%s à %s", bookingcore.FormatDateLabelFR(slotStart), slotStart.Format("15:04")),
	}
	s.mailer.SendAsync("WelloResto — Site vitrine", mailer.SupportEmail, s.notificationEmail, "Nouvelle demande de démo — site WelloResto", "demo_request.html", data)
}

func (s *Service) sendConfirmation(email, establishment, phone string, slotStart, slotEnd time.Time) {
	summary := fmt.Sprintf("Démo WelloResto — %s", establishment)
	description := fmt.Sprintf("Appel de démonstration WelloResto avec %s. Nous appellerons le %s.", establishment, phone)

	data := mailer.DemoConfirmationData{
		EmailBaseData:      emailBaseData(),
		Establishment:      establishment,
		DateLabel:          bookingcore.FormatDateLabelFR(slotStart),
		TimeLabel:          slotStart.Format("15:04"),
		Phone:              phone,
		GoogleCalendarLink: buildGoogleCalendarLink(slotStart, slotEnd, summary, description),
	}
	ics := buildICS(slotStart, slotEnd, summary, description)
	s.mailer.SendAsyncWithAttachment("Wello Resto", mailer.SupportEmail, email, "Votre rendez-vous WelloResto est confirmé", "demo_confirmation.html", data, ics, "rendez-vous-welloresto.ics")
}

func emailBaseData() mailer.EmailBaseData {
	return mailer.EmailBaseData{
		BrandName:    "Wello Resto",
		Year:         time.Now().Year(),
		SupportEmail: mailer.SupportEmail,
		BrandLogoURL: mailer.BrandLogoURL,
	}
}

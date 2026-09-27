package availabilities

import (
	"context"
	"crypto/md5"
	"fmt"
	"sort"
	"strings"
	"time"
	"welloresto-api/internal/middleware"
)

type AvailabilitiesService struct {
	availabilitiesRepo *AvailabilitiesRepository
}

func NewAvailabilitiesService(repo *AvailabilitiesRepository) *AvailabilitiesService {
	return &AvailabilitiesService{
		availabilitiesRepo: repo,
	}
}

// GetAvailabilitiesByMerchant récupère toutes les disponibilités pour le commerçant connecté
func (s *AvailabilitiesService) GetAvailabilitiesByMerchant(ctx context.Context) ([]Availability, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	availabilities, err := s.availabilitiesRepo.GetAvailabilitiesByMerchant(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	// Créneaux stockés en heure locale du merchant, renvoyés tels quels.
	return availabilities, nil
}

// GetAvailabilityByID récupère une disponibilité spécifique
func (s *AvailabilitiesService) GetAvailabilityByID(ctx context.Context, availabilityID string) (*Availability, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.availabilitiesRepo.GetAvailabilityByID(ctx, user.MerchantID, availabilityID)
}

// CreateAvailability crée une nouvelle disponibilité
func (s *AvailabilitiesService) CreateAvailability(ctx context.Context, req CreateAvailabilityRequest) (*Availability, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validation basique
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("availability name is required")
	}

	if len(req.ProductIDs) == 0 {
		return nil, fmt.Errorf("at least one product is required")
	}

	if len(req.Schedules) == 0 {
		return nil, fmt.Errorf("at least one schedule is required")
	}

	// Valider les créneaux
	if err := validateSchedules(req.Schedules); err != nil {
		return nil, err
	}

	return s.availabilitiesRepo.Create(ctx, user.MerchantID, req)
}

// UpdateAvailability met à jour une disponibilité existante (supporte les mises à jour partielles)
func (s *AvailabilitiesService) UpdateAvailability(ctx context.Context, availabilityID string, req UpdateAvailabilityRequest) (*Availability, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validation : au moins un champ doit être fourni
	if req.Name == nil && req.UnavailableMessage == nil && len(req.ProductIDs) == 0 && len(req.Schedules) == 0 && req.Available == nil {
		return nil, fmt.Errorf("at least one field must be provided for update")
	}

	// Validation conditionnelle : si Name est fourni, il ne doit pas être vide
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return nil, fmt.Errorf("availability name cannot be empty")
	}

	// Validation conditionnelle : si ProductIDs sont fournis, au moins un est requis
	if len(req.ProductIDs) > 0 {
		if len(req.ProductIDs) == 0 {
			return nil, fmt.Errorf("product_ids cannot be empty if provided")
		}
	}

	// Validation conditionnelle : si Schedules sont fournis, au moins un est requis
	if len(req.Schedules) > 0 {
		if len(req.Schedules) == 0 {
			return nil, fmt.Errorf("schedules cannot be empty if provided")
		}
		// Valider les créneaux
		if err := validateSchedules(req.Schedules); err != nil {
			return nil, err
		}
	}

	return s.availabilitiesRepo.Update(ctx, user.MerchantID, availabilityID, req)
}

// DeleteAvailability supprime une disponibilité
func (s *AvailabilitiesService) DeleteAvailability(ctx context.Context, availabilityID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.availabilitiesRepo.Delete(ctx, user.MerchantID, availabilityID)
}

// ============ Logique de Validation ============

// IsProductAvailable vérifie si un produit est disponible à l'heure actuelle.
// Même règle que le filtre des menus Kiosk/ScanNOrder (UnavailableProductsAt).
func (s *AvailabilitiesService) IsProductAvailable(ctx context.Context, merchantID, productID string) (bool, error) {
	return s.IsProductAvailableAt(ctx, merchantID, productID, time.Now())
}

// IsProductAvailableAt vérifie la disponibilité d'un produit à un instant donné.
func (s *AvailabilitiesService) IsProductAvailableAt(ctx context.Context, merchantID, productID string, checkTime time.Time) (bool, error) {
	unavailable, err := s.GetUnavailableProductsAt(ctx, merchantID, checkTime)
	if err != nil {
		return false, fmt.Errorf("failed to check product availability: %w", err)
	}
	_, blocked := unavailable[productID]
	return !blocked, nil
}

// GetUnavailableProductsAt retourne les produits du merchant masqués par une
// disponibilité horaire à l'instant at (product_id → nom). Une seule requête
// quel que soit le nombre de produits — utilisé par les menus, fiches produit,
// upsell et pricing des canaux Kiosk et ScanNOrder (pas le POS).
func (s *AvailabilitiesService) GetUnavailableProductsAt(ctx context.Context, merchantID string, at time.Time) (map[string]string, error) {
	rows, err := s.availabilitiesRepo.GetActiveProductSchedules(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	// Fuseau lu seulement si un produit est restreint (cas minoritaire).
	if len(rows) == 0 {
		return map[string]string{}, nil
	}
	timeZone, err := s.availabilitiesRepo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return UnavailableProductsAt(rows, at, merchantLocation(timeZone)), nil
}

// UnavailableProductsAt applique la règle « liste blanche » des disponibilités :
//   - un produit rattaché à aucune disponibilité active n'est jamais restreint
//     (il n'apparaît simplement pas dans rows) ;
//   - un produit rattaché à au moins une disponibilité active n'est disponible
//     que si at tombe dans au moins un de ses créneaux actifs.
//
// Les créneaux sont des heures de mur du merchant (« 6h–11h le lundi », été
// comme hiver) : at est évalué dans loc, le fuseau du merchant.
func UnavailableProductsAt(rows []ProductScheduleRow, at time.Time, loc *time.Location) map[string]string {
	if loc == nil {
		loc = time.UTC
	}
	atLocal := at.In(loc)
	restricted := make(map[string]string)
	open := make(map[string]bool)
	for _, row := range rows {
		restricted[row.ProductID] = row.ProductName
		if row.HasSchedule && isScheduleOpenAt(row.DayOfWeek, row.StartTime, row.EndTime, atLocal) {
			open[row.ProductID] = true
		}
	}

	unavailable := make(map[string]string)
	for productID, name := range restricted {
		if !open[productID] {
			unavailable[productID] = name
		}
	}
	return unavailable
}

// isScheduleOpenAt teste un créneau [start, end[ du jour day, en heure locale
// (atLocal déjà exprimé dans le fuseau du merchant). Une fin à 00:00 signifie
// « jusqu'à minuit » (ex. 19:00–00:00) ; validateSchedules refuse tout autre
// créneau à l'envers : un créneau ne déborde jamais sur le lendemain.
func isScheduleOpenAt(day int, start, end string, atLocal time.Time) bool {
	start = normalizeTime(strings.TrimSpace(start))
	end = normalizeTime(strings.TrimSpace(end))
	if end == midnight {
		end = "24:00:00"
	}
	clock := atLocal.Format("15:04:05")
	return getDayOfWeek(atLocal) == day && clock >= start && clock < end
}

const midnight = "00:00:00"

// UnavailabilityFingerprint résume un ensemble de produits indisponibles en
// une clé stable (indépendante de l'ordre de la map), à suffixer aux clés de
// cache des menus : le menu mis en cache reste ainsi exact à chaque
// changement de créneau, sans dépendre du TTL ni d'une invalidation active.
func UnavailabilityFingerprint(unavailable map[string]string) string {
	if len(unavailable) == 0 {
		return "all"
	}
	ids := make([]string, 0, len(unavailable))
	for id := range unavailable {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(ids, ","))))
}

// ============ Helper Functions ============

// getDayOfWeek retourne le jour de la semaine (1 = lundi, ..., 7 = dimanche)
func getDayOfWeek(t time.Time) int {
	weekday := t.Weekday()
	// Go Weekday renvoie 0 pour dimanche
	// Standard 1-7: 1 = lundi, ..., 7 = dimanche
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}

// validateSchedules valide les créneaux horaires. Le dimanche envoyé en 0
// (ancienne convention JS du back-office) est ramené à 7 (ISO, convention
// d'évaluation) : il était refusé, une disponibilité le dimanche ne pouvait
// pas être enregistrée. Fin à 00:00 = jusqu'à minuit.
func validateSchedules(schedules []CreateAvailabilityScheduleReq) error {
	for i := range schedules {
		if schedules[i].DayOfWeek == 0 {
			schedules[i].DayOfWeek = 7
		}
	}
	for i, schedule := range schedules {
		// Valider le jour de la semaine (1-7)
		if schedule.DayOfWeek < 1 || schedule.DayOfWeek > 7 {
			return fmt.Errorf("invalid day_of_week at schedule %d: must be between 1 and 7", i)
		}

		// Normaliser et valider les heures
		startTime := normalizeTime(schedule.StartTime)
		endTime := normalizeTime(schedule.EndTime)

		if !isValidTimeFormat(startTime) {
			return fmt.Errorf("invalid start_time format at schedule %d: must be HH:MM or HH:MM:SS", i)
		}

		if !isValidTimeFormat(endTime) {
			return fmt.Errorf("invalid end_time format at schedule %d: must be HH:MM or HH:MM:SS", i)
		}

		if startTime >= endTime && endTime != midnight {
			return fmt.Errorf("invalid time range at schedule %d: start_time must be before end_time (use 00:00 as end for midnight)", i)
		}
	}

	return nil
}

// merchantLocation charge le fuseau du merchant, UTC en repli.
func merchantLocation(timeZone string) *time.Location {
	loc, err := time.LoadLocation(strings.TrimSpace(timeZone))
	if err != nil {
		return time.UTC
	}
	return loc
}

// isValidTimeFormat vérifie si une chaîne est au format HH:MM:SS valide
func isValidTimeFormat(timeStr string) bool {
	if len(timeStr) != 8 {
		return false
	}

	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return false
	}

	var hours, minutes, seconds int
	_, err := fmt.Sscanf(timeStr, "%d:%d:%d", &hours, &minutes, &seconds)
	if err != nil {
		return false
	}

	if hours < 0 || hours > 23 {
		return false
	}
	if minutes < 0 || minutes > 59 {
		return false
	}
	if seconds < 0 || seconds > 59 {
		return false
	}

	return true
}

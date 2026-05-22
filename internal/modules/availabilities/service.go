package availabilities

import (
	"context"
	"fmt"
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

	return s.availabilitiesRepo.GetAvailabilitiesByMerchant(ctx, user.MerchantID)
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

// IsProductAvailable vérifie si un produit est disponible à l'heure actuelle
// Retourne true si:
// - Aucune disponibilité n'est définie pour ce produit (par défaut disponible)
// - Au moins une disponibilité active correspond à l'heure et au jour actuels
func (s *AvailabilitiesService) IsProductAvailable(ctx context.Context, merchantID, productID string) (bool, error) {
	// Récupérer les disponibilités pour ce produit
	availabilities, err := s.availabilitiesRepo.GetAvailabilitiesForProduct(ctx, merchantID, productID)
	if err != nil {
		return false, fmt.Errorf("failed to check product availability: %w", err)
	}

	// Si aucune disponibilité n'est définie, le produit est disponible par défaut
	if len(availabilities) == 0 {
		return true, nil
	}

	// Vérifier si l'heure et le jour actuels correspondent à au moins une disponibilité
	now := time.Now().UTC()
	currentTime := now.Format("15:04:05")
	currentDayOfWeek := getDayOfWeek(now)

	for _, availability := range availabilities {
		for _, schedule := range availability.Schedules {
			// Vérifier le jour de la semaine
			if schedule.DayOfWeek != currentDayOfWeek {
				continue
			}

			// Vérifier l'heure (comparaison en string format HH:MM:SS)
			if currentTime >= schedule.StartTime && currentTime <= schedule.EndTime {
				return true, nil
			}
		}
	}

	// Aucune disponibilité ne correspond à l'heure actuelle
	return false, nil
}

// IsProductAvailableAt vérifie la disponibilité d'un produit à une heure spécifique
// Utile pour les tests ou les calculs futures
func (s *AvailabilitiesService) IsProductAvailableAt(ctx context.Context, merchantID, productID string, checkTime time.Time) (bool, error) {
	availabilities, err := s.availabilitiesRepo.GetAvailabilitiesForProduct(ctx, merchantID, productID)
	if err != nil {
		return false, fmt.Errorf("failed to check product availability: %w", err)
	}

	if len(availabilities) == 0 {
		return true, nil
	}

	timeStr := checkTime.Format("15:04:05")
	dayOfWeek := getDayOfWeek(checkTime)

	for _, availability := range availabilities {
		for _, schedule := range availability.Schedules {
			if schedule.DayOfWeek != dayOfWeek {
				continue
			}

			if timeStr >= schedule.StartTime && timeStr <= schedule.EndTime {
				return true, nil
			}
		}
	}

	return false, nil
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

// validateSchedules valide les créneaux horaires
func validateSchedules(schedules []CreateAvailabilityScheduleReq) error {
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

		if startTime >= endTime {
			return fmt.Errorf("invalid time range at schedule %d: start_time must be before end_time", i)
		}
	}

	return nil
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

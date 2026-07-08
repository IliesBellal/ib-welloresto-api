package bookings

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

type BookingsService struct {
	repo   *BookingsRepository
	db     *sql.DB // Ajout d'une référence à la DB pour les transactions
	mailer mailer.Service
	sms    sms.Service
	events *bookingevents.Repository
	log    *zap.Logger
}

func NewBookingsService(
	repo *BookingsRepository,
	db *sql.DB,
	mail mailer.Service,
	smsSvc sms.Service,
	events *bookingevents.Repository,
	log *zap.Logger,
) *BookingsService {
	return &BookingsService{repo: repo, db: db, mailer: mail, sms: smsSvc, events: events, log: log}
}

func (s *BookingsService) GetBookings(ctx context.Context, token string, req *BookingObjectRequest) ([]Booking, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.MerchantID = user.MerchantID

	return s.repo.GetBookings(ctx, req)
}

func (s *BookingsService) GetBookingByID(ctx context.Context, token, bookingID string) (*Booking, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
}

func (s *BookingsService) CreateBooking(ctx context.Context, req *BookingObjectRequest) (*Booking, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.MerchantID = user.MerchantID

	// Variable pour stocker le résultat final hors de la closure
	var result *Booking

	// 2️⃣ Lancement de la transaction
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {

		// Contrôle de conflit table × créneau : les affectations en collision
		// sont verrouillées (FOR UPDATE) jusqu'au commit/rollback.
		if len(req.Booking.Locations) > 0 {
			locationIDs := make([]string, 0, len(req.Booking.Locations))
			for _, loc := range req.Booking.Locations {
				locationIDs = append(locationIDs, loc.LocationID)
			}

			conflicts, err := s.repo.FindConflictingBookings(
				txCtx, req.MerchantID, locationIDs,
				req.Booking.StartDate, req.Booking.EndDate, "",
			)
			if err != nil {
				return fmt.Errorf("repo.FindConflictingBookings failed: %w", err)
			}
			if len(conflicts) > 0 {
				return &TableConflictError{Conflicts: conflicts}
			}
		}

		// 3️⃣ Création du booking (utilise le txCtx pour propager la transaction)
		bookingID, err := s.repo.CreateBooking(txCtx, req)
		if err != nil {
			return fmt.Errorf("repo.CreateBooking failed: %w", err)
		}

		// 4️⃣ Rechargement du booking complet
		// On le fait à l'intérieur de la transaction pour être sûr de lire
		// ce qu'on vient d'écrire (isolation)
		result, err = s.repo.GetBookingByID(txCtx, req.MerchantID, bookingID)
		if err != nil {
			return fmt.Errorf("repo.GetBookingByID failed: %w", err)
		}

		return nil // Si tout est OK, RunInTx fera le Commit
	})

	// Si err != nil ici, RunInTx a déjà fait le Rollback
	if err != nil {
		return nil, err
	}

	// 5️⃣ Optionnel : Envoi d'email (Hors transaction car c'est un effet de bord externe)
	// s.sendBookingConfirmation(result)

	return result, nil
}

func (s *BookingsService) AcceptBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusConfirmed); err != nil {
		return nil, models.ErrInvalidInput
	}

	// 1️⃣ Update booking state
	err = s.repo.SetBookingState(ctx, user.MerchantID, bookingID, bookingcore.StatusConfirmed)
	if err != nil {
		return nil, err
	}

	// 2️⃣ Reload booking (fetchAndBuildBookings)
	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	// 3️⃣ Email pending — ignored for now

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

func (s *BookingsService) DenyBooking(ctx context.Context, token, bookingID string, req *DenyBookingRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusDenied); err != nil {
		return nil, models.ErrInvalidInput
	}

	if req != nil && req.DeletionReasonID != nil {
		ok, err := s.repo.IsValidDeletionReason(ctx, *req.DeletionReasonID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, models.ErrInvalidInput
		}
	}

	err = s.repo.DenyBooking(ctx, user.MerchantID, bookingID, user.UserID, req)
	if err != nil {
		return nil, err
	}

	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

func (s *BookingsService) AssignBookingLocations(ctx context.Context, token, bookingID string, req *AssignBookingLocationsRequest) (*Booking, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, models.ErrInvalidInput
	}

	var result *Booking
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		booking, err := s.repo.GetBookingByID(txCtx, user.MerchantID, bookingID)
		if err != nil {
			return err
		}

		locationIDs := make([]string, 0, len(req.Locations))
		for _, loc := range req.Locations {
			locationIDs = append(locationIDs, loc.LocationID)
		}

		conflicts, err := s.repo.FindConflictingBookings(
			txCtx,
			user.MerchantID,
			locationIDs,
			booking.StartDate,
			booking.EndDate,
			bookingID,
		)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return &TableConflictError{Conflicts: conflicts}
		}

		if err := s.repo.ReplaceBookingLocations(txCtx, user.MerchantID, bookingID, locationIDs); err != nil {
			return err
		}

		result, err = s.repo.GetBookingByID(txCtx, user.MerchantID, bookingID)
		return err
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *BookingsService) GetBookingAvailability(ctx context.Context, token, date string) (*BookingAvailabilityResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetBookingAvailability(ctx, user.MerchantID, date)
}

func (s *BookingsService) GetBookingSettings(ctx context.Context, token string) (*BookingSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetBookingSettings(ctx, user.MerchantID)
}

func (s *BookingsService) PutBookingSettings(ctx context.Context, token string, req *PutBookingSettingsRequest) (*BookingSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, models.ErrInvalidInput
	}

	if req.MinBookingNoticeMinutes < 0 {
		return nil, models.ErrInvalidInput
	}
	if req.MaxBookingHorizonDays < 1 {
		return nil, models.ErrInvalidInput
	}
	if req.OverbookingPercent < 0 || req.OverbookingPercent > 100 {
		return nil, models.ErrInvalidInput
	}
	if req.ReserveMinimumPartySize > req.ReserveMaximumPartySize {
		return nil, models.ErrInvalidInput
	}
	if req.WaitlistMaxSize < 0 {
		return nil, models.ErrInvalidInput
	}
	if req.WaitlistSlotExpiryMinutes < 0 {
		return nil, models.ErrInvalidInput
	}

	if err := s.repo.UpsertBookingSettings(ctx, user.MerchantID, req); err != nil {
		return nil, err
	}

	return s.repo.GetBookingSettings(ctx, user.MerchantID)
}

func (s *BookingsService) ListBookingDurationRules(ctx context.Context, token string) ([]BookingDurationRule, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.ListBookingDurationRules(ctx, user.MerchantID)
}

func (s *BookingsService) CreateBookingDurationRule(ctx context.Context, token string, req CreateDurationRuleRequest) (*BookingDurationRule, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if req.MinPartySize <= 0 || req.MaxPartySize <= 0 || req.DurationMinutes <= 0 || req.MinPartySize > req.MaxPartySize {
		return nil, models.ErrInvalidInput
	}

	rules, err := s.repo.ListBookingDurationRules(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if hasRuleOverlap(rules, req.MinPartySize, req.MaxPartySize, "") {
		return nil, models.ErrInvalidInput
	}

	return s.repo.CreateBookingDurationRule(ctx, user.MerchantID, req)
}

func (s *BookingsService) UpdateBookingDurationRule(ctx context.Context, token, ruleID string, req PatchDurationRuleRequest) (*BookingDurationRule, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := s.repo.GetBookingDurationRuleByID(ctx, user.MerchantID, ruleID)
	if err != nil {
		return nil, err
	}

	minPartySize := existing.MinPartySize
	maxPartySize := existing.MaxPartySize
	duration := existing.DurationMinutes

	if req.MinPartySize != nil {
		minPartySize = *req.MinPartySize
	}
	if req.MaxPartySize != nil {
		maxPartySize = *req.MaxPartySize
	}
	if req.DurationMinutes != nil {
		duration = *req.DurationMinutes
	}

	if minPartySize <= 0 || maxPartySize <= 0 || duration <= 0 || minPartySize > maxPartySize {
		return nil, models.ErrInvalidInput
	}

	rules, err := s.repo.ListBookingDurationRules(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if hasRuleOverlap(rules, minPartySize, maxPartySize, ruleID) {
		return nil, models.ErrInvalidInput
	}

	return s.repo.UpdateBookingDurationRule(ctx, user.MerchantID, ruleID, req)
}

func (s *BookingsService) DeleteBookingDurationRule(ctx context.Context, token, ruleID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	return s.repo.DeleteBookingDurationRule(ctx, user.MerchantID, ruleID)
}

func (s *BookingsService) GetBookingHours(ctx context.Context, token string) ([]models.POSHoursOfOperation, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.ListBookingHours(ctx, user.MerchantID)
}

func (s *BookingsService) PutBookingHours(ctx context.Context, token string, req *PutBookingSettingsHoursRequest) ([]models.POSHoursOfOperation, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, models.ErrInvalidInput
	}

	for _, h := range req.Hours {
		if h.DayOfWeekFrom < 1 || h.DayOfWeekFrom > 7 || h.DayOfWeekTo < 1 || h.DayOfWeekTo > 7 {
			return nil, models.ErrInvalidInput
		}
		if strings.TrimSpace(h.HourFrom) == "" || strings.TrimSpace(h.HourTo) == "" {
			return nil, models.ErrInvalidInput
		}
		if h.BookingCapacity != nil && *h.BookingCapacity < 0 {
			return nil, models.ErrInvalidInput
		}
		if h.FirstBookingTime != nil && h.LastBookingTime != nil {
			first, err1 := time.Parse("15:04:05", strings.TrimSpace(*h.FirstBookingTime))
			last, err2 := time.Parse("15:04:05", strings.TrimSpace(*h.LastBookingTime))
			if err1 != nil || err2 != nil || !first.Before(last) {
				return nil, models.ErrInvalidInput
			}
		}
	}

	if err := s.repo.ReplaceBookingHours(ctx, user.MerchantID, req.Hours); err != nil {
		return nil, err
	}

	return s.repo.ListBookingHours(ctx, user.MerchantID)
}

func (s *BookingsService) ListBookingsBackOffice(ctx context.Context, token string, filters BookingListFilters) (*BookingListResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.Limit <= 0 {
		filters.Limit = 20
	}
	if filters.Limit > 100 {
		filters.Limit = 100
	}
	if filters.SortBy == "" {
		filters.SortBy = "booking_date_from"
	}
	if filters.SortDir == "" {
		filters.SortDir = "desc"
	}

	items, totalItems, err := s.repo.ListBookingsBackOffice(ctx, user.MerchantID, filters)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + filters.Limit - 1) / filters.Limit
	}

	return &BookingListResponse{
		Metadata: models.PaginationMetadata{
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			CurrentPage: filters.Page,
			Limit:       filters.Limit,
		},
		Bookings: items,
	}, nil
}

func hasRuleOverlap(rules []BookingDurationRule, minPartySize, maxPartySize int, excludeRuleID string) bool {
	sorted := append([]BookingDurationRule(nil), rules...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].MinPartySize == sorted[j].MinPartySize {
			return sorted[i].MaxPartySize < sorted[j].MaxPartySize
		}
		return sorted[i].MinPartySize < sorted[j].MinPartySize
	})

	for _, rule := range sorted {
		if rule.RuleID == excludeRuleID {
			continue
		}
		if minPartySize <= rule.MaxPartySize && maxPartySize >= rule.MinPartySize {
			return true
		}
	}

	return false
}

func (s *BookingsService) ExpirePendingBookings(ctx context.Context) (int64, error) {
	return s.repo.ExpirePendingBookings(ctx)
}

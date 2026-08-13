package bookings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookingevents"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

var ErrHoursRequired = errors.New("hours_required")

type BookingsService struct {
	repo     *BookingsRepository
	db       *sql.DB // Ajout d'une référence à la DB pour les transactions
	mailer   mailer.Service
	sms      sms.Service
	events   *bookingevents.Repository
	notifier *notification.NotificationService
	comm     *bookingcomm.Service
	log      *zap.Logger
}

func NewBookingsService(
	repo *BookingsRepository,
	db *sql.DB,
	mail mailer.Service,
	smsSvc sms.Service,
	events *bookingevents.Repository,
	notifier *notification.NotificationService,
	comm *bookingcomm.Service,
	log *zap.Logger,
) *BookingsService {
	return &BookingsService{repo: repo, db: db, mailer: mail, sms: smsSvc, events: events, notifier: notifier, comm: comm, log: log}
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
	// created_by est dérivé de l'utilisateur authentifié, pas de la valeur
	// envoyée par le front (qui ne doit pas pouvoir l'usurper).
	req.CreatedBy = user.UserID

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

	// 5️⃣ Envoi de la confirmation (hors transaction, effet de bord externe) si
	// la réservation créée par le staff est immédiatement confirmed.
	if bookingcore.NormalizeLegacyStatus(result.Status) == bookingcore.StatusConfirmed {
		s.notifyBookingMessage(ctx, req.MerchantID, result, s.comm.SendConfirmation)
	}

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
	// Capturé avant la bascule : CanTransition autorise le no-op
	// confirmed -> confirmed (f == t), qui ne doit pas redéclencher la
	// confirmation déjà envoyée lors du vrai passage pending -> confirmed.
	previousStatus := bookingcore.NormalizeLegacyStatus(booking.Status)

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

	// 3️⃣ Confirmation au client (staff a accepté une demande pending) —
	// uniquement sur une vraie transition, pas sur le no-op déjà-confirmed.
	if previousStatus == bookingcore.StatusPending {
		s.notifyBookingMessage(ctx, user.MerchantID, booking, s.comm.SendConfirmation)
	}

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
	// Capturé avant la bascule : le no-op denied -> denied (f == t) ne doit
	// pas redéclencher l'annulation déjà envoyée lors du premier refus.
	previousStatus := bookingcore.NormalizeLegacyStatus(booking.Status)

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

	// Le refus d'une demande pending vaut annulation côté client — uniquement
	// sur une vraie transition, pas sur le no-op déjà-denied.
	if previousStatus == bookingcore.StatusPending {
		s.notifyBookingMessage(ctx, user.MerchantID, booking, s.comm.SendCancellation)
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

// CancelBooking annule staff une résa confirmed|seated. Distincte de
// DenyBooking, qui couvre uniquement les demandes pending (addendum §7.9).
func (s *BookingsService) CancelBooking(ctx context.Context, token, bookingID string, req *CancelBookingRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	if err := bookingcore.CanTransition(booking.Status, bookingcore.StatusCancelled); err != nil {
		return nil, models.ErrInvalidInput
	}
	// Capturé avant la bascule : CanTransition autorise le no-op
	// cancelled -> cancelled (f == t) — confirmed et seated sont tous deux
	// des origines valides pour une vraie annulation, donc on garde le
	// déclenchement sur "n'était pas déjà cancelled" plutôt que sur une
	// valeur d'origine unique.
	previousStatus := bookingcore.NormalizeLegacyStatus(booking.Status)

	if req != nil && req.DeletionReasonID != nil {
		ok, err := s.repo.IsValidDeletionReason(ctx, *req.DeletionReasonID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, models.ErrInvalidInput
		}
	}

	if err := s.repo.CancelBooking(ctx, user.MerchantID, bookingID, user.UserID, req); err != nil {
		return nil, err
	}

	if s.events != nil {
		metadata := map[string]interface{}{}
		if req != nil && req.DeletionReasonID != nil {
			metadata["deletion_reason_id"] = *req.DeletionReasonID
		}
		if err := s.events.Log(ctx, bookingevents.Event{
			MerchantID: user.MerchantID,
			BookingID:  bookingID,
			EventType:  bookingevents.TypeBookingCancelled,
			Source:     bookingevents.SourcePOS,
			Actor:      user.UserID,
			Metadata:   metadata,
		}); err != nil && s.log != nil {
			s.log.Warn("booking cancel event log failed", zap.Error(err))
		}
	}

	s.notifyPOS(user.MerchantID, bookingID, notification.NotificationTypeUpdateBooking)

	booking, err = s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	// Annulation notifiée uniquement sur une vraie transition, pas sur le
	// no-op déjà-cancelled.
	if previousStatus != bookingcore.StatusCancelled {
		s.notifyBookingMessage(ctx, user.MerchantID, booking, s.comm.SendCancellation)
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

// RescheduleBooking modifie staff la date/heure (et éventuellement le nombre
// de couverts, la note et le client) d'une résa pending|confirmed, en
// revalidant la disponibilité via le moteur unifié (booking exclue de son
// propre calcul d'occupation). Les autres statuts (seated/completed/
// cancelled/denied/no_show) sont verrouillés côté staff.
func (s *BookingsService) RescheduleBooking(ctx context.Context, token, bookingID string, req *RescheduleBookingRequest) (map[string]interface{}, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.BookingDateFrom) == "" || strings.TrimSpace(req.BookingDateTo) == "" {
		return nil, models.ErrInvalidInput
	}
	if req.PartySize != nil && *req.PartySize <= 0 {
		return nil, models.ErrInvalidInput
	}

	var result *Booking
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		booking, err := s.repo.GetBookingByID(txCtx, user.MerchantID, bookingID)
		if err != nil {
			return err
		}

		normalizedStatus := bookingcore.NormalizeLegacyStatus(booking.Status)
		if normalizedStatus != bookingcore.StatusPending && normalizedStatus != bookingcore.StatusConfirmed {
			return models.ErrInvalidInput
		}

		partySize := booking.PartySize
		if req.PartySize != nil {
			partySize = *req.PartySize
		}

		if len(booking.Locations) > 0 {
			locationIDs := make([]string, 0, len(booking.Locations))
			for _, loc := range booking.Locations {
				locationIDs = append(locationIDs, loc.LocationID)
			}

			conflicts, err := s.repo.FindConflictingBookings(
				txCtx, user.MerchantID, locationIDs,
				req.BookingDateFrom, req.BookingDateTo, bookingID,
			)
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return &TableConflictError{Conflicts: conflicts}
			}
		}

		available, err := s.repo.CheckCapacityForWindow(txCtx, user.MerchantID, req.BookingDateFrom, req.BookingDateTo, partySize, bookingID)
		if err != nil {
			return err
		}
		if !available {
			return models.ErrSlotUnavailable
		}

		if err := s.repo.RescheduleBooking(txCtx, user.MerchantID, bookingID, req.BookingDateFrom, req.BookingDateTo, req.PartySize, req.Comment, req.Customer); err != nil {
			return err
		}

		if s.events != nil {
			if err := s.events.Log(txCtx, bookingevents.Event{
				MerchantID: user.MerchantID,
				BookingID:  bookingID,
				EventType:  bookingevents.TypeBookingModified,
				Source:     bookingevents.SourcePOS,
				Actor:      user.UserID,
				Metadata: map[string]interface{}{
					"booking_date_from": req.BookingDateFrom,
					"booking_date_to":   req.BookingDateTo,
					"party_size":        partySize,
				},
			}); err != nil && s.log != nil {
				s.log.Warn("booking reschedule event log failed", zap.Error(err))
			}
		}

		result, err = s.repo.GetBookingByID(txCtx, user.MerchantID, bookingID)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.notifyPOS(user.MerchantID, bookingID, notification.NotificationTypeUpdateBooking)
	s.notifyBookingMessage(ctx, user.MerchantID, result, s.comm.SendModification)

	return map[string]interface{}{
		"status":  "1",
		"booking": result,
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
	if req.Hours == nil || len(req.Hours) == 0 {
		if s.log != nil {
			s.log.Warn("rejecting empty booking hours payload", zap.String("merchant_id", user.MerchantID), zap.String("error", "hours_required"))
		}
		return nil, ErrHoursRequired
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

// ExpirePendingBookings bascule les réservations pending expirées en
// cancelled. Les clients concernés sont notifiés (annulation système) avant
// la bascule de statut, sur la base du même prédicat de sélection.
func (s *BookingsService) ExpirePendingBookings(ctx context.Context) (int64, error) {
	toExpire, err := s.repo.ListPendingBookingsToExpire(ctx)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := s.repo.ExpirePendingBookings(ctx)
	if err != nil {
		return 0, err
	}

	for _, b := range toExpire {
		loc, err := time.LoadLocation(b.Timezone)
		if err != nil || loc == nil {
			loc = time.UTC
		}
		start, err := time.Parse("2006-01-02 15:04:05", b.StartDate)
		if err != nil {
			continue
		}
		start = start.In(loc)

		if s.comm != nil && (strings.TrimSpace(b.CustomerEmail) != "" || strings.TrimSpace(b.CustomerPhone) != "") {
			s.comm.SendCancellation(ctx, bookingcomm.BookingMessage{
				BookingID:     b.BookingID,
				MerchantSlug:  b.MerchantSlug,
				MerchantName:  b.MerchantName,
				CustomerName:  b.CustomerName,
				CustomerEmail: b.CustomerEmail,
				CustomerPhone: b.CustomerPhone,
				BookingNumber: b.BookingNumber,
				DateLabel:     bookingcore.FormatDateLabelFR(start),
				TimeLabel:     start.Format("15:04"),
				PartySize:     b.PartySize,
				SMSEnabled:    b.SMSEnabled,
			})
		}

		if s.events != nil {
			_ = s.events.Log(ctx, bookingevents.Event{
				MerchantID: b.MerchantID,
				BookingID:  b.BookingID,
				EventType:  bookingevents.TypeBookingCancelled,
				Source:     bookingevents.SourceSystem,
				Actor:      "SYSTEM",
				Metadata:   map[string]interface{}{"reason": "pending_expired"},
			})
		}
	}

	return rowsAffected, nil
}

// notifyPOS pousse un événement temps réel (WebSocket + FCM) vers le POS du
// marchand via le canal existant. No-op si le service de notification n'est
// pas câblé. Non bloquant (goroutine interne à SendNotificationAsync).
func (s *BookingsService) notifyPOS(merchantID, entityID, nType string) {
	if s.notifier == nil {
		return
	}
	_ = s.notifier.SendNotificationAsync(merchantID, entityID, nType)
}

package bookings

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/utils/dbutils"
)

type BookingsService struct {
	repo *BookingsRepository
	db   *sql.DB // Ajout d'une référence à la DB pour les transactions
}

func NewBookingsService(repo *BookingsRepository, db *sql.DB) *BookingsService {
	return &BookingsService{repo: repo, db: db}
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

func (s *BookingsService) ExpirePendingBookings(ctx context.Context) (int64, error) {
	return s.repo.ExpirePendingBookings(ctx)
}

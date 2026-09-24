package demorequest

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"welloresto-api/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Booking est ce que Service.Create insère après revalidation du créneau.
type Booking struct {
	SlotStart            time.Time
	SlotEnd              time.Time
	Establishment        string
	EstablishmentAddress string
	RestaurantType       string
	Phone                string
	Email                string
	Situation            string
}

// BookedSlotsFrom retourne l'ensemble des créneaux actifs (non annulés) à
// partir de `from`, indexés par timestamp Unix — utilisé pour retrancher les
// créneaux déjà pris de generateCandidateSlots (Service.AvailableSlots).
func (r *Repository) BookedSlotsFrom(ctx context.Context, from time.Time) (map[int64]bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT slot_start FROM demo_bookings
		WHERE cancelled_at IS NULL AND slot_start >= $1
	`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	booked := make(map[int64]bool)
	for rows.Next() {
		var slotStart time.Time
		if err := rows.Scan(&slotStart); err != nil {
			return nil, err
		}
		booked[slotStart.Unix()] = true
	}
	return booked, rows.Err()
}

// CreateBooking insère la réservation — l'index unique partiel
// idx_demo_bookings_slot_start_active (migration 154) est l'unique garde-fou
// contre une double réservation concurrente du même créneau : pas besoin de
// transaction ni de `SELECT ... FOR UPDATE` ici, contrairement au module
// bookings (réservation de table), parce qu'il s'agit d'une égalité stricte
// sur un instant fixe, pas d'une détection de chevauchement de plages.
func (r *Repository) CreateBooking(ctx context.Context, b Booking) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO demo_bookings (slot_start, slot_end, establishment, establishment_address, restaurant_type, phone, email, situation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, b.SlotStart, b.SlotEnd, b.Establishment, nullIfEmpty(b.EstablishmentAddress), b.RestaurantType, b.Phone, b.Email, b.Situation)
	if err != nil {
		if isSlotUniqueViolation(err) {
			return models.ErrSlotUnavailable
		}
		return err
	}
	return nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// isSlotUniqueViolation reports whether err is a unique-violation on
// demo_bookings — la seule contrainte unique de cette table (migration 154)
// est idx_demo_bookings_slot_start_active, donc le code d'erreur seul suffit
// à l'identifier sans dépendre du nom exact de l'index.
func isSlotUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Package bookingevents journalise les événements du cycle de vie des
// réservations et de la liste d'attente dans la table booking_events
// (migration 059). Il est volontairement minimal et partagé par les modules
// bookings et reservation pour éviter les imports croisés.
package bookingevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

// Types d'événements journalisés.
const (
	TypeNoShow            = "no_show"
	TypeWaitlistJoined    = "waitlist_joined"
	TypeWaitlistNotified  = "waitlist_notified"
	TypeWaitlistSeated    = "waitlist_seated"
	TypeWaitlistCancelled = "waitlist_cancelled"
	TypeWaitlistExpired   = "waitlist_expired"
	TypeSMSReconfirmed    = "sms_reconfirmed"
	TypeSMSCancelled      = "sms_cancelled"
	TypeBookingCancelled  = "booking_cancelled"
	TypeBookingModified   = "booking_modified"
	TypeBookingSeated     = "booking_seated"
	TypeBookingCompleted  = "booking_completed"
	TypeBookingReminder   = "booking_reminder_sent"
)

// Sources d'un événement.
const (
	SourcePOS    = "pos"
	SourceSystem = "system"
	SourceSMS    = "sms"
	SourcePublic = "public"
)

// Event décrit une entrée du journal. BookingID et WaitlistID sont optionnels
// (l'un ou l'autre selon le contexte). Metadata est sérialisé en JSON.
type Event struct {
	MerchantID string
	BookingID  string // "" => NULL ; sinon converti en INT (bookings.booking_id)
	WaitlistID string // "" => NULL
	EventType  string
	Source     string
	Actor      string
	Metadata   map[string]interface{}
}

// Repository écrit dans booking_events.
type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// Log insère un événement. Best-effort côté appelant : l'erreur est retournée
// mais ne doit pas faire échouer l'action métier principale.
func (r *Repository) Log(ctx context.Context, e Event) error {
	db := dbutils.GetDB(ctx, r.database)

	var bookingID interface{}
	if strings.TrimSpace(e.BookingID) != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(e.BookingID), 10, 64); err == nil {
			bookingID = n
		}
	}

	var waitlistID interface{}
	if strings.TrimSpace(e.WaitlistID) != "" {
		waitlistID = strings.TrimSpace(e.WaitlistID)
	}

	var actor interface{}
	if strings.TrimSpace(e.Actor) != "" {
		actor = strings.TrimSpace(e.Actor)
	}

	var source interface{}
	if strings.TrimSpace(e.Source) != "" {
		source = strings.TrimSpace(e.Source)
	}

	var metadata interface{}
	if len(e.Metadata) > 0 {
		if raw, err := json.Marshal(e.Metadata); err == nil {
			metadata = string(raw)
		}
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO booking_events (
			id, merchant_id, booking_id, waitlist_id, event_type, source, actor, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		helpers.GeneratePrefixedID("bev"),
		e.MerchantID,
		bookingID,
		waitlistID,
		e.EventType,
		source,
		actor,
		metadata,
	)
	return err
}

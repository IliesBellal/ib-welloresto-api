package kiosk

// Mesure d'audience anonyme des bornes : POST /kiosk/events. Table :
// migration 195. Règles : docs/mesure-audience.md (D27 à D33).
//
// La borne envoie en tâche de fond, par lots, sans jamais attendre la réponse
// pour continuer son parcours. Côté API, un événement invalide est ignoré
// seul, sans refuser le lot : une borne ne doit pas renvoyer en boucle.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// audienceEnabled : la mesure des bornes est active, sans réglage
// d'environnement (décision Ilies, 2026-10-27). La couper demande de passer
// cette constante à false et de redéployer : les bornes cessent alors
// d'envoyer au prochain chargement de leurs paramètres.
const audienceEnabled = true

const (
	EventSessionStart      = "session_start"
	EventFulfillmentChosen = "fulfillment_chosen"
	EventProductView       = "product_view"
	EventAddToCart         = "add_to_cart"
	EventSummaryView       = "summary_view"
	EventPaymentStart      = "payment_start"
	EventPaymentFailed     = "payment_failed"
	EventOrderCreated      = "order_created"
	EventSessionAbandoned  = "session_abandoned"

	// maxKioskEventsPerBatch : au-delà, le lot est refusé (400).
	maxKioskEventsPerBatch = 100
	// maxElapsedMs : une session de plus de 24 h n'a pas de sens ; la durée
	// est alors ignorée.
	maxElapsedMs = 24 * 60 * 60 * 1000
	// maxAmountCents : 100 000 € ; au-delà, le montant est ignoré.
	maxAmountCents = 10_000_000
)

// kioskEventRules : ce que chaque type doit porter.
var kioskEventRules = map[string]struct{ product, detail bool }{
	EventSessionStart:      {},
	EventFulfillmentChosen: {detail: true},
	EventProductView:       {product: true},
	EventAddToCart:         {product: true},
	EventSummaryView:       {},
	EventPaymentStart:      {detail: true},
	EventPaymentFailed:     {detail: true},
	EventOrderCreated:      {},
	EventSessionAbandoned:  {detail: true},
}

var kioskTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

type KioskEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	ElapsedMs *int64 `json:"elapsed_ms,omitempty"`
	ProductID string `json:"product_id,omitempty"`
	// AmountCents : montant du panier (session_abandoned).
	AmountCents *int64 `json:"amount_cents,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Step        string `json:"step,omitempty"`
}

type KioskEventsRequest struct {
	Events []KioskEvent `json:"events"`
}

type kioskEventRow struct {
	Type, SessionID, ProductID, Detail, Step string
	ElapsedMs, AmountCents                   *int64
}

// RecordEvents — POST /kiosk/events. 204 dans tous les cas où le corps est
// lisible, y compris événements ignorés.
func (h *Handler) RecordEvents(w http.ResponseWriter, r *http.Request) {
	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "record_events", models.ErrKioskDeviceTokenInvalid)
		return
	}
	var req KioskEventsRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "record_events", models.ErrInvalidRequestBody)
		return
	}
	if err := h.service.RecordEvents(r.Context(), authenticatedKiosk, req); err != nil {
		models.SendErrorJSON(w, "kiosk", "record_events", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) RecordEvents(ctx context.Context, kiosk *AuthenticatedKiosk, req KioskEventsRequest) error {
	if len(req.Events) > maxKioskEventsPerBatch {
		return models.ErrInvalidInput
	}
	if !audienceEnabled || len(req.Events) == 0 {
		return nil
	}
	rows := validKioskEvents(req.Events)
	if len(rows) == 0 {
		return nil
	}
	return s.repo.InsertKioskEvents(ctx, kiosk.MerchantID, kiosk.KioskID, rows)
}

// validKioskEvents garde les événements bien formés ; durée et montant hors
// bornes sont mis à null plutôt que d'écarter l'événement.
func validKioskEvents(events []KioskEvent) []kioskEventRow {
	rows := make([]kioskEventRow, 0, len(events))
	for _, e := range events {
		rule, ok := kioskEventRules[e.Type]
		if !ok {
			continue
		}
		sessionID, err := uuid.Parse(e.SessionID)
		if err != nil {
			continue
		}
		row := kioskEventRow{Type: e.Type, SessionID: sessionID.String()}

		row.ProductID = strings.TrimSpace(e.ProductID)
		if len(row.ProductID) > 64 || (rule.product && row.ProductID == "") {
			continue
		}
		if !rule.product {
			row.ProductID = ""
		}
		if e.Detail != "" && kioskTokenPattern.MatchString(e.Detail) {
			row.Detail = e.Detail
		}
		if rule.detail && row.Detail == "" {
			continue
		}
		if e.Step != "" && kioskTokenPattern.MatchString(e.Step) {
			row.Step = e.Step
		}
		if e.ElapsedMs != nil && *e.ElapsedMs >= 0 && *e.ElapsedMs <= maxElapsedMs {
			row.ElapsedMs = e.ElapsedMs
		}
		if e.Type == EventSessionAbandoned && e.AmountCents != nil && *e.AmountCents >= 0 && *e.AmountCents <= maxAmountCents {
			row.AmountCents = e.AmountCents
		}
		rows = append(rows, row)
	}
	return rows
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (r *Repository) InsertKioskEvents(ctx context.Context, merchantID, kioskID string, events []kioskEventRow) error {
	db := dbx.GetDB(ctx, r.database)
	var sb strings.Builder
	args := make([]interface{}, 0, len(events)*9)
	sb.WriteString(`INSERT INTO kiosk_events (event_type, merchant_id, kiosk_id, session_id, elapsed_ms, product_id, amount_cents, detail, step) VALUES `)
	for i, e := range events {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, CAST(? AS uuid), ?, ?, ?, ?, ?)")
		args = append(args, e.Type, merchantID, kioskID, e.SessionID, e.ElapsedMs, nullableString(e.ProductID), e.AmountCents, nullableString(e.Detail), nullableString(e.Step))
	}
	_, err := db.ExecContext(ctx, sb.String(), args...)
	return err
}

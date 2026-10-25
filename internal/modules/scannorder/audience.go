package scannorder

// Mesure d'audience anonyme de ScanNOrder (hub de franchise et pages
// restaurant) : POST /scannorder/events et origine des commandes. Tables :
// migration 194. Règles (exemption CNIL, pas de bandeau) : docs/franchises.md,
// D17 et D19 à D23.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

const (
	EventHubView             = "hub_view"
	EventHubOrderClick       = "hub_order_click"
	EventHubReservationClick = "hub_reservation_click"
	EventMenuView            = "menu_view"
	EventProductView         = "product_view"
	EventAddToCart           = "add_to_cart"
	EventCheckoutStart       = "checkout_start"
	EventPaymentRedirect     = "payment_redirect"
	EventOrderCreated        = "order_created"

	// maxEventsPerBatch : le front envoie par lots ; au-delà, la requête est refusée.
	maxEventsPerBatch = 25
)

// eventRules : pour chaque type, ce qu'il doit porter.
var eventRules = map[string]struct{ franchise, merchant, product bool }{
	EventHubView:             {franchise: true},
	EventHubOrderClick:       {franchise: true, merchant: true},
	EventHubReservationClick: {franchise: true, merchant: true},
	EventMenuView:            {merchant: true},
	EventProductView:         {merchant: true, product: true},
	EventAddToCart:           {merchant: true, product: true},
	EventCheckoutStart:       {merchant: true},
	EventPaymentRedirect:     {merchant: true},
	EventOrderCreated:        {merchant: true},
}

var tokenPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// normalizeSource : qr_table, direct, search, social, referral, hub, ou une
// valeur utm_source ; vide si invalide.
func normalizeSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	if !tokenPattern.MatchString(s) {
		return ""
	}
	return s
}

type SNOEvent struct {
	Type          string `json:"type"`
	MerchantID    string `json:"merchant_id,omitempty"`
	FranchiseSlug string `json:"franchise_slug,omitempty"`
	ProductID     string `json:"product_id,omitempty"`
	Source        string `json:"source,omitempty"`
}

type SNOEventsRequest struct {
	// VisitID : identifiant aléatoire de l'onglet (UUID), facultatif.
	VisitID string     `json:"visit_id,omitempty"`
	Events  []SNOEvent `json:"events"`
}

type snoEventRow struct {
	Type, MerchantID, FranchiseID, ProductID, Source string
}

// RecordEvents — POST /scannorder/events. 204 ; 400 pour une requête mal
// formée. Les événements visant un établissement ou une franchise inconnus
// sont ignorés sans erreur : l'envoi part du navigateur en tâche de fond.
func (h *Handler) RecordEvents(w http.ResponseWriter, r *http.Request) {
	var req SNOEventsRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "scannorder", "record_events", models.ErrInvalidRequestBody)
		return
	}
	if err := h.service.RecordEvents(r.Context(), req); err != nil {
		models.SendErrorJSON(w, "scannorder", "record_events", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) RecordEvents(ctx context.Context, req SNOEventsRequest) error {
	if len(req.Events) == 0 || len(req.Events) > maxEventsPerBatch {
		return models.ErrInvalidInput
	}
	visitID := ""
	if req.VisitID != "" {
		parsed, err := uuid.Parse(req.VisitID)
		if err != nil {
			return models.ErrInvalidInput
		}
		visitID = parsed.String()
	}

	// Validation de forme avant toute requête.
	for _, e := range req.Events {
		rule, ok := eventRules[e.Type]
		if !ok {
			return models.ErrInvalidInput
		}
		if (rule.franchise && strings.TrimSpace(e.FranchiseSlug) == "") ||
			(rule.merchant && strings.TrimSpace(e.MerchantID) == "") ||
			(rule.product && strings.TrimSpace(e.ProductID) == "") {
			return models.ErrInvalidInput
		}
	}

	merchantIDs := map[string]bool{}
	slugs := map[string]bool{}
	for _, e := range req.Events {
		if e.MerchantID != "" {
			merchantIDs[strings.TrimSpace(e.MerchantID)] = true
		}
		if e.FranchiseSlug != "" {
			slugs[strings.ToLower(strings.TrimSpace(e.FranchiseSlug))] = true
		}
	}
	known, err := s.repo.ExistingMerchantFranchises(ctx, keys(merchantIDs))
	if err != nil {
		return err
	}
	franchiseIDs, err := s.repo.FranchiseIDsBySlug(ctx, keys(slugs))
	if err != nil {
		return err
	}

	rows := make([]snoEventRow, 0, len(req.Events))
	for _, e := range req.Events {
		row := snoEventRow{
			Type:      e.Type,
			ProductID: strings.TrimSpace(e.ProductID),
			Source:    normalizeSource(e.Source),
		}
		if e.MerchantID != "" {
			id := strings.TrimSpace(e.MerchantID)
			if _, ok := known[id]; !ok {
				continue // établissement inconnu
			}
			row.MerchantID = id
		}
		if e.FranchiseSlug != "" {
			franchiseID, ok := franchiseIDs[strings.ToLower(strings.TrimSpace(e.FranchiseSlug))]
			if !ok {
				continue // franchise inconnue
			}
			// Un clic du hub doit viser un établissement de cette franchise.
			if row.MerchantID != "" && known[row.MerchantID] != franchiseID {
				continue
			}
			row.FranchiseID = franchiseID
		}
		if len(row.ProductID) > 64 {
			row.ProductID = row.ProductID[:64]
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return s.repo.InsertEvents(ctx, visitID, rows)
}

// recordOrderAttribution enregistre l'origine d'une commande ScanNOrder :
// source de la visite et hub de franchise (clic Commander dans les 2 h).
// Best-effort : un échec est journalisé, jamais remonté à la commande.
func (s *Service) recordOrderAttribution(ctx context.Context, req *models.PricingRequest, merchantID, orderID string) {
	if orderID == "" {
		return
	}
	visitID := ""
	if parsed, err := uuid.Parse(strings.TrimSpace(req.VisitID)); err == nil {
		visitID = parsed.String()
	}
	source := normalizeSource(req.VisitSource)

	franchiseID := ""
	if slug := strings.ToLower(strings.TrimSpace(req.HubFranchiseSlug)); slug != "" {
		ids, err := s.repo.FranchiseIDsBySlug(ctx, []string{slug})
		if err == nil {
			if id, ok := ids[slug]; ok {
				if ok, err := s.repo.MerchantInFranchise(ctx, id, merchantID); err == nil && ok {
					franchiseID = id
				}
			}
		}
	}

	if visitID == "" && source == "" && franchiseID == "" {
		return
	}
	if err := s.repo.InsertOrderAttribution(ctx, orderID, merchantID, visitID, source, franchiseID); err != nil {
		logger.FromContext(ctx).Warn("order attribution failed", zap.String("order_id", orderID), zap.Error(err))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func nullable(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// --- Repository ---

// ExistingMerchantFranchises : établissements existants parmi ids, avec leur
// franchise ("" si aucune).
func (r *Repository) ExistingMerchantFranchises(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(id AS TEXT), COALESCE(franchise_id, '')
		FROM merchant WHERE CAST(id AS TEXT) = ANY(?)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, franchiseID string
		if err := rows.Scan(&id, &franchiseID); err != nil {
			return nil, err
		}
		out[id] = franchiseID
	}
	return out, rows.Err()
}

// FranchiseIDsBySlug : slug → franchise_id pour les slugs connus.
func (r *Repository) FranchiseIDsBySlug(ctx context.Context, slugs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(slugs) == 0 {
		return out, nil
	}
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `SELECT slug, franchise_id FROM franchises WHERE slug = ANY(?)`, slugs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug, id string
		if err := rows.Scan(&slug, &id); err != nil {
			return nil, err
		}
		out[slug] = id
	}
	return out, rows.Err()
}

func (r *Repository) InsertEvents(ctx context.Context, visitID string, events []snoEventRow) error {
	db := dbx.GetDB(ctx, r.database)
	var sb strings.Builder
	args := make([]interface{}, 0, len(events)*6)
	sb.WriteString(`INSERT INTO sno_events (event_type, merchant_id, franchise_id, visit_id, source, product_id) VALUES `)
	for i, e := range events {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, CAST(? AS uuid), ?, ?)")
		args = append(args, e.Type, nullable(e.MerchantID), nullable(e.FranchiseID), nullable(visitID), nullable(e.Source), nullable(e.ProductID))
	}
	_, err := db.ExecContext(ctx, sb.String(), args...)
	return err
}

// InsertOrderAttribution : une seule origine par commande.
func (r *Repository) InsertOrderAttribution(ctx context.Context, orderID, merchantID, visitID, source, franchiseID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		INSERT INTO sno_order_attributions (order_id, merchant_id, visit_id, source, franchise_id)
		VALUES (CAST(? AS integer), ?, CAST(? AS uuid), ?, ?)
		ON CONFLICT (order_id) DO NOTHING`,
		orderID, merchantID, nullable(visitID), nullable(source), nullable(franchiseID))
	return err
}

package analytics

// Onglet Borne de la page Analyse : canal borne des établissements
// sélectionnés (commandes, CA, sur place / à emporter, carte / comptoir) et
// mesure d'audience anonyme des bornes (sessions, entonnoir, abandons, échecs
// de paiement, durée d'une commande). Décisions : docs/mesure-audience.md
// (D27 à D33). Table : migration 195.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// kioskTopProducts : produits les plus consultés affichés.
const kioskTopProducts = 10

type KioskAnalyticsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
}

type KioskTotals struct {
	KioskOrders          int64 `json:"kiosk_orders"`
	KioskRevenueTTCCents int64 `json:"kiosk_revenue_ttc_cents"`
	// AllRevenueTTCCents : CA tous canaux du même périmètre, pour la part de
	// la borne.
	AllRevenueTTCCents int64 `json:"all_revenue_ttc_cents"`
}

// KioskFunnel : sessions ayant atteint chaque étape. Une session commence au
// toucher de l'écran de veille et finit au retour en veille.
type KioskFunnel struct {
	Sessions          int64 `json:"sessions"`
	FulfillmentChosen int64 `json:"fulfillment_chosen"`
	ProductViews      int64 `json:"product_views"`
	AddToCart         int64 `json:"add_to_cart"`
	SummaryViews      int64 `json:"summary_views"`
	PaymentStarts     int64 `json:"payment_starts"`
	Orders            int64 `json:"orders"`
}

// KioskCount : une valeur et son nombre (raison d'abandon, d'échec…).
type KioskCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type KioskAbandonStep struct {
	Step        string `json:"step"`
	Sessions    int64  `json:"sessions"`
	AmountCents int64  `json:"amount_cents"`
}

// KioskAbandonments : sessions terminées sans commande, par inactivité ou
// abandon explicite, avec le montant du panier laissé.
type KioskAbandonments struct {
	Sessions    int64              `json:"sessions"`
	AmountCents int64              `json:"amount_cents"`
	ByStep      []KioskAbandonStep `json:"by_step"`
	ByReason    []KioskCount       `json:"by_reason"`
}

// KioskPaymentFailures : paiements carte refusés, annulés ou impossibles.
type KioskPaymentFailures struct {
	// CardStarts : sessions ayant lancé un paiement carte.
	CardStarts int64        `json:"card_starts"`
	Failures   int64        `json:"failures"`
	ByReason   []KioskCount `json:"by_reason"`
}

// KioskDuration : temps du toucher de l'écran de veille à la commande créée.
type KioskDuration struct {
	Orders        int64    `json:"orders"`
	MedianSeconds *float64 `json:"median_seconds"`
	P90Seconds    *float64 `json:"p90_seconds"`
}

// KioskSplitRow : commandes borne par mode (IN, TAKE_AWAY) ou paiement
// (card, counter).
type KioskSplitRow struct {
	Key             string `json:"key"`
	Orders          int64  `json:"orders"`
	RevenueTTCCents int64  `json:"revenue_ttc_cents"`
}

type KioskProductRow struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Views     int64  `json:"views"`
	AddToCart int64  `json:"add_to_cart"`
}

type KioskDay struct {
	Date         string `json:"date"`
	Sessions     int64  `json:"sessions"`
	KioskOrders  int64  `json:"kiosk_orders"`
	Abandonments int64  `json:"abandonments"`
}

type KioskMerchantRow struct {
	MerchantID           string `json:"merchant_id"`
	Sessions             int64  `json:"sessions"`
	KioskOrders          int64  `json:"kiosk_orders"`
	KioskRevenueTTCCents int64  `json:"kiosk_revenue_ttc_cents"`
}

type KioskDeviceRow struct {
	KioskID         string   `json:"kiosk_id"`
	Name            string   `json:"name"`
	MerchantID      string   `json:"merchant_id"`
	Sessions        int64    `json:"sessions"`
	Orders          int64    `json:"orders"`
	RevenueTTCCents int64    `json:"revenue_ttc_cents"`
	Abandonments    int64    `json:"abandonments"`
	PaymentFailures int64    `json:"payment_failures"`
	MedianSeconds   *float64 `json:"median_seconds"`
}

type KioskAnalyticsResponse struct {
	Scope  RevenueScope `json:"scope"`
	Totals KioskTotals  `json:"totals"`
	// AudienceSince : premier événement des bornes du périmètre (YYYY-MM-DD),
	// nil si aucun — avant, rien n'était mesuré.
	AudienceSince   *string              `json:"audience_since"`
	Funnel          KioskFunnel          `json:"funnel"`
	Abandonments    KioskAbandonments    `json:"abandonments"`
	PaymentFailures KioskPaymentFailures `json:"payment_failures"`
	Duration        KioskDuration        `json:"duration"`
	ByOrderType     []KioskSplitRow      `json:"by_order_type"`
	ByPayment       []KioskSplitRow      `json:"by_payment"`
	TopProducts     []KioskProductRow    `json:"top_products"`
	Daily           []KioskDay           `json:"daily"`
	ByMerchant      []KioskMerchantRow   `json:"by_merchant"`
	ByKiosk         []KioskDeviceRow     `json:"by_kiosk"`
}

// --- Handler ---

// GetKiosk POST /analytics/kiosk.
func (h *Handler) GetKiosk(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(helpers.ExtractToken(r)) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_kiosk", map[string]string{"error": "missing_token"})
		return
	}
	var req KioskAnalyticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_kiosk", map[string]string{"error": "invalid_request"})
		return
	}
	resp, err := h.service.GetKiosk(r.Context(), req)
	if err != nil {
		writeError(w, "get_kiosk", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "analytics", "get_kiosk", resp)
}

// --- Service ---

func (s *Service) GetKiosk(ctx context.Context, req KioskAnalyticsRequest) (*KioskAnalyticsResponse, error) {
	scope, err := s.resolveChannelScope(ctx, req.MerchantIDs, req.DateFrom, req.DateTo)
	if err != nil {
		return nil, err
	}
	resp, err := s.repo.GetKioskStats(ctx, scope.MerchantIDs, scope.StartUTC, scope.EndUTC, scope.TZ)
	if err != nil {
		return nil, err
	}
	resp.Scope = RevenueScope{MerchantIDs: scope.MerchantIDs}

	// Une ligne par établissement du périmètre, même sans activité.
	byMerchant := map[string]KioskMerchantRow{}
	for _, row := range resp.ByMerchant {
		byMerchant[row.MerchantID] = row
	}
	resp.ByMerchant = make([]KioskMerchantRow, 0, len(scope.MerchantIDs))
	for _, id := range scope.MerchantIDs {
		row, ok := byMerchant[id]
		if !ok {
			row = KioskMerchantRow{MerchantID: id}
		}
		resp.ByMerchant = append(resp.ByMerchant, row)
	}

	// Série quotidienne continue (jours sans activité à zéro).
	days := map[string]KioskDay{}
	for _, d := range resp.Daily {
		days[d.Date] = d
	}
	resp.Daily = make([]KioskDay, 0, len(days))
	scope.eachDay(func(key string) {
		day, ok := days[key]
		if !ok {
			day = KioskDay{Date: key}
		}
		resp.Daily = append(resp.Daily, day)
	})
	return resp, nil
}

// --- Repository ---

type scannable = interface{ Scan(...interface{}) error }

// msToSeconds : percentile en millisecondes → secondes, nil conservé.
func msToSeconds(ms *float64) *float64 {
	if ms == nil {
		return nil
	}
	s := *ms / 1000
	return &s
}

// GetKioskStats : commandes valides au sens de AnalyticsOrdersScope passées
// sur une borne (order_source KIOSK, kiosk_id), et mesure d'audience des
// bornes (kiosk_events).
func (r *Repository) GetKioskStats(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time, tz *time.Location) (*KioskAnalyticsResponse, error) {
	out := &KioskAnalyticsResponse{
		Abandonments:    KioskAbandonments{ByStep: []KioskAbandonStep{}, ByReason: []KioskCount{}},
		PaymentFailures: KioskPaymentFailures{ByReason: []KioskCount{}},
		ByOrderType:     []KioskSplitRow{},
		ByPayment:       []KioskSplitRow{},
		TopProducts:     []KioskProductRow{},
		ByKiosk:         []KioskDeviceRow{},
	}
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	kiosk := fmt.Sprintf("o.order_source = '%s'", OrderSourceKiosk)
	eventsWhere := `e.merchant_id = ANY(?) AND e.created_at >= ? AND e.created_at < ?`
	eventsArgs := []interface{}{merchantIDs, startUTC, endUTC}
	withArgs := func(base []interface{}, extra ...interface{}) []interface{} {
		return append(append([]interface{}{}, base...), extra...)
	}
	tzName := tz.String()

	err := r.runTx(ctx, func(ctx context.Context, db *dbx.DB) error {
		// Totaux borne et CA tous canaux.
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FILTER (WHERE `+kiosk+`),
			       COALESCE(SUM(o.price) FILTER (WHERE `+kiosk+`), 0),
			       COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where, args...).Scan(&out.Totals.KioskOrders, &out.Totals.KioskRevenueTTCCents, &out.Totals.AllRevenueTTCCents); err != nil {
			return fmt.Errorf("kiosk totals: %w", err)
		}

		// Sur place / à emporter.
		if err := scanQueryRows(ctx, db, `
			SELECT COALESCE(o.order_type, 'unknown'), COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where+` AND `+kiosk+`
			GROUP BY 1
			ORDER BY 2 DESC`, args, func(sc scannable) error {
			var row KioskSplitRow
			if err := sc.Scan(&row.Key, &row.Orders, &row.RevenueTTCCents); err != nil {
				return err
			}
			out.ByOrderType = append(out.ByOrderType, row)
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk by order type: %w", err)
		}

		// Carte à la borne ou au comptoir. Un paiement carte à la borne passe
		// par Stripe Terminal : il laisse un PaymentIntent capturé dans
		// stripe_payments (webhook Stripe), pas de ligne payments.
		if err := scanQueryRows(ctx, db, `
			SELECT CASE WHEN EXISTS (
			           SELECT 1 FROM stripe_payments sp
			           WHERE sp.order_id = o.order_id AND sp.payment_intent_status = 'CAPTURED'
			       ) THEN 'card' ELSE 'counter' END AS method,
			       COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where+` AND `+kiosk+`
			GROUP BY 1
			ORDER BY 2 DESC`, args, func(sc scannable) error {
			var row KioskSplitRow
			if err := sc.Scan(&row.Key, &row.Orders, &row.RevenueTTCCents); err != nil {
				return err
			}
			out.ByPayment = append(out.ByPayment, row)
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk by payment: %w", err)
		}

		// Par établissement.
		byMerchant := map[string]*KioskMerchantRow{}
		merchant := func(id string) *KioskMerchantRow {
			if row, ok := byMerchant[id]; ok {
				return row
			}
			row := &KioskMerchantRow{MerchantID: id}
			byMerchant[id] = row
			return row
		}
		if err := scanQueryRows(ctx, db, `
			SELECT o.merchant_id, COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where+` AND `+kiosk+`
			GROUP BY o.merchant_id`, args, func(sc scannable) error {
			var id string
			var orders, revenue int64
			if err := sc.Scan(&id, &orders, &revenue); err != nil {
				return err
			}
			row := merchant(id)
			row.KioskOrders, row.KioskRevenueTTCCents = orders, revenue
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk by merchant: %w", err)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT e.merchant_id, COUNT(DISTINCT e.session_id)
			FROM kiosk_events e
			WHERE `+eventsWhere+` AND e.event_type = 'session_start'
			GROUP BY e.merchant_id`, eventsArgs, func(sc scannable) error {
			var id string
			var sessions int64
			if err := sc.Scan(&id, &sessions); err != nil {
				return err
			}
			merchant(id).Sessions = sessions
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk sessions by merchant: %w", err)
		}
		for _, row := range byMerchant {
			out.ByMerchant = append(out.ByMerchant, *row)
		}

		// Entonnoir : sessions ayant atteint chaque étape.
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'session_start'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'fulfillment_chosen'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'product_view'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'add_to_cart'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'summary_view'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'payment_start'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'order_created'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'payment_start' AND e.detail = 'card'),
			       COUNT(*) FILTER (WHERE e.event_type = 'payment_failed')
			FROM kiosk_events e
			WHERE `+eventsWhere, eventsArgs...).Scan(
			&out.Funnel.Sessions, &out.Funnel.FulfillmentChosen, &out.Funnel.ProductViews, &out.Funnel.AddToCart,
			&out.Funnel.SummaryViews, &out.Funnel.PaymentStarts, &out.Funnel.Orders,
			&out.PaymentFailures.CardStarts, &out.PaymentFailures.Failures,
		); err != nil {
			return fmt.Errorf("kiosk funnel: %w", err)
		}

		// Début de la mesure pour ce périmètre.
		var since *time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT MIN(e.created_at) FROM kiosk_events e WHERE e.merchant_id = ANY(?)`, merchantIDs).Scan(&since); err != nil {
			return fmt.Errorf("kiosk audience since: %w", err)
		}
		if since != nil {
			d := since.In(tz).Format("2006-01-02")
			out.AudienceSince = &d
		}

		// Abandons par étape (une session n'est abandonnée qu'une fois).
		if err := scanQueryRows(ctx, db, `
			SELECT COALESCE(e.step, 'unknown'), COUNT(DISTINCT e.session_id), COALESCE(SUM(e.amount_cents), 0)
			FROM kiosk_events e
			WHERE `+eventsWhere+` AND e.event_type = 'session_abandoned'
			GROUP BY 1
			ORDER BY 2 DESC`, eventsArgs, func(sc scannable) error {
			var row KioskAbandonStep
			if err := sc.Scan(&row.Step, &row.Sessions, &row.AmountCents); err != nil {
				return err
			}
			out.Abandonments.ByStep = append(out.Abandonments.ByStep, row)
			out.Abandonments.Sessions += row.Sessions
			out.Abandonments.AmountCents += row.AmountCents
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk abandonments by step: %w", err)
		}
		countBy := func(eventType string, dest *[]KioskCount) error {
			return scanQueryRows(ctx, db, `
				SELECT e.detail, COUNT(*)
				FROM kiosk_events e
				WHERE `+eventsWhere+` AND e.event_type = ? AND e.detail IS NOT NULL
				GROUP BY 1
				ORDER BY 2 DESC`, withArgs(eventsArgs, eventType), func(sc scannable) error {
				var row KioskCount
				if err := sc.Scan(&row.Key, &row.Count); err != nil {
					return err
				}
				*dest = append(*dest, row)
				return nil
			})
		}
		if err := countBy("session_abandoned", &out.Abandonments.ByReason); err != nil {
			return fmt.Errorf("kiosk abandonments by reason: %w", err)
		}
		if err := countBy("payment_failed", &out.PaymentFailures.ByReason); err != nil {
			return fmt.Errorf("kiosk payment failures by reason: %w", err)
		}

		// Durée d'une commande (temps écoulé à order_created, mesuré par la borne).
		var median, p90 *float64
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*),
			       percentile_cont(0.5) WITHIN GROUP (ORDER BY e.elapsed_ms),
			       percentile_cont(0.9) WITHIN GROUP (ORDER BY e.elapsed_ms)
			FROM kiosk_events e
			WHERE `+eventsWhere+` AND e.event_type = 'order_created' AND e.elapsed_ms IS NOT NULL`, eventsArgs...).Scan(
			&out.Duration.Orders, &median, &p90); err != nil {
			return fmt.Errorf("kiosk duration: %w", err)
		}
		out.Duration.MedianSeconds, out.Duration.P90Seconds = msToSeconds(median), msToSeconds(p90)

		// Produits les plus consultés, avec leurs ajouts au panier.
		if err := scanQueryRows(ctx, db, `
			SELECT e.product_id, COALESCE(MAX(p.name), ''),
			       COUNT(*) FILTER (WHERE e.event_type = 'product_view') AS views,
			       COUNT(*) FILTER (WHERE e.event_type = 'add_to_cart')
			FROM kiosk_events e
			LEFT JOIN products p ON CAST(p.product_id AS TEXT) = e.product_id
			WHERE `+eventsWhere+` AND e.event_type IN ('product_view', 'add_to_cart') AND e.product_id IS NOT NULL
			GROUP BY e.product_id
			ORDER BY views DESC
			LIMIT ?`, withArgs(eventsArgs, kioskTopProducts), func(sc scannable) error {
			var row KioskProductRow
			if err := sc.Scan(&row.ProductID, &row.Name, &row.Views, &row.AddToCart); err != nil {
				return err
			}
			out.TopProducts = append(out.TopProducts, row)
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk top products: %w", err)
		}

		// Série quotidienne (jour local de l'établissement).
		days := map[string]*KioskDay{}
		day := func(key string) *KioskDay {
			if d, ok := days[key]; ok {
				return d
			}
			d := &KioskDay{Date: key}
			days[key] = d
			return d
		}
		if err := scanQueryRows(ctx, db, `
			SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS day, COUNT(*)
			FROM orders o
			WHERE `+where+` AND `+kiosk+`
			GROUP BY day`, append([]interface{}{tzName}, args...), func(sc scannable) error {
			var key string
			var orders int64
			if err := sc.Scan(&key, &orders); err != nil {
				return err
			}
			day(key).KioskOrders = orders
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk daily orders: %w", err)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT to_char(e.created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'session_start'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'session_abandoned')
			FROM kiosk_events e
			WHERE `+eventsWhere+`
			GROUP BY day`, append([]interface{}{tzName}, eventsArgs...), func(sc scannable) error {
			var key string
			var sessions, abandonments int64
			if err := sc.Scan(&key, &sessions, &abandonments); err != nil {
				return err
			}
			d := day(key)
			d.Sessions, d.Abandonments = sessions, abandonments
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk daily audience: %w", err)
		}
		for _, d := range days {
			out.Daily = append(out.Daily, *d)
		}

		// Par borne : bornes non révoquées du périmètre, et toute borne ayant
		// eu de l'activité sur la période.
		devices := map[string]*KioskDeviceRow{}
		device := func(id string) *KioskDeviceRow {
			if row, ok := devices[id]; ok {
				return row
			}
			row := &KioskDeviceRow{KioskID: id}
			devices[id] = row
			return row
		}
		if err := scanQueryRows(ctx, db, `
			SELECT o.kiosk_id, COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where+` AND `+kiosk+` AND o.kiosk_id IS NOT NULL
			GROUP BY o.kiosk_id`, args, func(sc scannable) error {
			var id string
			var orders, revenue int64
			if err := sc.Scan(&id, &orders, &revenue); err != nil {
				return err
			}
			row := device(id)
			row.Orders, row.RevenueTTCCents = orders, revenue
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk by device orders: %w", err)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT e.kiosk_id,
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'session_start'),
			       COUNT(DISTINCT e.session_id) FILTER (WHERE e.event_type = 'session_abandoned'),
			       COUNT(*) FILTER (WHERE e.event_type = 'payment_failed'),
			       percentile_cont(0.5) WITHIN GROUP (ORDER BY e.elapsed_ms)
			           FILTER (WHERE e.event_type = 'order_created' AND e.elapsed_ms IS NOT NULL)
			FROM kiosk_events e
			WHERE `+eventsWhere+`
			GROUP BY e.kiosk_id`, eventsArgs, func(sc scannable) error {
			var id string
			var sessions, abandonments, failures int64
			var medianMs *float64
			if err := sc.Scan(&id, &sessions, &abandonments, &failures, &medianMs); err != nil {
				return err
			}
			row := device(id)
			row.Sessions, row.Abandonments, row.PaymentFailures = sessions, abandonments, failures
			row.MedianSeconds = msToSeconds(medianMs)
			return nil
		}); err != nil {
			return fmt.Errorf("kiosk by device audience: %w", err)
		}
		ids := make([]string, 0, len(devices))
		for id := range devices {
			ids = append(ids, id)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT k.id, k.name, k.merchant_id
			FROM kiosks k
			WHERE k.id = ANY(?) OR (k.merchant_id = ANY(?) AND k.status <> 'revoked')`,
			[]interface{}{ids, merchantIDs}, func(sc scannable) error {
				var id, name, merchantID string
				if err := sc.Scan(&id, &name, &merchantID); err != nil {
					return err
				}
				row := device(id)
				row.Name, row.MerchantID = name, merchantID
				return nil
			}); err != nil {
			return fmt.Errorf("kiosk names: %w", err)
		}
		for _, row := range devices {
			out.ByKiosk = append(out.ByKiosk, *row)
		}
		sort.Slice(out.ByKiosk, func(i, j int) bool {
			if out.ByKiosk[i].RevenueTTCCents != out.ByKiosk[j].RevenueTTCCents {
				return out.ByKiosk[i].RevenueTTCCents > out.ByKiosk[j].RevenueTTCCents
			}
			return out.ByKiosk[i].Name < out.ByKiosk[j].Name
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

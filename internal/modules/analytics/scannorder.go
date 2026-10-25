package analytics

// Onglet ScanNOrder de la page Analyse : canal ScanNOrder des établissements
// sélectionnés, entonnoir de conversion et sources des visites (mesure
// d'audience anonyme), et apport du hub de leur franchise. Décisions :
// docs/franchises.md (D16 à D23). Tables : migration 194.

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
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/timeutil"
)

// scanNOrderTopProducts : produits les plus vus affichés.
const scanNOrderTopProducts = 10

type ScanNOrderAnalyticsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
}

type ScanNOrderTotals struct {
	SNOOrders          int64 `json:"sno_orders"`
	SNORevenueTTCCents int64 `json:"sno_revenue_ttc_cents"`
	// AllRevenueTTCCents : CA tous canaux du même périmètre, pour la part du
	// ScanNOrder.
	AllRevenueTTCCents int64 `json:"all_revenue_ttc_cents"`
}

// ScanNOrderFunnel : visites (onglets) ayant atteint chaque étape. Une visite
// = un onglet ouvert, oublié après 30 min d'inactivité.
type ScanNOrderFunnel struct {
	Visits          int64 `json:"visits"`
	ProductViews    int64 `json:"product_views"`
	AddToCart       int64 `json:"add_to_cart"`
	CheckoutStart   int64 `json:"checkout_start"`
	PaymentRedirect int64 `json:"payment_redirect"`
	Orders          int64 `json:"orders"`
}

type ScanNOrderSourceRow struct {
	// Source : qr_table, direct, search, social, referral, hub, une valeur
	// utm_source, ou unknown (commande sans origine connue).
	Source          string `json:"source"`
	Visits          int64  `json:"visits"`
	Orders          int64  `json:"orders"`
	RevenueTTCCents int64  `json:"revenue_ttc_cents"`
}

type ScanNOrderProductRow struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Views     int64  `json:"views"`
	AddToCart int64  `json:"add_to_cart"`
}

type ScanNOrderHubTotals struct {
	// InFranchise : au moins un établissement du périmètre est dans une
	// franchise (sinon l'onglet invite à en créer une).
	InFranchise bool     `json:"in_franchise"`
	Franchises  []string `json:"franchises"`
	// Views : affichages des hubs de ces franchises (tous établissements
	// confondus — un affichage montre toute la liste).
	Views             int64 `json:"views"`
	OrderClicks       int64 `json:"order_clicks"`
	ReservationClicks int64 `json:"reservation_clicks"`
	Orders            int64 `json:"orders"`
	RevenueTTCCents   int64 `json:"revenue_ttc_cents"`
}

type ScanNOrderDay struct {
	Date        string `json:"date"`
	Visits      int64  `json:"visits"`
	SNOOrders   int64  `json:"sno_orders"`
	HubOrders   int64  `json:"hub_orders"`
	OrderClicks int64  `json:"order_clicks"`
}

type ScanNOrderMerchantRow struct {
	MerchantID         string `json:"merchant_id"`
	Visits             int64  `json:"visits"`
	SNOOrders          int64  `json:"sno_orders"`
	SNORevenueTTCCents int64  `json:"sno_revenue_ttc_cents"`
	OrderClicks        int64  `json:"order_clicks"`
	ReservationClicks  int64  `json:"reservation_clicks"`
	HubOrders          int64  `json:"hub_orders"`
	HubRevenueTTCCents int64  `json:"hub_revenue_ttc_cents"`
}

type ScanNOrderAnalyticsResponse struct {
	Scope  RevenueScope     `json:"scope"`
	Totals ScanNOrderTotals `json:"totals"`
	// AudienceSince : premier événement d'audience du périmètre (YYYY-MM-DD),
	// nil si aucun — avant, rien n'était mesuré.
	AudienceSince *string                 `json:"audience_since"`
	Funnel        ScanNOrderFunnel        `json:"funnel"`
	Sources       []ScanNOrderSourceRow   `json:"sources"`
	TopProducts   []ScanNOrderProductRow  `json:"top_products"`
	Hub           ScanNOrderHubTotals     `json:"hub"`
	Daily         []ScanNOrderDay         `json:"daily"`
	ByMerchant    []ScanNOrderMerchantRow `json:"by_merchant"`
}

// --- Handler ---

// GetScanNOrder POST /analytics/scannorder.
func (h *Handler) GetScanNOrder(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(helpers.ExtractToken(r)) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "analytics", "get_scannorder", map[string]string{"error": "missing_token"})
		return
	}
	var req ScanNOrderAnalyticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "analytics", "get_scannorder", map[string]string{"error": "invalid_request"})
		return
	}
	resp, err := h.service.GetScanNOrder(r.Context(), req)
	if err != nil {
		writeError(w, "get_scannorder", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "analytics", "get_scannorder", resp)
}

// --- Service ---

func (s *Service) GetScanNOrder(ctx context.Context, req ScanNOrderAnalyticsRequest) (*ScanNOrderAnalyticsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	accessible, err := s.resolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}
	if len(merchantIDs) == 0 {
		return nil, ErrMerchantNotAccessible
	}

	dateFrom, err := time.Parse("2006-01-02", req.DateFrom)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	dateTo, err := time.Parse("2006-01-02", req.DateTo)
	if err != nil || dateTo.Before(dateFrom) {
		return nil, ErrInvalidRequest
	}

	tzString, err := s.getMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}
	startUTC, endUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)

	data, err := s.repo.GetScanNOrderStats(ctx, merchantIDs, startUTC, endUTC, tz)
	if err != nil {
		return nil, err
	}

	resp := &ScanNOrderAnalyticsResponse{
		Scope:         RevenueScope{MerchantIDs: merchantIDs},
		Totals:        data.Totals,
		AudienceSince: data.AudienceSince,
		Funnel:        data.Funnel,
		Sources:       data.Sources,
		TopProducts:   data.TopProducts,
		Hub:           data.Hub,
	}

	// Une ligne par établissement du périmètre, même sans activité ; totaux du
	// hub = somme des établissements (clics et commandes sont par établissement).
	byMerchant := map[string]*ScanNOrderMerchantRow{}
	for _, row := range data.ByMerchant {
		r := row
		byMerchant[row.MerchantID] = &r
	}
	for _, id := range merchantIDs {
		row, ok := byMerchant[id]
		if !ok {
			row = &ScanNOrderMerchantRow{MerchantID: id}
		}
		resp.Hub.OrderClicks += row.OrderClicks
		resp.Hub.ReservationClicks += row.ReservationClicks
		resp.Hub.Orders += row.HubOrders
		resp.Hub.RevenueTTCCents += row.HubRevenueTTCCents
		resp.ByMerchant = append(resp.ByMerchant, *row)
	}

	// Série quotidienne continue sur la période (jours sans activité à zéro).
	days := map[string]ScanNOrderDay{}
	for _, d := range data.Daily {
		days[d.Date] = d
	}
	for d := dateFrom; !d.After(dateTo); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		day, ok := days[key]
		if !ok {
			day = ScanNOrderDay{Date: key}
		}
		resp.Daily = append(resp.Daily, day)
	}
	return resp, nil
}

// --- Repository ---

type scanNOrderStats struct {
	Totals        ScanNOrderTotals
	AudienceSince *string
	Funnel        ScanNOrderFunnel
	Sources       []ScanNOrderSourceRow
	TopProducts   []ScanNOrderProductRow
	Hub           ScanNOrderHubTotals
	Daily         []ScanNOrderDay
	ByMerchant    []ScanNOrderMerchantRow
}

// scanQueryRows exécute une requête et appelle scan pour chaque ligne.
func scanQueryRows(ctx context.Context, db *dbx.DB, query string, args []interface{}, scan func(scanner interface{ Scan(...interface{}) error }) error) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// GetScanNOrderStats : commandes valides au sens de AnalyticsOrdersScope (même
// périmètre que les onglets CA / Commandes), origine des commandes
// (sno_order_attributions) et mesure d'audience (sno_events).
func (r *Repository) GetScanNOrderStats(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time, tz *time.Location) (*scanNOrderStats, error) {
	out := &scanNOrderStats{
		Hub:         ScanNOrderHubTotals{Franchises: []string{}},
		Sources:     []ScanNOrderSourceRow{},
		TopProducts: []ScanNOrderProductRow{},
	}
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	sno := fmt.Sprintf("o.order_source = '%s'", OrderSourceScanNOrder)
	eventsWhere := `e.merchant_id = ANY(?) AND e.created_at >= ? AND e.created_at < ?`
	eventsArgs := []interface{}{merchantIDs, startUTC, endUTC}
	tzName := tz.String()

	err := r.runTx(ctx, func(ctx context.Context, db *dbx.DB) error {
		// Totaux ScanNOrder et CA tous canaux.
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FILTER (WHERE `+sno+`),
			       COALESCE(SUM(o.price) FILTER (WHERE `+sno+`), 0),
			       COALESCE(SUM(o.price), 0)
			FROM orders o
			WHERE `+where, args...).Scan(&out.Totals.SNOOrders, &out.Totals.SNORevenueTTCCents, &out.Totals.AllRevenueTTCCents); err != nil {
			return fmt.Errorf("scannorder totals: %w", err)
		}

		// Par établissement : ScanNOrder et commandes venues du hub.
		byMerchant := map[string]*ScanNOrderMerchantRow{}
		get := func(id string) *ScanNOrderMerchantRow {
			if row, ok := byMerchant[id]; ok {
				return row
			}
			row := &ScanNOrderMerchantRow{MerchantID: id}
			byMerchant[id] = row
			return row
		}
		if err := scanQueryRows(ctx, db, `
			SELECT o.merchant_id,
			       COUNT(*) FILTER (WHERE `+sno+`),
			       COALESCE(SUM(o.price) FILTER (WHERE `+sno+`), 0),
			       COUNT(a.order_id) FILTER (WHERE a.franchise_id IS NOT NULL),
			       COALESCE(SUM(o.price) FILTER (WHERE a.franchise_id IS NOT NULL), 0)
			FROM orders o
			LEFT JOIN sno_order_attributions a ON a.order_id = o.order_id
			WHERE `+where+`
			GROUP BY o.merchant_id`, args, func(sc interface{ Scan(...interface{}) error }) error {
			var id string
			var snoOrders, snoRevenue, hubOrders, hubRevenue int64
			if err := sc.Scan(&id, &snoOrders, &snoRevenue, &hubOrders, &hubRevenue); err != nil {
				return err
			}
			row := get(id)
			row.SNOOrders, row.SNORevenueTTCCents, row.HubOrders, row.HubRevenueTTCCents = snoOrders, snoRevenue, hubOrders, hubRevenue
			return nil
		}); err != nil {
			return fmt.Errorf("scannorder by merchant: %w", err)
		}

		// Audience par établissement : visites et clics du hub.
		if err := scanQueryRows(ctx, db, `
			SELECT e.merchant_id,
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'menu_view'),
			       COUNT(*) FILTER (WHERE e.event_type = 'hub_order_click'),
			       COUNT(*) FILTER (WHERE e.event_type = 'hub_reservation_click')
			FROM sno_events e
			WHERE `+eventsWhere+`
			GROUP BY e.merchant_id`, eventsArgs, func(sc interface{ Scan(...interface{}) error }) error {
			var id string
			var visits, orderClicks, reservationClicks int64
			if err := sc.Scan(&id, &visits, &orderClicks, &reservationClicks); err != nil {
				return err
			}
			row := get(id)
			row.Visits, row.OrderClicks, row.ReservationClicks = visits, orderClicks, reservationClicks
			return nil
		}); err != nil {
			return fmt.Errorf("audience by merchant: %w", err)
		}
		for _, row := range byMerchant {
			out.ByMerchant = append(out.ByMerchant, *row)
		}
		sort.Slice(out.ByMerchant, func(i, j int) bool { return out.ByMerchant[i].MerchantID < out.ByMerchant[j].MerchantID })

		// Entonnoir : visites ayant atteint chaque étape.
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'menu_view'),
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'product_view'),
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'add_to_cart'),
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'checkout_start'),
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'payment_redirect'),
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'order_created')
			FROM sno_events e
			WHERE `+eventsWhere+` AND e.visit_id IS NOT NULL`, eventsArgs...).Scan(
			&out.Funnel.Visits, &out.Funnel.ProductViews, &out.Funnel.AddToCart,
			&out.Funnel.CheckoutStart, &out.Funnel.PaymentRedirect, &out.Funnel.Orders,
		); err != nil {
			return fmt.Errorf("funnel: %w", err)
		}

		// Début de la mesure pour ce périmètre.
		var since *time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT MIN(e.created_at) FROM sno_events e WHERE e.merchant_id = ANY(?)`, merchantIDs).Scan(&since); err != nil {
			return fmt.Errorf("audience since: %w", err)
		}
		if since != nil {
			d := since.In(tz).Format("2006-01-02")
			out.AudienceSince = &d
		}

		// Sources : visites (audience) et commandes / CA (origine des commandes).
		sources := map[string]*ScanNOrderSourceRow{}
		source := func(name string) *ScanNOrderSourceRow {
			if row, ok := sources[name]; ok {
				return row
			}
			row := &ScanNOrderSourceRow{Source: name}
			sources[name] = row
			return row
		}
		if err := scanQueryRows(ctx, db, `
			SELECT COALESCE(e.source, 'direct'), COUNT(DISTINCT e.visit_id)
			FROM sno_events e
			WHERE `+eventsWhere+` AND e.event_type = 'menu_view'
			GROUP BY 1`, eventsArgs, func(sc interface{ Scan(...interface{}) error }) error {
			var name string
			var n int64
			if err := sc.Scan(&name, &n); err != nil {
				return err
			}
			source(name).Visits = n
			return nil
		}); err != nil {
			return fmt.Errorf("sources visits: %w", err)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT COALESCE(a.source, 'unknown'), COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			LEFT JOIN sno_order_attributions a ON a.order_id = o.order_id
			WHERE `+where+` AND `+sno+`
			GROUP BY 1`, args, func(sc interface{ Scan(...interface{}) error }) error {
			var name string
			var orders, revenue int64
			if err := sc.Scan(&name, &orders, &revenue); err != nil {
				return err
			}
			row := source(name)
			row.Orders, row.RevenueTTCCents = orders, revenue
			return nil
		}); err != nil {
			return fmt.Errorf("sources orders: %w", err)
		}
		for _, row := range sources {
			out.Sources = append(out.Sources, *row)
		}
		sort.Slice(out.Sources, func(i, j int) bool {
			if out.Sources[i].Visits != out.Sources[j].Visits {
				return out.Sources[i].Visits > out.Sources[j].Visits
			}
			return out.Sources[i].Orders > out.Sources[j].Orders
		})

		// Produits les plus vus, avec leurs ajouts au panier.
		if err := scanQueryRows(ctx, db, `
			SELECT e.product_id, COALESCE(MAX(p.name), ''),
			       COUNT(*) FILTER (WHERE e.event_type = 'product_view') AS views,
			       COUNT(*) FILTER (WHERE e.event_type = 'add_to_cart')
			FROM sno_events e
			LEFT JOIN products p ON CAST(p.product_id AS TEXT) = e.product_id
			WHERE `+eventsWhere+` AND e.event_type IN ('product_view', 'add_to_cart') AND e.product_id IS NOT NULL
			GROUP BY e.product_id
			ORDER BY views DESC
			LIMIT ?`, append(append([]interface{}{}, eventsArgs...), scanNOrderTopProducts), func(sc interface{ Scan(...interface{}) error }) error {
			var row ScanNOrderProductRow
			if err := sc.Scan(&row.ProductID, &row.Name, &row.Views, &row.AddToCart); err != nil {
				return err
			}
			out.TopProducts = append(out.TopProducts, row)
			return nil
		}); err != nil {
			return fmt.Errorf("top products: %w", err)
		}

		// Franchises du périmètre et affichages de leurs hubs.
		if err := scanQueryRows(ctx, db, `
			SELECT DISTINCT f.name
			FROM franchises f
			INNER JOIN merchant m ON m.franchise_id = f.franchise_id
			WHERE CAST(m.id AS TEXT) = ANY(?)
			ORDER BY f.name`, []interface{}{merchantIDs}, func(sc interface{ Scan(...interface{}) error }) error {
			var name string
			if err := sc.Scan(&name); err != nil {
				return err
			}
			out.Hub.Franchises = append(out.Hub.Franchises, name)
			return nil
		}); err != nil {
			return fmt.Errorf("scope franchises: %w", err)
		}
		out.Hub.InFranchise = len(out.Hub.Franchises) > 0
		if out.Hub.InFranchise {
			if err := db.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM sno_events e
				WHERE e.event_type = 'hub_view' AND e.created_at >= ? AND e.created_at < ?
				  AND e.franchise_id IN (
					SELECT m.franchise_id FROM merchant m
					WHERE CAST(m.id AS TEXT) = ANY(?) AND m.franchise_id IS NOT NULL
				  )`, startUTC, endUTC, merchantIDs).Scan(&out.Hub.Views); err != nil {
				return fmt.Errorf("hub views: %w", err)
			}
		}

		// Série quotidienne (jour local de l'établissement).
		days := map[string]*ScanNOrderDay{}
		day := func(key string) *ScanNOrderDay {
			if d, ok := days[key]; ok {
				return d
			}
			d := &ScanNOrderDay{Date: key}
			days[key] = d
			return d
		}
		if err := scanQueryRows(ctx, db, `
			SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
			       COUNT(*) FILTER (WHERE `+sno+`),
			       COUNT(a.order_id) FILTER (WHERE a.franchise_id IS NOT NULL)
			FROM orders o
			LEFT JOIN sno_order_attributions a ON a.order_id = o.order_id
			WHERE `+where+`
			GROUP BY day`, append([]interface{}{tzName}, args...), func(sc interface{ Scan(...interface{}) error }) error {
			var key string
			var snoOrders, hubOrders int64
			if err := sc.Scan(&key, &snoOrders, &hubOrders); err != nil {
				return err
			}
			d := day(key)
			d.SNOOrders, d.HubOrders = snoOrders, hubOrders
			return nil
		}); err != nil {
			return fmt.Errorf("scannorder daily: %w", err)
		}
		if err := scanQueryRows(ctx, db, `
			SELECT to_char(e.created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
			       COUNT(DISTINCT e.visit_id) FILTER (WHERE e.event_type = 'menu_view'),
			       COUNT(*) FILTER (WHERE e.event_type = 'hub_order_click')
			FROM sno_events e
			WHERE `+eventsWhere+`
			GROUP BY day`, append([]interface{}{tzName}, eventsArgs...), func(sc interface{ Scan(...interface{}) error }) error {
			var key string
			var visits, clicks int64
			if err := sc.Scan(&key, &visits, &clicks); err != nil {
				return err
			}
			d := day(key)
			d.Visits, d.OrderClicks = visits, clicks
			return nil
		}); err != nil {
			return fmt.Errorf("audience daily: %w", err)
		}
		for _, d := range days {
			out.Daily = append(out.Daily, *d)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

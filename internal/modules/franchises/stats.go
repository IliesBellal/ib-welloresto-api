package franchises

// Statistiques de la franchise : GET /franchises/{id}/stats (menu Franchise →
// Statistiques du back-office). Mesure d'audience du hub (sno_events) et
// commandes venues du hub (sno_order_attributions), sur la même définition
// des commandes que la page Analyse. Réservé aux gestionnaires de la
// franchise. Décisions : docs/franchises.md, D24 à D26.

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/analytics"
	"welloresto-api/internal/timeutil"
)

// maxStatsDays borne la période demandée.
const maxStatsDays = 366

type StatsTotals struct {
	HubViews          int64 `json:"hub_views"`
	OrderClicks       int64 `json:"order_clicks"`
	ReservationClicks int64 `json:"reservation_clicks"`
	HubOrders         int64 `json:"hub_orders"`
	HubRevenueTTC     int64 `json:"hub_revenue_ttc_cents"`
}

type StatsSource struct {
	// Source : provenance des visites du hub (direct, search, social,
	// referral, ou valeur utm_source / src).
	Source string `json:"source"`
	Views  int64  `json:"views"`
}

type StatsDay struct {
	Date        string `json:"date"`
	HubViews    int64  `json:"hub_views"`
	OrderClicks int64  `json:"order_clicks"`
	HubOrders   int64  `json:"hub_orders"`
}

type StatsMerchant struct {
	MerchantID        string `json:"merchant_id"`
	BusinessName      string `json:"business_name"`
	OrderClicks       int64  `json:"order_clicks"`
	ReservationClicks int64  `json:"reservation_clicks"`
	HubOrders         int64  `json:"hub_orders"`
	HubRevenueTTC     int64  `json:"hub_revenue_ttc_cents"`
}

type StatsResponse struct {
	FranchiseID string `json:"franchise_id"`
	DateFrom    string `json:"date_from"`
	DateTo      string `json:"date_to"`
	// TrackingSince : premier événement mesuré pour cette franchise
	// (YYYY-MM-DD), nil si aucun.
	TrackingSince *string         `json:"tracking_since"`
	Totals        StatsTotals     `json:"totals"`
	Sources       []StatsSource   `json:"sources"`
	Daily         []StatsDay      `json:"daily"`
	Merchants     []StatsMerchant `json:"merchants"`
}

// Stats — GET /franchises/{franchise_id}/stats?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	handle(w, r, "get_franchise_stats", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.Stats(r.Context(), a, chi.URLParam(r, "franchise_id"), q.Get("date_from"), q.Get("date_to"))
	})
}

func (s *Service) Stats(ctx context.Context, actor Actor, franchiseID, dateFrom, dateTo string) (*StatsResponse, error) {
	f, _, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	from, err := time.Parse("2006-01-02", dateFrom)
	if err != nil {
		return nil, models.ErrInvalidInput
	}
	to, err := time.Parse("2006-01-02", dateTo)
	if err != nil || to.Before(from) || to.Sub(from) > maxStatsDays*24*time.Hour {
		return nil, models.ErrInvalidInput
	}

	merchants, err := s.store.ListFranchiseMerchants(ctx, f.FranchiseID)
	if err != nil {
		return nil, err
	}
	merchantIDs := make([]string, 0, len(merchants))
	for _, m := range merchants {
		merchantIDs = append(merchantIDs, m.MerchantID)
	}

	tzName, err := s.store.FranchiseTimezone(ctx, f.FranchiseID)
	if err != nil {
		return nil, err
	}
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		tz, tzName = time.UTC, "UTC"
	}
	startUTC, endUTC := timeutil.LocalDayRangeBounds(from, to, tz)

	resp, err := s.store.FranchiseStats(ctx, f.FranchiseID, merchantIDs, startUTC, endUTC, tzName)
	if err != nil {
		return nil, err
	}
	resp.FranchiseID, resp.DateFrom, resp.DateTo = f.FranchiseID, dateFrom, dateTo

	// Un établissement par ligne, même sans activité, avec son nom.
	byID := map[string]StatsMerchant{}
	for _, m := range resp.Merchants {
		byID[m.MerchantID] = m
	}
	resp.Merchants = make([]StatsMerchant, 0, len(merchants))
	for _, m := range merchants {
		row := byID[m.MerchantID]
		row.MerchantID, row.BusinessName = m.MerchantID, m.BusinessName
		resp.Merchants = append(resp.Merchants, row)
	}
	sort.SliceStable(resp.Merchants, func(i, j int) bool {
		return resp.Merchants[i].HubRevenueTTC > resp.Merchants[j].HubRevenueTTC
	})

	// Série quotidienne continue.
	days := map[string]StatsDay{}
	for _, d := range resp.Daily {
		days[d.Date] = d
	}
	resp.Daily = resp.Daily[:0]
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		day, ok := days[key]
		if !ok {
			day = StatsDay{Date: key}
		}
		resp.Daily = append(resp.Daily, day)
	}
	return resp, nil
}

// --- Repository ---

// FranchiseTimezone : fuseau du premier établissement, Europe/Paris sinon.
func (r *Repository) FranchiseTimezone(ctx context.Context, franchiseID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var tz string
	err := db.QueryRowContext(ctx, `
		SELECT timezone FROM merchant WHERE franchise_id = ? ORDER BY id LIMIT 1`, franchiseID).Scan(&tz)
	if err == sql.ErrNoRows || tz == "" {
		return "Europe/Paris", nil
	}
	return tz, err
}

func (r *Repository) FranchiseStats(ctx context.Context, franchiseID string, merchantIDs []string, startUTC, endUTC time.Time, tz string) (*StatsResponse, error) {
	db := dbx.GetDB(ctx, r.db)
	out := &StatsResponse{Sources: []StatsSource{}, Daily: []StatsDay{}, Merchants: []StatsMerchant{}}
	eventsWhere := `e.franchise_id = ? AND e.created_at >= ? AND e.created_at < ?`
	eventsArgs := []interface{}{franchiseID, startUTC, endUTC}

	// Affichages et clics.
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE e.event_type = 'hub_view'),
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_order_click'),
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_reservation_click')
		FROM sno_events e WHERE `+eventsWhere, eventsArgs...).Scan(
		&out.Totals.HubViews, &out.Totals.OrderClicks, &out.Totals.ReservationClicks); err != nil {
		return nil, err
	}

	var since *time.Time
	if err := db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM sno_events WHERE franchise_id = ?`, franchiseID).Scan(&since); err != nil {
		return nil, err
	}
	if since != nil {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		d := since.In(loc).Format("2006-01-02")
		out.TrackingSince = &d
	}

	// Provenance des visites du hub.
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(e.source, 'direct'), COUNT(*)
		FROM sno_events e WHERE `+eventsWhere+` AND e.event_type = 'hub_view'
		GROUP BY 1 ORDER BY 2 DESC`, eventsArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s StatsSource
		if err := rows.Scan(&s.Source, &s.Views); err != nil {
			rows.Close()
			return nil, err
		}
		out.Sources = append(out.Sources, s)
	}
	rows.Close()

	// Clics par établissement.
	byMerchant := map[string]*StatsMerchant{}
	get := func(id string) *StatsMerchant {
		if m, ok := byMerchant[id]; ok {
			return m
		}
		m := &StatsMerchant{MerchantID: id}
		byMerchant[id] = m
		return m
	}
	rows, err = db.QueryContext(ctx, `
		SELECT e.merchant_id,
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_order_click'),
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_reservation_click')
		FROM sno_events e WHERE `+eventsWhere+` AND e.merchant_id IS NOT NULL
		GROUP BY e.merchant_id`, eventsArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var orderClicks, reservationClicks int64
		if err := rows.Scan(&id, &orderClicks, &reservationClicks); err != nil {
			rows.Close()
			return nil, err
		}
		m := get(id)
		m.OrderClicks, m.ReservationClicks = orderClicks, reservationClicks
	}
	rows.Close()

	days := map[string]*StatsDay{}
	day := func(key string) *StatsDay {
		if d, ok := days[key]; ok {
			return d
		}
		d := &StatsDay{Date: key}
		days[key] = d
		return d
	}
	rows, err = db.QueryContext(ctx, `
		SELECT to_char(e.created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_view'),
		       COUNT(*) FILTER (WHERE e.event_type = 'hub_order_click')
		FROM sno_events e WHERE `+eventsWhere+`
		GROUP BY day`, append([]interface{}{tz}, eventsArgs...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var views, clicks int64
		if err := rows.Scan(&key, &views, &clicks); err != nil {
			rows.Close()
			return nil, err
		}
		d := day(key)
		d.HubViews, d.OrderClicks = views, clicks
	}
	rows.Close()

	// Commandes venues du hub : commandes valides au sens de la page Analyse
	// (AnalyticsOrdersScope), sur les établissements actuels de la franchise.
	if len(merchantIDs) > 0 {
		where, args := analytics.AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
		rows, err = db.QueryContext(ctx, `
			SELECT o.merchant_id, to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
			       COUNT(*), COALESCE(SUM(o.price), 0)
			FROM orders o
			INNER JOIN sno_order_attributions a ON a.order_id = o.order_id AND a.franchise_id = ?
			WHERE `+where+`
			GROUP BY o.merchant_id, day`, append([]interface{}{tz, franchiseID}, args...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, key string
			var orders, revenue int64
			if err := rows.Scan(&id, &key, &orders, &revenue); err != nil {
				rows.Close()
				return nil, err
			}
			m := get(id)
			m.HubOrders += orders
			m.HubRevenueTTC += revenue
			day(key).HubOrders += orders
			out.Totals.HubOrders += orders
			out.Totals.HubRevenueTTC += revenue
		}
		rows.Close()
	}

	for _, m := range byMerchant {
		out.Merchants = append(out.Merchants, *m)
	}
	for _, d := range days {
		out.Daily = append(out.Daily, *d)
	}
	return out, nil
}

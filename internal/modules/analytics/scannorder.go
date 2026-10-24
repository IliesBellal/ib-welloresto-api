package analytics

// Onglet ScanNOrder de la page Analyse : le canal ScanNOrder des
// établissements sélectionnés et la part qui vient du hub de leur franchise
// (affichages, clics Commander / Réserver, commandes attribuées). Décisions :
// docs/franchises.md. Tables du hub : migration 194.

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
	// TrackingSince : premier événement enregistré pour ces franchises
	// (YYYY-MM-DD), nil si aucun : avant, rien n'était mesuré.
	TrackingSince *string `json:"tracking_since"`
}

type ScanNOrderDay struct {
	Date        string `json:"date"`
	SNOOrders   int64  `json:"sno_orders"`
	HubOrders   int64  `json:"hub_orders"`
	OrderClicks int64  `json:"order_clicks"`
}

type ScanNOrderMerchantRow struct {
	MerchantID         string `json:"merchant_id"`
	SNOOrders          int64  `json:"sno_orders"`
	SNORevenueTTCCents int64  `json:"sno_revenue_ttc_cents"`
	OrderClicks        int64  `json:"order_clicks"`
	ReservationClicks  int64  `json:"reservation_clicks"`
	HubOrders          int64  `json:"hub_orders"`
	HubRevenueTTCCents int64  `json:"hub_revenue_ttc_cents"`
}

type ScanNOrderAnalyticsResponse struct {
	Scope      RevenueScope            `json:"scope"`
	Totals     ScanNOrderTotals        `json:"totals"`
	Hub        ScanNOrderHubTotals     `json:"hub"`
	Daily      []ScanNOrderDay         `json:"daily"`
	ByMerchant []ScanNOrderMerchantRow `json:"by_merchant"`
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

	data, err := s.repo.GetScanNOrderStats(ctx, merchantIDs, startUTC, endUTC, tzString)
	if err != nil {
		return nil, err
	}

	resp := &ScanNOrderAnalyticsResponse{
		Scope:  RevenueScope{MerchantIDs: merchantIDs},
		Totals: data.Totals,
		Hub:    data.Hub,
	}

	// Une ligne par établissement du périmètre, même sans activité.
	byMerchant := map[string]*ScanNOrderMerchantRow{}
	for _, id := range merchantIDs {
		byMerchant[id] = &ScanNOrderMerchantRow{MerchantID: id}
	}
	for _, row := range data.ByMerchant {
		if target, ok := byMerchant[row.MerchantID]; ok {
			*target = row
		}
	}
	for _, id := range merchantIDs {
		row := byMerchant[id]
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
	Totals     ScanNOrderTotals
	Hub        ScanNOrderHubTotals
	Daily      []ScanNOrderDay
	ByMerchant []ScanNOrderMerchantRow
}

// GetScanNOrderStats : commandes valides au sens de AnalyticsOrdersScope (même
// périmètre que les onglets CA / Commandes), commandes attribuées au hub
// (franchise_hub_orders) et événements du hub (franchise_hub_events).
func (r *Repository) GetScanNOrderStats(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time, tz string) (*scanNOrderStats, error) {
	out := &scanNOrderStats{Hub: ScanNOrderHubTotals{Franchises: []string{}}}
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	sno := fmt.Sprintf("o.order_source = '%s'", OrderSourceScanNOrder)

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
		rows, err := db.QueryContext(ctx, `
			SELECT o.merchant_id,
			       COUNT(*) FILTER (WHERE `+sno+`),
			       COALESCE(SUM(o.price) FILTER (WHERE `+sno+`), 0),
			       COUNT(h.order_id),
			       COALESCE(SUM(o.price) FILTER (WHERE h.order_id IS NOT NULL), 0)
			FROM orders o
			LEFT JOIN franchise_hub_orders h ON h.order_id = o.order_id
			WHERE `+where+`
			GROUP BY o.merchant_id`, args...)
		if err != nil {
			return fmt.Errorf("scannorder by merchant: %w", err)
		}
		byMerchant := map[string]*ScanNOrderMerchantRow{}
		for rows.Next() {
			row := &ScanNOrderMerchantRow{}
			if err := rows.Scan(&row.MerchantID, &row.SNOOrders, &row.SNORevenueTTCCents, &row.HubOrders, &row.HubRevenueTTCCents); err != nil {
				rows.Close()
				return err
			}
			byMerchant[row.MerchantID] = row
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Clics du hub par établissement.
		rows, err = db.QueryContext(ctx, `
			SELECT merchant_id, event_type, COUNT(*)
			FROM franchise_hub_events
			WHERE merchant_id = ANY(?) AND created_at >= ? AND created_at < ?
			  AND event_type IN ('order_click', 'reservation_click')
			GROUP BY merchant_id, event_type`, merchantIDs, startUTC, endUTC)
		if err != nil {
			return fmt.Errorf("hub clicks: %w", err)
		}
		for rows.Next() {
			var merchantID, eventType string
			var n int64
			if err := rows.Scan(&merchantID, &eventType, &n); err != nil {
				rows.Close()
				return err
			}
			row, ok := byMerchant[merchantID]
			if !ok {
				row = &ScanNOrderMerchantRow{MerchantID: merchantID}
				byMerchant[merchantID] = row
			}
			if eventType == "order_click" {
				row.OrderClicks = n
			} else {
				row.ReservationClicks = n
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, row := range byMerchant {
			out.ByMerchant = append(out.ByMerchant, *row)
		}
		sort.Slice(out.ByMerchant, func(i, j int) bool { return out.ByMerchant[i].MerchantID < out.ByMerchant[j].MerchantID })

		// Franchises du périmètre, affichages de leurs hubs, début de la mesure.
		rows, err = db.QueryContext(ctx, `
			SELECT DISTINCT f.name
			FROM franchises f
			INNER JOIN merchant m ON m.franchise_id = f.franchise_id
			WHERE CAST(m.id AS TEXT) = ANY(?)
			ORDER BY f.name`, merchantIDs)
		if err != nil {
			return fmt.Errorf("scope franchises: %w", err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			out.Hub.Franchises = append(out.Hub.Franchises, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		out.Hub.InFranchise = len(out.Hub.Franchises) > 0

		if out.Hub.InFranchise {
			var since *time.Time
			if err := db.QueryRowContext(ctx, `
				SELECT COUNT(*) FILTER (WHERE e.event_type = 'view' AND e.created_at >= ? AND e.created_at < ?),
				       MIN(e.created_at)
				FROM franchise_hub_events e
				WHERE e.franchise_id IN (
					SELECT m.franchise_id FROM merchant m
					WHERE CAST(m.id AS TEXT) = ANY(?) AND m.franchise_id IS NOT NULL
				)`, startUTC, endUTC, merchantIDs).Scan(&out.Hub.Views, &since); err != nil {
				return fmt.Errorf("hub views: %w", err)
			}
			if since != nil {
				loc, err := time.LoadLocation(tz)
				if err != nil {
					loc = time.UTC
				}
				d := since.In(loc).Format("2006-01-02")
				out.Hub.TrackingSince = &d
			}
		}

		// Série quotidienne (jour local de l'établissement).
		dailyArgs := append([]interface{}{tz}, args...)
		rows, err = db.QueryContext(ctx, `
			SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
			       COUNT(*) FILTER (WHERE `+sno+`),
			       COUNT(h.order_id)
			FROM orders o
			LEFT JOIN franchise_hub_orders h ON h.order_id = o.order_id
			WHERE `+where+`
			GROUP BY day`, dailyArgs...)
		if err != nil {
			return fmt.Errorf("scannorder daily: %w", err)
		}
		days := map[string]*ScanNOrderDay{}
		for rows.Next() {
			d := &ScanNOrderDay{}
			if err := rows.Scan(&d.Date, &d.SNOOrders, &d.HubOrders); err != nil {
				rows.Close()
				return err
			}
			days[d.Date] = d
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = db.QueryContext(ctx, `
			SELECT to_char(created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day, COUNT(*)
			FROM franchise_hub_events
			WHERE merchant_id = ANY(?) AND created_at >= ? AND created_at < ? AND event_type = 'order_click'
			GROUP BY day`, tz, merchantIDs, startUTC, endUTC)
		if err != nil {
			return fmt.Errorf("hub clicks daily: %w", err)
		}
		for rows.Next() {
			var day string
			var n int64
			if err := rows.Scan(&day, &n); err != nil {
				rows.Close()
				return err
			}
			d, ok := days[day]
			if !ok {
				d = &ScanNOrderDay{Date: day}
				days[day] = d
			}
			d.OrderClicks = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
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

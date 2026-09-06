package stats

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type StatsService struct {
	statsRepo *StatsRepository
}

func NewStatsService(repo *StatsRepository) *StatsService {
	return &StatsService{statsRepo: repo}
}

// GetDashboardSummary returns dashboard statistics from the database
func (s *StatsService) GetDashboardSummary(ctx context.Context, token string) (*DashboardSummaryResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID

	// 1ï¸âƒ£ Load merchant timezone
	tzString, err := s.statsRepo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to load merchant timezone: %w", err)
	}

	// 2ï¸âƒ£ Parse timezone string
	merchantTz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone '%s': %w", tzString, err)
	}

	// 3ï¸âƒ£ Get current time in merchant timezone
	nowUTC := time.Now().UTC()
	nowInMerchantTz := nowUTC.In(merchantTz)

	// 4ï¸âƒ£ Get revenue data (passing timezone and time in merchant timezone)
	revToday, revYesterday, revWeekCurrent, revWeekPrevious, revMonthCurrent, revMonthPrevious, err := s.statsRepo.GetRevenue(ctx, merchantID, merchantTz, nowInMerchantTz)
	if err != nil {
		return nil, fmt.Errorf("failed to get revenue: %w", err)
	}

	// Get order count
	ordersToday, ordersYesterday, err := s.statsRepo.GetOrderCount(ctx, merchantID, merchantTz, nowInMerchantTz)
	if err != nil {
		return nil, fmt.Errorf("failed to get order count: %w", err)
	}

	// Get average basket
	avgBasketToday, avgBasketYesterday, err := s.statsRepo.GetAverageBasket(ctx, merchantID, merchantTz, nowInMerchantTz)
	if err != nil {
		return nil, fmt.Errorf("failed to get average basket: %w", err)
	}

	// Get hourly data
	hourlyRevenue, hourlyOrders, err := s.statsRepo.GetHourlyData(ctx, merchantID, merchantTz, nowInMerchantTz)
	if err != nil {
		return nil, fmt.Errorf("failed to get hourly data: %w", err)
	}

	// Build hourly data responses
	hourlyRevenueData := s.buildHourlyMetric(hourlyRevenue)
	hourlyOrdersData := s.buildHourlyMetric(hourlyOrders)

	summary := &DashboardSummaryResponse{
		KPIs: KPIData{
			Revenue: RevenueKPI{
				Today:     revToday,
				Yesterday: revYesterday,
				Currency:  "EUR",
				Week: PeriodData{
					Current:        revWeekCurrent,
					PreviousPeriod: revWeekPrevious,
				},
				Month: PeriodData{
					Current:        revMonthCurrent,
					PreviousPeriod: revMonthPrevious,
				},
			},
			AvgBasket: BasketKPI{
				Today:     avgBasketToday,
				Yesterday: avgBasketYesterday,
				Currency:  "EUR",
			},
			Orders: OrdersKPI{
				Today:     ordersToday,
				Yesterday: ordersYesterday,
			},
		},
		HourlyRevenue: hourlyRevenueData,
		HourlyOrders:  hourlyOrdersData,
	}

	return summary, nil
}

// GetUpsellStats returns upsell impact stats and the by-server ranking for the given local day range.
// NOTE: orderitems.is_upsell is written by all three ordering channels (POS, Kiosk, ScanNOrder) as of
// 2026-09 — zeros here reflect real usage volume (upsell enabled for very few merchants, see
// docs/audits/2026-09-05-upsell-diagnostic-prompt20.md), not a missing write path.
func (s *StatsService) GetUpsellStats(ctx context.Context, token string, fromLocalDay, toLocalDay time.Time) (*UpsellStatsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID

	tzString, err := s.statsRepo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to load merchant timezone: %w", err)
	}

	merchantTz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone '%s': %w", tzString, err)
	}

	fromDate := time.Date(fromLocalDay.Year(), fromLocalDay.Month(), fromLocalDay.Day(), 0, 0, 0, 0, merchantTz)
	toDate := time.Date(toLocalDay.Year(), toLocalDay.Month(), toLocalDay.Day(), 0, 0, 0, 0, merchantTz)
	startUTC := fromDate.UTC()
	endUTC := toDate.Add(24 * time.Hour).UTC()

	totals, err := s.statsRepo.GetUpsellTotals(ctx, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("failed to get upsell totals: %w", err)
	}

	ordersWithUpsell, err := s.statsRepo.GetOrdersWithUpsellCount(ctx, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("failed to get orders with upsell count: %w", err)
	}

	totalOrders, err := s.statsRepo.getOrderCountForPeriod(ctx, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("failed to get total order count: %w", err)
	}

	var rate float64
	if totalOrders > 0 {
		rate = float64(ordersWithUpsell) / float64(totalOrders) * 100
	}

	byServerRows, err := s.statsRepo.ListUpsellByServer(ctx, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("failed to list upsell by server: %w", err)
	}

	byServer := make([]UpsellServerStat, 0, len(byServerRows))
	for _, row := range byServerRows {
		byServer = append(byServer, UpsellServerStat{
			ServerID:        row.ServerID,
			ServerName:      row.ServerName,
			UpsellLines:     row.UpsellLines,
			UpsellRevenueHT: row.UpsellRevenueHT,
		})
	}

	return &UpsellStatsResponse{
		TotalUpsellLines:     totals.TotalLines,
		UpsellRevenueHT:      totals.RevenueHTCents,
		OrdersWithUpsellRate: rate,
		ByServer:             byServer,
	}, nil
}

// buildHourlyMetric converts raw hourly data into HourlyMetric response format
func (s *StatsService) buildHourlyMetric(rawData []map[string]interface{}) []HourlyMetric {
	hourlyMap := make(map[int]*HourlyMetric)

	// Initialize all hours from 0 to 23
	for i := 0; i < 24; i++ {
		hourlyMap[i] = &HourlyMetric{
			Hour:      fmt.Sprintf("%02d:00", i),
			SurPlace:  0,
			Emporter:  0,
			Livraison: 0,
			UberEats:  0,
			Deliveroo: 0,
			Total:     0,
		}
	}

	// Fill in data from query results
	for _, row := range rawData {
		hour := row["hour"].(int)
		if _, exists := hourlyMap[hour]; !exists {
			hourlyMap[hour] = &HourlyMetric{
				Hour: fmt.Sprintf("%02d:00", hour),
			}
		}

		hourlyMap[hour].SurPlace = getInt64Value(row["sur_place"])
		hourlyMap[hour].Emporter = getInt64Value(row["emporter"])
		hourlyMap[hour].Livraison = getInt64Value(row["livraison"])
		hourlyMap[hour].UberEats = getInt64Value(row["uber_eats"])
		hourlyMap[hour].Deliveroo = getInt64Value(row["deliveroo"])
		hourlyMap[hour].Total = getInt64Value(row["total"])
	}

	// Convert map to sorted slice
	result := make([]HourlyMetric, 0, 24)
	for i := 0; i < 24; i++ {
		result = append(result, *hourlyMap[i])
	}

	return result
}

// getInt64Value safely extracts int64 value from interface{}
func getInt64Value(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	default:
		return 0
	}
}

// getIntValue safely extracts int value from interface{}
func getIntValue(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}

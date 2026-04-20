package stats

import (
	"context"
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

// GetDashboardSummary retourne les statistiques du dashboard (mockup pour maintenant)
func (s *StatsService) GetDashboardSummary(ctx context.Context, token string) (*DashboardSummary, error) {
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	// Générer les données mockées
	satisfaction := 4.5

	summary := &DashboardSummary{
		KPIs: KPIs{
			Revenue: RevenueKPI{
				Today:            1250.50,
				Yesterday:        1180.75,
				SameDayLastWeek:  1320.00,
				SameDayLastYear:  950.25,
				Currency:         "EUR",
				TargetDay:        1500.00,
				TargetWeek:       9000.00,
				TargetMonth:      35000.00,
				InProgressAmount: 450.00,
				Week: PeriodData{
					Current:            8750.50,
					PreviousPeriod:     8200.00,
					SamePeriodLastYear: 7850.00,
				},
				Month: PeriodData{
					Current:            32500.75,
					PreviousPeriod:     30200.50,
					SamePeriodLastYear: 28500.00,
				},
			},
			AvgTicket: TicketKPI{
				Today:           31.25,
				Yesterday:       29.50,
				SameDayLastWeek: 33.00,
				SameDayLastYear: 23.75,
				Currency:        "EUR",
			},
			AvgBasket: BasketKPI{
				Today:           62.50,
				Yesterday:       59.00,
				SameDayLastWeek: 66.00,
				SameDayLastYear: 47.50,
				Currency:        "EUR",
			},
			Orders: OrdersKPI{
				Today:           40,
				Yesterday:       40,
				SameDayLastWeek: 40,
				SameDayLastYear: 40,
				InProgress:      8,
				CancelledToday:  2,
			},
		},
		Channels: ChannelsData{
			TotalOrders:  156,
			TotalRevenue: 4562.75,
			Channels: []ChannelMetric{
				{
					Channel:                   "sur_place",
					Label:                     "Sur place",
					Orders:                    62,
					Revenue:                   2125.50,
					AvgPreparationTimeMinutes: 12,
					TrendVsYesterdayPct:       5.2,
				},
				{
					Channel:                   "emporter",
					Label:                     "À emporter",
					Orders:                    38,
					Revenue:                   980.75,
					AvgPreparationTimeMinutes: 8,
					TrendVsYesterdayPct:       -2.5,
				},
				{
					Channel:                   "livraison",
					Label:                     "Livraison",
					Orders:                    28,
					Revenue:                   820.50,
					AvgPreparationTimeMinutes: 25,
					TrendVsYesterdayPct:       3.1,
				},
				{
					Channel:                   "uber_eats",
					Label:                     "Uber Eats",
					Orders:                    16,
					Revenue:                   450.00,
					AvgPreparationTimeMinutes: 15,
					TrendVsYesterdayPct:       8.5,
				},
				{
					Channel:                   "deliveroo",
					Label:                     "Deliveroo",
					Orders:                    12,
					Revenue:                   186.00,
					AvgPreparationTimeMinutes: 18,
					TrendVsYesterdayPct:       -1.2,
				},
			},
		},
		TopProducts: []TopProduct{
			{
				Rank:             1,
				ID:               "prod-001",
				Name:             "Burger Premium",
				Category:         "Burgers",
				QuantitySold:     45,
				Revenue:          675.00,
				TrendVsYesterday: "up",
				TrendPercentage:  12.5,
				OutOfStock:       false,
			},
			{
				Rank:             2,
				ID:               "prod-002",
				Name:             "Pizza Margherita",
				Category:         "Pizzas",
				QuantitySold:     38,
				Revenue:          380.00,
				TrendVsYesterday: "stable",
				TrendPercentage:  0.0,
				OutOfStock:       false,
			},
			{
				Rank:             3,
				ID:               "prod-003",
				Name:             "Salade Verte",
				Category:         "Salades",
				QuantitySold:     32,
				Revenue:          224.00,
				TrendVsYesterday: "down",
				TrendPercentage:  -5.2,
				OutOfStock:       true,
			},
			{
				Rank:             4,
				ID:               "prod-004",
				Name:             "Pâtes Carbonara",
				Category:         "Pâtes",
				QuantitySold:     28,
				Revenue:          336.00,
				TrendVsYesterday: "up",
				TrendPercentage:  8.3,
				OutOfStock:       false,
			},
			{
				Rank:             5,
				ID:               "prod-005",
				Name:             "Dessert Chocolat",
				Category:         "Desserts",
				QuantitySold:     25,
				Revenue:          150.00,
				TrendVsYesterday: "up",
				TrendPercentage:  15.0,
				OutOfStock:       false,
			},
		},
		Hourly: []HourlyData{
			{Hour: "08:00", SurPlace: 2, Emporter: 3, Livraison: 1, UberEats: 0, Deliveroo: 0, Total: 6},
			{Hour: "09:00", SurPlace: 5, Emporter: 4, Livraison: 2, UberEats: 1, Deliveroo: 0, Total: 12},
			{Hour: "10:00", SurPlace: 8, Emporter: 6, Livraison: 3, UberEats: 2, Deliveroo: 1, Total: 20},
			{Hour: "11:00", SurPlace: 12, Emporter: 8, Livraison: 5, UberEats: 3, Deliveroo: 2, Total: 30},
			{Hour: "12:00", SurPlace: 15, Emporter: 10, Livraison: 8, UberEats: 4, Deliveroo: 2, Total: 39},
			{Hour: "13:00", SurPlace: 10, Emporter: 5, Livraison: 4, UberEats: 2, Deliveroo: 1, Total: 22},
			{Hour: "14:00", SurPlace: 6, Emporter: 3, Livraison: 2, UberEats: 1, Deliveroo: 1, Total: 13},
			{Hour: "18:00", SurPlace: 8, Emporter: 6, Livraison: 3, UberEats: 2, Deliveroo: 1, Total: 20},
			{Hour: "19:00", SurPlace: 14, Emporter: 8, Livraison: 6, UberEats: 3, Deliveroo: 1, Total: 32},
			{Hour: "20:00", SurPlace: 16, Emporter: 7, Livraison: 5, UberEats: 2, Deliveroo: 2, Total: 32},
		},
		Activity: []ActivityLog{
			{
				ID:      "act-001",
				Type:    "ORDER_CREATED",
				Message: "Commande #12345 créée",
				Value:   nil,
				Time:    time.Now().UTC().Format(time.RFC3339),
				User:    stringPtr("Alice"),
			},
			{
				ID:      "act-002",
				Type:    "ORDER_PAYMENT",
				Message: "Paiement reçu - Commande #12340",
				Value:   stringPtr("250.50 EUR"),
				Time:    time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339),
				User:    stringPtr("Bob"),
			},
			{
				ID:      "act-003",
				Type:    "DELIVERY",
				Message: "Livraison #LIV-567 complétée",
				Value:   nil,
				Time:    time.Now().UTC().Add(-15 * time.Minute).Format(time.RFC3339),
				User:    stringPtr("Charlie"),
			},
			{
				ID:      "act-004",
				Type:    "STOCK_ALERT",
				Message: "Stock faible : Tomates",
				Value:   stringPtr("5 unités restantes"),
				Time:    time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
				User:    nil,
			},
			{
				ID:      "act-005",
				Type:    "Z_REPORT",
				Message: "Z-Report généré",
				Value:   stringPtr("1250.50 EUR"),
				Time:    time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339),
				User:    stringPtr("Admin"),
			},
		},
		Alerts: AlertsData{
			LowStockCount:      3,
			LowStockItems:      []string{"Tomates", "Mozzarella", "Huile d'olive"},
			VoidedOrders:       2,
			PendingDeliveries:  5,
			CashRegisterAlerts: 1,
			UnpaidOrders:       3,
		},
		Service: ServiceMetrics{
			AvgPreparationTimeMinutes: 14,
			AvgTableTimeMinutes:       45,
			TablesOccupied:            8,
			TablesTotal:               12,
			CoversToday:               62,
			CoversTarget:              80,
			SatisfactionRate:          &satisfaction,
		},
	}

	return summary, nil
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

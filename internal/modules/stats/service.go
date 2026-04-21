package stats

import (
	"context"
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
func (s *StatsService) GetDashboardSummary(ctx context.Context, token string) (*DashboardSummaryResponse, error) {
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	summary := &DashboardSummaryResponse{
		KPIs: KPIData{
			Revenue: RevenueKPI{
				Today:     12480,
				Yesterday: 10920,
				Currency:  "EUR",
				Week: PeriodData{
					Current:        87500,
					PreviousPeriod: 82200,
				},
				Month: PeriodData{
					Current:        385200,
					PreviousPeriod: 371600,
				},
			},
			AvgBasket: BasketKPI{
				Today:     1975,
				Yesterday: 1790,
				Currency:  "EUR",
			},
			Orders: OrdersKPI{
				Today:     439,
				Yesterday: 398,
			},
		},
		Hourly: []HourlyData{
			{Hour: "10:00", SurPlace: 0, Emporter: 120, Livraison: 80, UberEats: 60, Deliveroo: 40, Total: 300},
			{Hour: "11:00", SurPlace: 180, Emporter: 210, Livraison: 130, UberEats: 90, Deliveroo: 50, Total: 660},
			{Hour: "12:00", SurPlace: 980, Emporter: 560, Livraison: 310, UberEats: 280, Deliveroo: 180, Total: 2310},
			{Hour: "13:00", SurPlace: 1420, Emporter: 720, Livraison: 390, UberEats: 340, Deliveroo: 210, Total: 3080},
			{Hour: "14:00", SurPlace: 640, Emporter: 380, Livraison: 210, UberEats: 190, Deliveroo: 120, Total: 1540},
			{Hour: "15:00", SurPlace: 120, Emporter: 150, Livraison: 90, UberEats: 80, Deliveroo: 40, Total: 480},
			{Hour: "16:00", SurPlace: 80, Emporter: 110, Livraison: 60, UberEats: 50, Deliveroo: 30, Total: 330},
			{Hour: "17:00", SurPlace: 60, Emporter: 90, Livraison: 40, UberEats: 40, Deliveroo: 20, Total: 250},
			{Hour: "18:00", SurPlace: 280, Emporter: 200, Livraison: 120, UberEats: 110, Deliveroo: 70, Total: 780},
			{Hour: "19:00", SurPlace: 820, Emporter: 410, Livraison: 280, UberEats: 240, Deliveroo: 140, Total: 1890},
			{Hour: "20:00", SurPlace: 1180, Emporter: 530, Livraison: 350, UberEats: 310, Deliveroo: 170, Total: 2540},
			{Hour: "21:00", SurPlace: 850, Emporter: 320, Livraison: 210, UberEats: 180, Deliveroo: 110, Total: 1670},
		},
	}

	return summary, nil
}

package stats

// ============ Dashboard Summary Models (Nouvelle structure simplifiée) ============

// DashboardSummaryResponse est la structure de réponse du dashboard
type DashboardSummaryResponse struct {
	KPIs          KPIData        `json:"kpis"`
	HourlyRevenue []HourlyMetric `json:"hourly_revenue"`
	HourlyOrders  []HourlyMetric `json:"hourly_orders"`
}

// KPIData contient les indicateurs clés de performance
type KPIData struct {
	Revenue   RevenueKPI `json:"revenue"`
	AvgBasket BasketKPI  `json:"avg_basket"`
	Orders    OrdersKPI  `json:"orders"`
}

// RevenueKPI contient les données de chiffre d'affaires
type RevenueKPI struct {
	Today     int64      `json:"today"`
	Yesterday int64      `json:"yesterday"`
	Currency  string     `json:"currency"`
	Week      PeriodData `json:"week"`
	Month     PeriodData `json:"month"`
}

// PeriodData contient les données comparées entre deux périodes
type PeriodData struct {
	Current        int64 `json:"current"`
	PreviousPeriod int64 `json:"previous_period"`
}

// BasketKPI contient les données du panier moyen
type BasketKPI struct {
	Today     int64  `json:"today"`
	Yesterday int64  `json:"yesterday"`
	Currency  string `json:"currency"`
}

// OrdersKPI contient les données des commandes
type OrdersKPI struct {
	Today     int `json:"today"`
	Yesterday int `json:"yesterday"`
}

// HourlyMetric contient les données horaires (revenue ou orders)
type HourlyMetric struct {
	Hour      string `json:"hour"`
	SurPlace  int64  `json:"sur_place"`
	Emporter  int64  `json:"emporter"`
	Livraison int64  `json:"livraison"`
	UberEats  int64  `json:"uber_eats"`
	Deliveroo int64  `json:"deliveroo"`
	Total     int64  `json:"total"`
}

// HourlyData deprecated - use HourlyMetric instead
type HourlyData struct {
	Hour      string `json:"hour"`
	SurPlace  int    `json:"sur_place"`
	Emporter  int    `json:"emporter"`
	Livraison int    `json:"livraison"`
	UberEats  int    `json:"uber_eats"`
	Deliveroo int    `json:"deliveroo"`
	Total     int    `json:"total"`
}

package stats

// ============ Dashboard Summary Models ============

type DashboardSummary struct {
	KPIs        KPIs           `json:"kpis"`
	Channels    ChannelsData   `json:"channels"`
	TopProducts []TopProduct   `json:"top_products"`
	Hourly      []HourlyData   `json:"hourly"`
	Activity    []ActivityLog  `json:"activity"`
	Alerts      AlertsData     `json:"alerts"`
	Service     ServiceMetrics `json:"service"`
}

type RevenueKPI struct {
	Today            float64    `json:"today"`
	Yesterday        float64    `json:"yesterday"`
	SameDayLastWeek  float64    `json:"same_day_last_week"`
	SameDayLastYear  float64    `json:"same_day_last_year"`
	Currency         string     `json:"currency"`
	TargetDay        float64    `json:"target_day"`
	TargetWeek       float64    `json:"target_week"`
	TargetMonth      float64    `json:"target_month"`
	InProgressAmount float64    `json:"in_progress_amount"`
	Week             PeriodData `json:"week"`
	Month            PeriodData `json:"month"`
}

type PeriodData struct {
	Current            float64 `json:"current"`
	PreviousPeriod     float64 `json:"previous_period"`
	SamePeriodLastYear float64 `json:"same_period_last_year"`
}

type TicketKPI struct {
	Today           float64 `json:"today"`
	Yesterday       float64 `json:"yesterday"`
	SameDayLastWeek float64 `json:"same_day_last_week"`
	SameDayLastYear float64 `json:"same_day_last_year"`
	Currency        string  `json:"currency"`
}

type BasketKPI struct {
	Today           float64 `json:"today"`
	Yesterday       float64 `json:"yesterday"`
	SameDayLastWeek float64 `json:"same_day_last_week"`
	SameDayLastYear float64 `json:"same_day_last_year"`
	Currency        string  `json:"currency"`
}

type OrdersKPI struct {
	Today           int `json:"today"`
	Yesterday       int `json:"yesterday"`
	SameDayLastWeek int `json:"same_day_last_week"`
	SameDayLastYear int `json:"same_day_last_year"`
	InProgress      int `json:"in_progress"`
	CancelledToday  int `json:"cancelled_today"`
}

type KPIs struct {
	Revenue   RevenueKPI `json:"revenue"`
	AvgTicket TicketKPI  `json:"avg_ticket"`
	AvgBasket BasketKPI  `json:"avg_basket"`
	Orders    OrdersKPI  `json:"orders"`
}

type ChannelMetric struct {
	Channel                   string  `json:"channel"`
	Label                     string  `json:"label"`
	Orders                    int     `json:"orders"`
	Revenue                   float64 `json:"revenue"`
	AvgPreparationTimeMinutes float64 `json:"avg_preparation_time_minutes"`
	TrendVsYesterdayPct       float64 `json:"trend_vs_yesterday_pct"`
}

type ChannelsData struct {
	TotalOrders  int             `json:"total_orders"`
	TotalRevenue float64         `json:"total_revenue"`
	Channels     []ChannelMetric `json:"channels"`
}

type TopProduct struct {
	Rank             int     `json:"rank"`
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Category         string  `json:"category"`
	QuantitySold     int     `json:"quantity_sold"`
	Revenue          float64 `json:"revenue"`
	TrendVsYesterday string  `json:"trend_vs_yesterday"`
	TrendPercentage  float64 `json:"trend_percentage"`
	OutOfStock       bool    `json:"out_of_stock"`
}

type HourlyData struct {
	Hour      string `json:"hour"`
	SurPlace  int    `json:"sur_place"`
	Emporter  int    `json:"emporter"`
	Livraison int    `json:"livraison"`
	UberEats  int    `json:"uber_eats"`
	Deliveroo int    `json:"deliveroo"`
	Total     int    `json:"total"`
}

type ActivityLog struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Message string  `json:"message"`
	Value   *string `json:"value"`
	Time    string  `json:"time"`
	User    *string `json:"user,omitempty"`
}

type AlertsData struct {
	LowStockCount      int      `json:"low_stock_count"`
	LowStockItems      []string `json:"low_stock_items"`
	VoidedOrders       int      `json:"voided_orders"`
	PendingDeliveries  int      `json:"pending_deliveries"`
	CashRegisterAlerts int      `json:"cash_register_alerts"`
	UnpaidOrders       int      `json:"unpaid_orders"`
}

type ServiceMetrics struct {
	AvgPreparationTimeMinutes int      `json:"avg_preparation_time_minutes"`
	AvgTableTimeMinutes       int      `json:"avg_table_time_minutes"`
	TablesOccupied            int      `json:"tables_occupied"`
	TablesTotal               int      `json:"tables_total"`
	CoversToday               int      `json:"covers_today"`
	CoversTarget              int      `json:"covers_target"`
	SatisfactionRate          *float64 `json:"satisfaction_rate"`
}

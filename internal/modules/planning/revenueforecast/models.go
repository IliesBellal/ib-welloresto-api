package revenueforecast

type ForecastInput struct {
	Date        string `json:"date"`
	AmountCents *int64 `json:"amount_cents"`
}

type UpsertRevenueForecastsRequest struct {
	Forecasts []ForecastInput `json:"forecasts"`
}

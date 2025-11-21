package models

type OrderHistoryRequest struct {
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}

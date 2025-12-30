package models

type OrdersHandlerResponse struct {
	ID   int               `json:"id"`
	Data PendingOrdersData `json:"data"`
}

type PendingOrdersData struct {
	Orders []Order `json:"orders"`
}

type MenuHandlerResponse struct {
	ID   int           `json:"id"`
	Data *MenuResponse `json:"data"`
}

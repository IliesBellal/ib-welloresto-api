package models

type PendingOrdersHandlerResponse struct {
	ID   int               `json:"id"`
	Data PendingOrdersData `json:"data"`
}

type PendingOrdersData struct {
	Orders []Order `json:"orders"`
}

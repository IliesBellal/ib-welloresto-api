package models

type PendingOrdersData struct {
	Orders []Order `json:"orders"`
}

type OpenCashRegisterData struct {
	Status string `json:"status"`
}

type HandlerDefaultResponse struct {
	ID   string      `json:"id"`
	Data interface{} `json:"data"`
}

type HandlerDefaultResponseModelSet struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data1  string `json:"data1,omitempty"`
}

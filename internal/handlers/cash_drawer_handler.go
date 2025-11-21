package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/services"
)

// CashDrawerHandler handles orders endpoints
type CashDrawerHandler struct {
	cashDrawerService *services.CashDrawerService
}

func NewCashDrawerHandler(cashDrawerService *services.CashDrawerService) *CashDrawerHandler {
	return &CashDrawerHandler{
		cashDrawerService: cashDrawerService,
	}
}

func (h *CashDrawerHandler) OpenCashDrawer(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "1",
	})
}

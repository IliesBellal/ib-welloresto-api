package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

type StocksHandler struct {
	stockSvc *services.StocksService
	usersSvc *services.UsersService
}

func NewStocksHandler(stocks *services.StocksService, users *services.UsersService) *StocksHandler {
	return &StocksHandler{
		stockSvc: stocks,
		usersSvc: users,
	}
}

// GET /stock/barcode/{code}
func (h *StocksHandler) GetBarcodeInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	code := chi.URLParam(r, "barcode_id")

	resp, err := h.stockSvc.GetBarcodeInfo(ctx, token, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

// DELETE /stock/barcode/{code}
func (h *StocksHandler) DeleteBarcode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	code := chi.URLParam(r, "barcode_id")

	err := h.stockSvc.DeleteBarcode(ctx, token, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

func (h *StocksHandler) CreateBarcode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	var p models.CreateBarcodePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := h.stockSvc.CreateBarcode(ctx, token, p.Code, p.ComponentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": 1,
	})
}

// POST /stock/barcodes/scan
func (h *StocksHandler) AddStockBarcode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// token -> user (merchantID + userID)
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	var payload struct {
		Barcode string              `json:"barcode"`
		Specs   models.BarcodeSpecs `json:"specs"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// call service
	if err := h.stockSvc.AddStockBarcode(ctx, token, payload.Barcode, payload.Specs); err != nil {
		// h.log.Error("AddStockBarcode failed", zap.Error(err))
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "1"})
}

func (h *StocksHandler) SetStockLoss(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	var req models.StockLossRequest
	json.NewDecoder(r.Body).Decode(&req)

	err := h.stockSvc.SetStockLoss(ctx, token, req)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"status": 1})
}

func (h *StocksHandler) GetStockProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	objectType := r.URL.Query().Get("type")
	res, err := h.stockSvc.GetStockProducts(ctx, token, objectType)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	json.NewEncoder(w).Encode(res)
}

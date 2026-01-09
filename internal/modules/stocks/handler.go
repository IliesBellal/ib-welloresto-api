package stocks

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type StocksHandler struct {
	stockSvc *StocksService
}

func NewStocksHandler(stocks *StocksService) *StocksHandler {
	return &StocksHandler{
		stockSvc: stocks,
	}
}

// GET /stock/barcode/{code}
func (h *StocksHandler) GetBarcodeInfo(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	code := chi.URLParam(r, "barcode")

	resp, err := h.stockSvc.GetBarcodeInfo(ctx, token, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

// DELETE /stock/barcode/{code}
func (h *StocksHandler) DeleteBarcode(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	code := chi.URLParam(r, "barcode")

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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	var p models.CreateBarcodePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := h.stockSvc.CreateBarcode(ctx, token, p.Barcode, p.ComponentID)
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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	objectType := r.URL.Query().Get("type")
	res, err := h.stockSvc.GetStockProducts(ctx, token, objectType)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	json.NewEncoder(w).Encode(res)
}

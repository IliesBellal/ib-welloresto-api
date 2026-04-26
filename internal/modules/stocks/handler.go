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
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "get_barcode_info", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	code := chi.URLParam(r, "barcode")

	resp, err := h.stockSvc.GetBarcodeInfo(ctx, token, code)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "get_barcode_info", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "get_barcode_info", resp)
}

// DELETE /stock/barcode/{code}
func (h *StocksHandler) DeleteBarcode(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "delete_barcode", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	code := chi.URLParam(r, "barcode")

	err := h.stockSvc.DeleteBarcode(ctx, token, code)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "delete_barcode", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "delete_barcode", map[string]interface{}{
		"status": "ok",
	})
}

func (h *StocksHandler) CreateBarcode(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "create_barcode", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var p models.CreateBarcodePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "stocks", "create_barcode", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.stockSvc.CreateBarcode(ctx, token, p.Barcode, p.ComponentID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "create_barcode", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "create_barcode", map[string]interface{}{
		"status": 1,
	})
}

// POST /stock/barcodes/scan
func (h *StocksHandler) AddStockBarcode(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "add_stock_barcode", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var payload struct {
		Barcode string              `json:"barcode"`
		Specs   models.BarcodeSpecs `json:"specs"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "stocks", "add_stock_barcode", map[string]string{"error": "invalid_body"})
		return
	}

	// call service
	if err := h.stockSvc.AddStockBarcode(ctx, token, payload.Barcode, payload.Specs); err != nil {
		// h.log.Error("AddStockBarcode failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "add_stock_barcode", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "add_stock_barcode", map[string]interface{}{"status": "1"})
}

func (h *StocksHandler) SetStockLoss(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "set_stock_loss", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.StockLossRequest
	json.NewDecoder(r.Body).Decode(&req)

	err := h.stockSvc.SetStockLoss(ctx, token, req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "set_stock_loss", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "set_stock_loss", map[string]any{"status": 1})
}

func (h *StocksHandler) GetStockProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "get_stock_products", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	objectType := r.URL.Query().Get("type")
	res, err := h.stockSvc.GetStockProducts(ctx, token, objectType)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "get_stock_products", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "get_stock_products", res)
}

func (h *StocksHandler) GetComponentsList(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stocks", "get_components_list", map[string]string{"error": "missing_token"})
		return
	}

	components, err := h.stockSvc.GetComponentsList(r.Context(), token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "get_components_list", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "get_components_list", map[string]interface{}{
		"components": components,
	})
}

// PUT /stocks/components/{component_id}
func (h *StocksHandler) RecordComponentMovement(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req StockComponentMovementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "stocks", "record_component_movement", map[string]string{"error": "invalid_body"})
		return
	}

	req.ComponentID = chi.URLParam(r, "component_id")

	err := h.stockSvc.RecordComponentMovement(ctx, req)
	if err != nil {
		switch err {
		case ErrComponentNotFound:
			models.SendJSON(w, http.StatusNotFound, "stocks", "record_component_movement", map[string]string{"error": "unknown_component"})
		case ErrUnitNotFound:
			models.SendJSON(w, http.StatusUnprocessableEntity, "stocks", "record_component_movement", map[string]string{"error": "unknown_unit"})
		case ErrInvalidMovement:
			models.SendJSON(w, http.StatusBadRequest, "stocks", "record_component_movement", map[string]string{"error": "invalid_type"})
		default:
			models.SendJSON(w, http.StatusInternalServerError, "stocks", "record_component_movement", map[string]string{"error": err.Error()})
		}
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "record_component_movement", map[string]interface{}{"status": "ok"})
}

// GET /stocks/movements?from=2026-04-26&to=2026-04-26
func (h *StocksHandler) GetMovements(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	if from == "" || to == "" {
		models.SendJSON(w, http.StatusBadRequest, "stocks", "movements", map[string]string{"error": "missing_from_or_to"})
		return
	}

	items, err := h.stockSvc.GetMovements(ctx, from, to)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stocks", "movements", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stocks", "movements", map[string]interface{}{
		"components": items,
	})
}

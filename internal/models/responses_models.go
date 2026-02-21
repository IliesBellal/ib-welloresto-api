package models

import (
	"encoding/json"
	"net/http"
)

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

// SendJSON envoie une réponse JSON standardisée avec la structure HandlerDefaultResponse
// Params:
//   - w: http.ResponseWriter
//   - module: nom du module (ex: "auth", "users", "pos")
//   - fnName: nom de la fonction handler (ex: "login", "get_profile")
//   - data: données à retourner (peut être nil)
func SendJSON(w http.ResponseWriter, module string, fnName string, data interface{}) {
	result := HandlerDefaultResponse{
		ID:   module + "." + fnName,
		Data: data,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

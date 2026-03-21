package models

import (
	"encoding/json"
	"errors"
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
//   - statusCode: Code HTTP (ex: http.StatusOK, http.StatusUnauthorized)
//   - module: nom du module (ex: "auth")
//   - fnName: nom de la fonction handler (ex: "login")
//   - data: données à retourner (peut être nil)
func SendJSON(w http.ResponseWriter, statusCode int, module string, fnName string, data interface{}) {
	result := HandlerDefaultResponse{
		ID:   module + "." + fnName,
		Data: data,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode) // Très important : doit être appelé APRÈS le header mais AVANT l'encode
	json.NewEncoder(w).Encode(result)
}

// Sentinel errors pour une gestion d'erreurs standardisée entre Services et Handlers
var (
	// ErrUnauthorized indique que l'utilisateur n'est pas authentifié (401)
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indique que l'utilisateur n'a pas les permissions nécessaires (403)
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound indique que la ressource demandée n'existe pas (404)
	ErrNotFound = errors.New("not found")

	// ErrInvalidInput indique que les données fournies sont invalides (400)
	ErrInvalidInput = errors.New("invalid input")

	// ErrInvalidInput indique que les données fournies sont invalides (400)
	ErrInvalidInputPasswordTooShort = errors.New("le mot de passe doit faire au minimum 8 charactères")

	// Erreurs spécifiques métier
	ErrDeliverySessionAlreadyActive = errors.New("delivery_session_already_active")

	ErrInvalidToken = errors.New("invalid_token")

	ErrCannotDisableExternalPayments = errors.New("cannot disable external platforms payments")

	// Erreurs d'authentification
	ErrUserNotFound = errors.New("user_not_found")

	ErrAccountDisabled = errors.New("account_disabled")

	ErrUserNotAllowed = errors.New("user_not_allowed")

	ErrCartEmpty = errors.New("cart_is_empty")

	ErrInternalServerError = errors.New("internal_server_error")

	ErrNoCashRegisterOpen = errors.New("no_cash_register_open")
)

// SendErrorJSON analyse l'erreur et envoie la réponse structurée appropriée
func SendErrorJSON(w http.ResponseWriter, module string, fnName string, err error) {
	status := http.StatusInternalServerError
	errorMsg := "internal_server_error"

	// Mapping des erreurs sentinelles vers les codes HTTP
	switch {
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		errorMsg = "unauthorized"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		errorMsg = "permission_denied"
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		errorMsg = "not_found"
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		errorMsg = "invalid_input"
	case errors.Is(err, ErrInvalidInputPasswordTooShort):
		status = http.StatusBadRequest
		errorMsg = "password_too_short"
	case errors.Is(err, ErrCannotDisableExternalPayments):
		status = http.StatusUnavailableForLegalReasons
		errorMsg = "cannot_disable_external_payments"
	case errors.Is(err, ErrUserNotFound):
		status = http.StatusNotFound
		errorMsg = "user_not_found"
	case errors.Is(err, ErrAccountDisabled):
		status = http.StatusForbidden
		errorMsg = "account_disabled"
	case errors.Is(err, ErrUserNotAllowed):
		status = http.StatusForbidden
		errorMsg = "user_not_allowed"
	case errors.Is(err, ErrCartEmpty):
		status = http.StatusUnauthorized
		errorMsg = "cart_is_empty"
	default:
		// Pour les erreurs inconnues, on peut logguer l'erreur réelle ici
		errorMsg = err.Error()
	}

	SendJSON(w, status, module, fnName, map[string]string{"error": errorMsg})
}

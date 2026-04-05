package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"welloresto-api/internal/logger"
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

	if err := json.NewEncoder(w).Encode(result); err != nil {
		// Erreur lors de l'encoding JSON - logger pour debug
		// Note: On ne peut pas changer le statut car WriteHeader est déjà appelé
		// mais on peut au moins logger l'erreur
		println("[JSON_ENCODE_ERROR] " + module + "." + fnName + ": " + err.Error())
	}
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

	ErrRefoundMustBeGreaterThanZero = errors.New("refund_amount_must_be_greater_than_zero")

	ErrReceiptNotFound = errors.New("receipt_not_found")

	ErrDeviceIDMissing = errors.New("device_id_missing")

	ErrMOPMissing = errors.New("mop_missing")

	ErrRefoundMustBeLowerThanOriginalReceipt = errors.New("refund_amount_must_be_lower_than_original_receipt")

	ErrOrdersStillOpened = errors.New("orders_still_opened")

	ErrCashRegisterStillOpen = errors.New("cash_register_still_open")

	ErrOrderClosed = errors.New("order_closed")

	ErrOrderOpen = errors.New("order_open")

	ErrMFARequired = errors.New("mfa_required")

	ErrMFAExpired = errors.New("mfa_expired")

	ErrOTPMismatch = errors.New("otp_mismatch")

	ErrOTPWaitTime = errors.New("otp_wait_time")

	ErrRedisNotAvailable = errors.New("not_available")
)

// SendErrorJSON analyse l'erreur et envoie la réponse structurée appropriée
func SendErrorJSON(w http.ResponseWriter, module string, fnName string, err error) {
	status := http.StatusInternalServerError
	errorStatus := "internal serveur error"
	errorMsg := "internal_server_error"

	// Mapping des erreurs sentinelles vers les codes HTTP
	switch {
	case errors.Is(err, ErrRedisNotAvailable):
		status = http.StatusServiceUnavailable
		errorStatus = "not_available"
		errorMsg = "Redis not available"

	case errors.Is(err, ErrMFAExpired):
		status = http.StatusUnauthorized
		errorStatus = "mfa_expired"
		errorMsg = "The MFA code was provided too recently. Please try again later."

	case errors.Is(err, ErrOTPWaitTime):
		status = http.StatusForbidden
		errorStatus = "otp_wait_time"
		errorMsg = "The OTP code was provided too recently. Please try again later."

	case errors.Is(err, ErrOTPMismatch):
		status = http.StatusUnauthorized
		errorStatus = "otp_mismatch"
		errorMsg = "The OTP code provided is not valid. Please try again."

	case errors.Is(err, ErrMFARequired):
		status = http.StatusUnauthorized
		errorStatus = "mfa_required"
		errorMsg = "MFA required, please try login"

	case errors.Is(err, ErrOrderOpen):
		status = http.StatusUnauthorized
		errorStatus = "order_open"
		errorMsg = "cannot permorm this action on an opened order"

	case errors.Is(err, ErrOrderClosed):
		status = http.StatusUnauthorized
		errorStatus = "order_closed"
		errorMsg = "cannot permorm this action on a closed order"

	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		errorStatus = "unauthorized"
		errorMsg = "unauthorized"

	case errors.Is(err, ErrNoCashRegisterOpen):
		status = http.StatusConflict
		errorStatus = "no_cash_register_opened"
		errorMsg = "no cash register opened for this device id"

	case errors.Is(err, ErrCashRegisterStillOpen):
		status = http.StatusUnauthorized
		errorStatus = "cash_register_still_open"
		errorMsg = "cannot perform this action on an opened cash register"

	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		errorStatus = "permission_denied"
		errorMsg = "permission_denied"

	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		errorStatus = "not_found"
		errorMsg = "not_found"

	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		errorStatus = "invalid_input"
		errorMsg = "Invalid input. Please check payloads and path parameters."

	case errors.Is(err, ErrInvalidInputPasswordTooShort):
		status = http.StatusBadRequest
		errorStatus = "password_too_short"
		errorMsg = "Le mot de passe doit faire au minimum 8 charactères"

	case errors.Is(err, ErrCannotDisableExternalPayments):
		status = http.StatusUnavailableForLegalReasons
		errorStatus = "cannot_disable_external_payments"
		errorMsg = "Cannot disable external payments"

	case errors.Is(err, ErrUserNotFound):
		status = http.StatusNotFound
		errorStatus = "user_not_found"
		errorMsg = "User not found"

	case errors.Is(err, ErrAccountDisabled):
		status = http.StatusForbidden
		errorStatus = "account_disabled"
		errorMsg = "User account disabled"

	case errors.Is(err, ErrUserNotAllowed):
		status = http.StatusForbidden
		errorStatus = "user_not_allowed"
		errorMsg = "User not allowed to perform this action"

	case errors.Is(err, ErrCartEmpty):
		status = http.StatusUnauthorized
		errorStatus = "cart_is_empty"
		errorMsg = "Cart is empty"

	case errors.Is(err, ErrRefoundMustBeGreaterThanZero):
		status = http.StatusBadRequest
		errorStatus = "refund_amount_must_be_greater_than_zero"
		errorMsg = "Refund amount must be greater than zero"

	case errors.Is(err, ErrReceiptNotFound):
		status = http.StatusNotFound
		errorStatus = "receipt_not_found"
		errorMsg = "Receipt not found"

	case errors.Is(err, ErrDeviceIDMissing):
		status = http.StatusBadRequest
		errorStatus = "device_id_missing"
		errorMsg = "Device ID missing"

	case errors.Is(err, ErrMOPMissing):
		status = http.StatusBadRequest
		errorStatus = "mop_missing"
		errorMsg = "Mean of payment missing"

	case errors.Is(err, ErrMOPMissing):
		status = http.StatusBadRequest
		errorStatus = "mop_missing"
		errorMsg = "Mean of payment missing"

	case errors.Is(err, ErrRefoundMustBeLowerThanOriginalReceipt):
		status = http.StatusBadRequest
		errorStatus = "refund_amount_must_be_lower_than_original_receipt"
		errorMsg = "Refund amount must be lower than original receipt"

	case errors.Is(err, ErrOrdersStillOpened):
		status = http.StatusUnauthorized
		errorStatus = "orders_still_opened"
		errorMsg = "Some orders created with this cash register are still opened"

	default:
		// Pour les erreurs inconnues, on peut logguer l'erreur réelle ici
		errorStatus = err.Error()
	}

	logger.FromContext(context.Background()).Warn("error " + strconv.Itoa(status) + " " + module + "." + fnName + ": " + errorMsg + " - " + errorStatus)

	SendJSON(w, status, module, fnName, map[string]string{"status": errorStatus, "error": errorMsg})
}

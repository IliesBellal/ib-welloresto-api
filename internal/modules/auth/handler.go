package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type AuthHandler struct {
	svc AuthService
}

func NewAuthHandler(s AuthService) *AuthHandler {
	return &AuthHandler{svc: s}
}

// Login handler - Can be used with user and pwd, with token in get, or token in authorization
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)

	var req LoginRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && token == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "login", map[string]string{"error": "invalid_request"})
		return
	}

	// Détection du backoffice (ex: via un header envoyé par le front web)
	isBackoffice := r.Header.Get("X-App-Source") == "backoffice"

	// On passe isBackoffice au service
	resp, err := h.svc.Login(r.Context(), req, token, isBackoffice)
	if err != nil {
		models.SendErrorJSON(w, "auth", "login", err)
		return
	}

	// Si le MFA est requis, on renvoie un code 202 Accepted au lieu de 200 OK
	if resp.Status == "MFA_REQUIRED" {
		models.SendJSON(w, http.StatusAccepted, "auth", "login", resp)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "login", resp)
}

// Can be used with user and pwd, with token in get, or token in authorization
func (h *AuthHandler) LoginOld(w http.ResponseWriter, r *http.Request) {

	token := helpers.ExtractToken(r)
	// Parse JSON body
	var req LoginRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && token == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "login", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.Login(r.Context(), req, token, false)
	if err != nil {
		models.SendErrorJSON(w, "auth", "login", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "login", resp)
}

func (h *AuthHandler) CheckAppVersion(w http.ResponseWriter, r *http.Request) {
	// Get token (header or ?token= fallback)
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "check", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	// Parse JSON body
	var req CheckAppVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "app", "version_check", map[string]string{"error": "invalid_request"})
		return
	}

	version := strings.TrimSpace(req.Version)
	appName := strings.TrimSpace(req.App)
	if version == "" || appName == "" {
		models.SendJSON(w, http.StatusBadRequest, "app", "version_check", map[string]string{"error": "missing_fields"})
		return
	}

	resp, err := h.svc.CheckAppVersion(ctx, token, version, appName)
	if err != nil {
		models.SendErrorJSON(w, "app", "version_check", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "app", "version_check", resp)
}

func (h *AuthHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "device", "save_token", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	var req SaveDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "device", "save_token", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	if err != nil {
		models.SendErrorJSON(w, "device", "save_token", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "device", "save_token", resp)
}

// SendVerification envoie un code de vérification à l'utilisateur
func (h *AuthHandler) SendVerification(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "send_verification", map[string]string{"error": "token manquant"})
		return
	}

	var req VerifyOTPRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "auth", "send_verification", map[string]string{"error": "invalid_request"})
		return
	}

	err := h.svc.SendVerificationCode(r.Context(), token, req.Mode)
	if err != nil {
		models.SendErrorJSON(w, "auth", "send_verification", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "send_verification", map[string]string{
		"status":  "success",
		"message": "Verification code sent.",
	})
}

// VerifyMFA traite la soumission du code à 6 chiffres
func (h *AuthHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	// Le token est envoyé par le front (soit dans le header Authorization, soit en query param)
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "otp_verify", map[string]string{"error": "token manquant"})
		return
	}

	var req VerifyOTPRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "auth", "otp_verify", map[string]string{"error": "invalid_request"})
		return
	}

	// Appel du service
	err := h.svc.ConfirmVerification(r.Context(), token, req.Mode, req.Code)
	if err != nil {
		models.SendErrorJSON(w, "auth", "otp_verify", err)
		return
	}

	// Succès
	models.SendJSON(w, http.StatusOK, "auth", "otp_verify", map[string]string{
		"status":  "success",
		"message": "Authentication successful.",
	})
}

// AuthPIN authenticates an employee by PIN.
// Authorization: anchor token (existing session of any user on the same merchant).
// Body: { "pin": "1234" }
// Response: identical to /auth/login, with the permanent token of the matched employee.
func (h *AuthHandler) AuthPIN(w http.ResponseWriter, r *http.Request) {
	anchorToken := helpers.ExtractToken(r)
	if anchorToken == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin", map[string]string{"error": "missing_token"})
		return
	}

	var req PINAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.PIN) == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "pin", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.AuthenticatePIN(r.Context(), anchorToken, req.PIN)
	if err != nil {
		var lockoutErr *PINLockoutError
		if errors.As(err, &lockoutErr) {
			models.SendJSON(w, http.StatusTooManyRequests, "auth", "pin", map[string]interface{}{
				"error":         "pin_locked",
				"delay_seconds": lockoutErr.DelaySeconds,
			})
			return
		}
		models.SendErrorJSON(w, "auth", "pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "pin", resp)
}

// SetPIN allows the authenticated user to set their own PIN (self-service).
// Authorization: the caller's own session token.
// Body: { "pin": "1234" }
// The user_id is taken from the authenticated session — a user cannot set another user's PIN.
func (h *AuthHandler) SetPIN(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin_set", map[string]string{"error": "missing_token"})
		return
	}

	var req SetPINRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.PIN) == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "pin_set", map[string]string{"error": "invalid_request"})
		return
	}

	caller, err := h.svc.GetUserByToken(r.Context(), token)
	if err != nil || caller == nil {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin_set", map[string]string{"error": "invalid_token"})
		return
	}

	if err := h.svc.SetPINSelf(r.Context(), caller.MerchantID, caller.UserID, req.PIN); err != nil {
		switch {
		case errors.Is(err, ErrPINInvalidLength):
			models.SendJSON(w, http.StatusBadRequest, "auth", "pin_set", map[string]string{"error": "pin_invalid_length"})
		case errors.Is(err, ErrPINConflict):
			models.SendJSON(w, http.StatusConflict, "auth", "pin_set", map[string]string{"error": "pin_already_used"})
		default:
			models.SendErrorJSON(w, "auth", "pin_set", err)
		}
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "pin_set", map[string]string{"status": "success"})
}

// ResetPIN clears the PIN of a target employee (sets pin_hash to NULL).
// Authorization: admin/manager (HasUserManagementAccess).
// Body: { "user_id": "..." }
func (h *AuthHandler) ResetPIN(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin_reset", map[string]string{"error": "missing_token"})
		return
	}

	var req ResetPINRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.UserID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "pin_reset", map[string]string{"error": "invalid_request"})
		return
	}

	caller, err := h.svc.GetUserByToken(r.Context(), token)
	if err != nil || caller == nil {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin_reset", map[string]string{"error": "invalid_token"})
		return
	}

	if err := h.svc.ResetPIN(r.Context(), caller.MerchantID, req.UserID); err != nil {
		models.SendErrorJSON(w, "auth", "pin_reset", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "pin_reset", map[string]string{"status": "success"})
}

// FallbackSMS déclenche l'envoi d'un code de secours par SMS
func (h *AuthHandler) FallbackSMS(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "fallback_sms", map[string]string{"error": "token manquant"})
		return
	}

	err := h.svc.FallbackSMS(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "auth", "fallback_sms", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "fallback_sms", map[string]string{
		"status":  "success",
		"message": "A new SMS code has been sent. Previous code has been invalidated.",
	})
}

// ForgotPassword handles POST /auth/forgot-password (public).
//
// Always answers 200 with the same body, whatever happened: unknown account,
// throttled, disabled, or a link actually sent. Any observable difference would
// turn this endpoint into an account-enumeration oracle.
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "auth", "forgot_password", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.svc.SendPasswordResetLink(r.Context(), req.Login, helpers.ClientIP(r)); err != nil {
		models.SendErrorJSON(w, "auth", "forgot_password", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "forgot_password", map[string]string{
		"status":  "success",
		"message": "Si un compte correspond, un email de réinitialisation a été envoyé.",
	})
}

// ResetPassword handles POST /auth/reset-password (public).
//
// Consumes the single-use token and applies the new password. A rejected
// password does not consume the token, so the user can retry with the same link.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "auth", "reset_password", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.svc.ConfirmPasswordReset(r.Context(), req.Token, req.NewPassword); err != nil {
		if errors.Is(err, ErrInvalidResetToken) {
			models.SendJSON(w, http.StatusBadRequest, "auth", "reset_password", map[string]string{
				"error":   "invalid_or_expired_token",
				"message": "Ce lien est invalide, expiré ou déjà utilisé.",
			})
			return
		}
		models.SendErrorJSON(w, "auth", "reset_password", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "reset_password", map[string]string{
		"status":  "success",
		"message": "Mot de passe réinitialisé. Toutes vos sessions ont été fermées.",
	})
}

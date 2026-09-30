package googleauth

import (
	"encoding/json"
	"errors"
	"net/http"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Authenticate handles POST /v1/auth/google. Translates this package's
// local sentinel errors into internal/models' HTTP-mapped vocabulary —
// models cannot import a feature module (would invert the dependency
// direction), so the translation happens here, at the handler boundary,
// same convention as signup's use of presets.ErrPresetNotFound.
func (h *Handler) Authenticate(w http.ResponseWriter, r *http.Request) {
	var req AuthenticateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "googleauth", "authenticate", models.ErrInvalidRequestBody)
		return
	}
	if req.IDToken == "" {
		models.SendErrorJSON(w, "googleauth", "authenticate", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.svc.Authenticate(r.Context(), req.IDToken)
	if err != nil {
		models.SendErrorJSON(w, "googleauth", "authenticate", translateError(err))
		return
	}

	models.SendJSON(w, http.StatusOK, "googleauth", "authenticate", map[string]interface{}{
		"status":      "success",
		"merchant_id": resp.MerchantID,
		"user_id":     resp.UserID,
		"token":       resp.Token,
	})
}

func translateError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidGoogleToken):
		return models.ErrInvalidGoogleToken
	case errors.Is(err, ErrGoogleEmailNotVerified):
		return models.ErrGoogleEmailNotVerified
	case errors.Is(err, ErrGoogleAccountNotFound):
		return models.ErrGoogleAccountNotFound
	case errors.Is(err, ErrGoogleAccountHasPassword):
		return models.ErrGoogleAccountHasPassword
	case errors.Is(err, ErrGoogleAccountLinkedElsewhere):
		return models.ErrGoogleAccountLinkedElsewhere
	case errors.Is(err, ErrGoogleAlreadyLinked):
		return models.ErrGoogleAlreadyLinked
	case errors.Is(err, ErrGoogleUnlinkRequiresPassword):
		return models.ErrGoogleUnlinkRequiresPassword
	default:
		return err
	}
}

// callerUserID resolves the session token to the caller's user_id, writing
// the 401 itself when it can't — same pattern as auth.AuthHandler's
// self-service routes (identity from the token, never the body).
func (h *Handler) callerUserID(w http.ResponseWriter, r *http.Request, fnName string) (string, bool) {
	token := helpers.ExtractToken(r)
	if token == "" {
		models.SendJSON(w, http.StatusUnauthorized, "googleauth", fnName, map[string]string{"error": "missing_token"})
		return "", false
	}
	userID, err := h.svc.CallerUserID(r.Context(), token)
	if err != nil || userID == "" {
		models.SendJSON(w, http.StatusUnauthorized, "googleauth", fnName, map[string]string{"error": "invalid_token"})
		return "", false
	}
	return userID, true
}

// LinkStatus handles GET /v1/auth/google/link.
func (h *Handler) LinkStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerUserID(w, r, "link_status")
	if !ok {
		return
	}
	h.sendLinkStatus(w, r, userID, "link_status")
}

// Link handles POST /v1/auth/google/link — attaches the Google account behind
// id_token to the caller's account.
func (h *Handler) Link(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerUserID(w, r, "link")
	if !ok {
		return
	}
	var req LinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IDToken == "" {
		models.SendErrorJSON(w, "googleauth", "link", models.ErrInvalidRequestBody)
		return
	}
	if err := h.svc.Link(r.Context(), userID, req.IDToken); err != nil {
		models.SendErrorJSON(w, "googleauth", "link", translateError(err))
		return
	}
	h.sendLinkStatus(w, r, userID, "link")
}

// Unlink handles DELETE /v1/auth/google/link.
func (h *Handler) Unlink(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerUserID(w, r, "unlink")
	if !ok {
		return
	}
	if err := h.svc.Unlink(r.Context(), userID); err != nil {
		models.SendErrorJSON(w, "googleauth", "unlink", translateError(err))
		return
	}
	h.sendLinkStatus(w, r, userID, "unlink")
}

// sendLinkStatus answers with the caller's current LinkStatusResponse, so the
// settings screen can render straight from any of the three routes' replies.
func (h *Handler) sendLinkStatus(w http.ResponseWriter, r *http.Request, userID, fnName string) {
	resp, err := h.svc.LinkStatus(r.Context(), userID)
	if err != nil {
		models.SendErrorJSON(w, "googleauth", fnName, translateError(err))
		return
	}
	models.SendJSON(w, http.StatusOK, "googleauth", fnName, resp)
}

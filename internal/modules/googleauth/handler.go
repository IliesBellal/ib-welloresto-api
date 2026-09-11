package googleauth

import (
	"encoding/json"
	"errors"
	"net/http"

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
	default:
		return err
	}
}

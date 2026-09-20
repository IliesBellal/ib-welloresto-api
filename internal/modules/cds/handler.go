package cds

// Handlers du parcours device (l'écran lui-même). Le parcours back-office
// vit dans admin_handler.go.
//
// Aucun handler d'écriture métier ici, et c'est structurel : un CDS lit des
// commandes, il n'en crée ni n'en modifie jamais. C'est la raison d'être du
// module séparé (CDS_DECISIONS.md D10) — un token d'écran ne doit ouvrir
// aucune route équivalente à POST /kiosk/orders.

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

// EnrollDevice — POST /cds/auth/enroll (public, pas de Bearer).
//
// ⚠ Route publique consommant un code à 6 chiffres, sans rate limiting dans
// cette API à ce jour : voir docs/audits/2026-09-19-enrollment-rate-limiting.md.
func (h *Handler) EnrollDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", "enroll_device", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.EnrollDevice(ctx, req, r.RemoteAddr)
	if err != nil {
		log.Warn("cds enroll failed", zap.Error(err))
		models.SendErrorJSON(w, "cds", "enroll_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "enroll_device", resp)
}

// RefreshDeviceToken — POST /cds/auth/token/refresh (public, pas de Bearer :
// c'est justement l'access token qui a expiré).
func (h *Handler) RefreshDeviceToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", "refresh_device_token", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.RefreshDeviceToken(ctx, req.RefreshToken)
	if err != nil {
		log.Warn("cds refresh failed", zap.Error(err))
		models.SendErrorJSON(w, "cds", "refresh_device_token", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "refresh_device_token", resp)
}

// DeviceHeartbeat — POST /cds/auth/heartbeat (CDSAuth).
func (h *Handler) DeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cds := middleware.GetCDS(r)
	if cds == nil {
		models.SendErrorJSON(w, "cds", "heartbeat", models.ErrCDSDeviceTokenInvalid)
		return
	}

	// Body optionnel : un heartbeat sans app_version reste valide.
	var req HeartbeatRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.service.RecordHeartbeat(ctx, cds, req, r.RemoteAddr)
	if err != nil {
		models.SendErrorJSON(w, "cds", "heartbeat", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "heartbeat", resp)
}

// GetBoard — GET /cds/orders (CDSAuth).
//
// Point d'entrée unique du temps réel côté écran : l'événement WebSocket
// UPDATE_ORDER ne transporte aucun état (décision D2 de l'API, reprise en
// CDS_DECISIONS.md D8), l'écran rappelle donc cette route, debouncée, et
// reconstruit son plateau. Même chemin au retour de connexion — la
// réconciliation n'est pas un cas particulier, c'est le flux nominal.
func (h *Handler) GetBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cds := middleware.GetCDS(r)
	if cds == nil {
		models.SendErrorJSON(w, "cds", "get_board", models.ErrCDSDeviceTokenInvalid)
		return
	}

	resp, err := h.service.GetBoard(ctx, cds)
	if err != nil {
		models.SendErrorJSON(w, "cds", "get_board", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "get_board", resp)
}

// GetDeviceSettings — GET /cds/settings (CDSAuth).
func (h *Handler) GetDeviceSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cds := middleware.GetCDS(r)
	if cds == nil {
		models.SendErrorJSON(w, "cds", "get_settings", models.ErrCDSDeviceTokenInvalid)
		return
	}

	resp, err := h.service.GetDeviceSettings(ctx, cds)
	if err != nil {
		models.SendErrorJSON(w, "cds", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "get_settings", resp)
}

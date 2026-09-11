package signup

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc          *Service
	sessionsRepo *Repository
}

func NewHandler(svc *Service, sessionsRepo *Repository) *Handler {
	return &Handler{svc: svc, sessionsRepo: sessionsRepo}
}

// memResponseWriter captures exactly what a handler writes, so the same
// bytes can be cached for idempotent replay (signup_sessions.payload) and
// then copied to the real ResponseWriter. Deliberately not
// net/http/httptest.ResponseRecorder — that type is meant for tests, not for
// wrapping a live request.
type memResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newMemResponseWriter() *memResponseWriter {
	return &memResponseWriter{header: http.Header{}, status: http.StatusOK}
}

func (w *memResponseWriter) Header() http.Header         { return w.header }
func (w *memResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *memResponseWriter) WriteHeader(status int)      { w.status = status }

// Signup handles POST /v1/signup — public (see cmd/api/routes.go), Idempotency-Key
// header required, response replayed verbatim for 24h on a repeated call
// with the same key (LOT A Semaine 2, Chantier 6b).
func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		models.SendErrorJSON(w, "signup", "create", models.ErrIdempotencyKeyRequired)
		return
	}

	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "signup", "create", models.ErrInvalidRequestBody)
		return
	}

	ctx := r.Context()

	// Replay check.
	existing, err := h.sessionsRepo.GetSession(ctx, idempotencyKey)
	if err != nil {
		models.SendErrorJSON(w, "signup", "create", err)
		return
	}
	if existing != nil {
		h.replay(w, existing)
		return
	}

	// Claim the key.
	reqJSON, err := json.Marshal(req)
	if err != nil {
		models.SendErrorJSON(w, "signup", "create", models.ErrInvalidRequestBody)
		return
	}
	expiresAt := time.Now().UTC().Add(SignupSessionTTL)
	created, err := h.sessionsRepo.TryBeginSession(ctx, idempotencyKey, req.Identity.Email, req.Identity.Provider, req.ContextToken, reqJSON, expiresAt)
	if err != nil {
		models.SendErrorJSON(w, "signup", "create", err)
		return
	}
	if !created {
		// Lost a race against a concurrent call with the same key — handle
		// exactly like an ordinary replay lookup.
		existing, err := h.sessionsRepo.GetSession(ctx, idempotencyKey)
		if err != nil {
			models.SendErrorJSON(w, "signup", "create", err)
			return
		}
		if existing == nil {
			models.SendErrorJSON(w, "signup", "create", models.ErrSignupInProgress)
			return
		}
		h.replay(w, existing)
		return
	}

	// Process behind a capturing writer so the exact response bytes can be
	// cached for the next replay.
	rec := newMemResponseWriter()
	result, procErr := h.svc.Signup(ctx, req)
	if procErr != nil {
		models.SendErrorJSON(rec, "signup", "create", procErr)
		_ = h.sessionsRepo.FailSession(ctx, idempotencyKey, rec.status, rec.body.Bytes())
	} else {
		models.SendJSON(rec, http.StatusCreated, "signup", "create", map[string]interface{}{
			"status":           "success",
			"merchant_id":      result.MerchantID,
			"user_id":          result.UserID,
			"token":            result.Token,
			"activation_state": result.ActivationState,
		})
		_ = h.sessionsRepo.CompleteSession(ctx, idempotencyKey, rec.status, rec.body.Bytes())
	}

	for k, v := range rec.header {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
}

// CreateSignupContext handles POST /v1/public/signup-context (LOT A
// Semaine 3, Chantier 11) — public, IP-rate-limited (see
// Service.CreateContext).
func (h *Handler) CreateSignupContext(w http.ResponseWriter, r *http.Request) {
	var req CreateContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "signup", "create-context", models.ErrInvalidRequestBody)
		return
	}
	resp, err := h.svc.CreateContext(r.Context(), helpers.ClientIP(r), req)
	if err != nil {
		models.SendErrorJSON(w, "signup", "create-context", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "signup", "create-context", resp)
}

// GetSignupContext handles GET /v1/public/signup-context/{token} — public.
func (h *Handler) GetSignupContext(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	resp, err := h.svc.GetContext(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "signup", "get-context", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "signup", "get-context", resp)
}

// replay writes a terminal session's cached response verbatim, or 409s if
// it is still pending (a genuine concurrent duplicate mid-flight).
func (h *Handler) replay(w http.ResponseWriter, session *Session) {
	if session.State == StatePending {
		models.SendErrorJSON(w, "signup", "create", models.ErrSignupInProgress)
		return
	}
	var cached CachedResponse
	if err := json.Unmarshal(session.Payload, &cached); err != nil {
		models.SendErrorJSON(w, "signup", "create", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(cached.Status)
	_, _ = w.Write(cached.Body)
}

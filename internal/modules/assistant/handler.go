package assistant

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"welloresto-api/internal/models"
)

type Handler struct {
	service *Service
	repo    *Repository
}

func NewHandler(svc *Service, repo *Repository) *Handler {
	return &Handler{service: svc, repo: repo}
}

const module = "assistant"

// GET /assistant/model?surface=backoffice&version=v1-…
func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request) {
	surface := r.URL.Query().Get("surface")
	if surface == "" {
		surface = "backoffice"
	}
	res, err := h.service.GetModel(r.Context(), surface, r.URL.Query().Get("version"))
	if err != nil {
		models.SendErrorJSON(w, module, "get_model", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "get_model", res)
}

// POST /assistant/events — 204. Envoyé en tâche de fond par le client.
func (h *Handler) RecordEvents(w http.ResponseWriter, r *http.Request) {
	var batch EventBatch
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		models.SendErrorJSON(w, module, "record_events", models.ErrInvalidRequestBody)
		return
	}
	if err := h.service.RecordEvents(r.Context(), &batch); err != nil {
		models.SendErrorJSON(w, module, "record_events", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Administration (staff WelloResto) ─────────────────────────────────────

// period lit from / to (AAAA-MM-JJ, to inclus). Défaut : 30 derniers jours.
func period(r *http.Request) (time.Time, time.Time) {
	to := time.Now().AddDate(0, 0, 1).Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -30)
	if v, err := time.Parse("2006-01-02", r.URL.Query().Get("from")); err == nil {
		from = v
	}
	if v, err := time.Parse("2006-01-02", r.URL.Query().Get("to")); err == nil {
		to = v.AddDate(0, 0, 1)
	}
	return from, to
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return v
	}
	return def
}

// GET /admin/assistant/stats?from=&to=&merchant_id=
func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
	from, to := period(r)
	st, err := h.repo.Stats(r.Context(), from, to, r.URL.Query().Get("merchant_id"))
	if err != nil {
		models.SendErrorJSON(w, module, "admin_stats", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_stats", st)
}

// GET /admin/assistant/fallbacks?from=&to=&limit=
func (h *Handler) AdminFallbacks(w http.ResponseWriter, r *http.Request) {
	from, to := period(r)
	groups, err := h.repo.FallbackGroups(r.Context(), from, to, intParam(r, "limit", 50))
	if err != nil {
		models.SendErrorJSON(w, module, "admin_fallbacks", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_fallbacks", map[string]interface{}{"groups": groups})
}

// GET /admin/assistant/intents
func (h *Handler) AdminIntents(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListIntents(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "admin_intents", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_intents", map[string]interface{}{"intents": list})
}

// GET /admin/assistant/terms?status=&source=&intent_key=&merchant_id=&review=true&limit=&offset=
func (h *Handler) AdminListTerms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := TermFilter{
		Status: q.Get("status"), Source: q.Get("source"), IntentKey: q.Get("intent_key"), MerchantID: q.Get("merchant_id"),
		Limit: intParam(r, "limit", 100), Offset: intParam(r, "offset", 0),
	}
	if v, err := strconv.ParseBool(q.Get("review")); err == nil {
		f.Review = &v
	}
	list, err := h.repo.ListTerms(r.Context(), f)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_terms", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_terms", map[string]interface{}{"terms": list})
}

// POST /admin/assistant/terms — terme saisi à la main (validé d'office).
func (h *Handler) AdminCreateTerm(w http.ResponseWriter, r *http.Request) {
	var req CreateTermRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, module, "admin_create_term", models.ErrInvalidRequestBody)
		return
	}
	row, err := h.service.CreateTerm(r.Context(), &req)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_create_term", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, module, "admin_create_term", row)
}

// PATCH /admin/assistant/terms/{id} — valider, rejeter, déplacer, repondérer, passer en global.
func (h *Handler) AdminUpdateTerm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_update_term", models.ErrMissingResourceID)
		return
	}
	var req UpdateTermRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, module, "admin_update_term", models.ErrInvalidRequestBody)
		return
	}
	row, err := h.service.UpdateTerm(r.Context(), id, &req)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_update_term", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_update_term", row)
}

// GET /admin/assistant/aliases?status=&merchant_id=&limit=
func (h *Handler) AdminListAliases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.repo.ListAliases(r.Context(), q.Get("status"), q.Get("merchant_id"), intParam(r, "limit", 100))
	if err != nil {
		models.SendErrorJSON(w, module, "admin_aliases", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_aliases", map[string]interface{}{"aliases": list})
}

// PATCH /admin/assistant/aliases/{id}
func (h *Handler) AdminUpdateAlias(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_update_alias", models.ErrMissingResourceID)
		return
	}
	var req UpdateAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, module, "admin_update_alias", models.ErrInvalidRequestBody)
		return
	}
	row, err := h.service.UpdateAlias(r.Context(), id, &req)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_update_alias", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_update_alias", row)
}

// GET /admin/assistant/sessions/{id}/events — rejeu d'une session.
func (h *Handler) AdminSessionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		models.SendErrorJSON(w, module, "admin_session_events", models.ErrMissingResourceID)
		return
	}
	events, err := h.repo.SessionEvents(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, module, "admin_session_events", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_session_events", map[string]interface{}{"events": events})
}

// POST /admin/assistant/learning/run — passage d'apprentissage immédiat.
func (h *Handler) AdminRunLearning(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.RunLearningNow(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "admin_run_learning", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "admin_run_learning", report)
}

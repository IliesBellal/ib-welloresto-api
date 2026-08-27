package roles

import (
	"encoding/json"
	"errors"
	"net/http"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// sendServiceError special-cases the two errors that carry structured
// payload the front needs (§2: version conflict must return the current
// version; G5 must return the holder count) before falling back to the
// standard sentinel mapping in models.SendErrorJSON.
func sendServiceError(w http.ResponseWriter, fnName string, err error) {
	var versionErr *VersionConflictError
	if errors.As(err, &versionErr) {
		models.SendJSON(w, http.StatusConflict, "roles", fnName, map[string]interface{}{
			"status":          "version_conflict",
			"message":         "This role was changed by someone else. Reload it and try again.",
			"error":           "version_conflict",
			"current_version": versionErr.CurrentVersion,
		})
		return
	}

	var membersErr *RoleHasMembersError
	if errors.As(err, &membersErr) {
		models.SendJSON(w, http.StatusConflict, "roles", fnName, map[string]interface{}{
			"status":       "role_has_members",
			"message":      "This role is still held by at least one user and cannot be archived.",
			"error":        "role_has_members",
			"holder_count": membersErr.Count,
		})
		return
	}

	models.SendErrorJSON(w, "roles", fnName, err)
}

func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	domains, err := h.svc.ListPermissionCatalog(r.Context())
	if err != nil {
		sendServiceError(w, "list_permissions", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "list_permissions", map[string]interface{}{"status": "success", "domains": domains})
}

func (h *Handler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.MyPermissions(r.Context())
	if err != nil {
		sendServiceError(w, "my_permissions", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "my_permissions", map[string]interface{}{"status": "success", "my_permissions": result})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListRoles(r.Context())
	if err != nil {
		sendServiceError(w, "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "list", map[string]interface{}{"status": "success", "roles": items})
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var req CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "roles", "create", models.ErrInvalidRequestBody)
		return
	}
	role, err := h.svc.CreateRole(r.Context(), req)
	if err != nil {
		sendServiceError(w, "create", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "roles", "create", map[string]interface{}{"status": "success", "role": role})
}

func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	role, err := h.svc.GetRole(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		sendServiceError(w, "get", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "get", map[string]interface{}{"status": "success", "role": role})
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	var req UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "roles", "update", models.ErrInvalidRequestBody)
		return
	}
	role, err := h.svc.UpdateRole(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		sendServiceError(w, "update", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "update", map[string]interface{}{"status": "success", "role": role})
}

func (h *Handler) ReplacePermissions(w http.ResponseWriter, r *http.Request) {
	var req ReplacePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "roles", "replace_permissions", models.ErrInvalidRequestBody)
		return
	}
	role, err := h.svc.ReplacePermissions(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		sendServiceError(w, "replace_permissions", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "replace_permissions", map[string]interface{}{"status": "success", "role": role})
}

func (h *Handler) ListRoleMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.svc.ListRoleMembers(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		sendServiceError(w, "members", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "members", map[string]interface{}{"status": "success", "members": members})
}

func (h *Handler) ArchiveRole(w http.ResponseWriter, r *http.Request) {
	role, err := h.svc.ArchiveRole(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		sendServiceError(w, "archive", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "archive", map[string]interface{}{"status": "success", "role": role})
}

func (h *Handler) SetUserRole(w http.ResponseWriter, r *http.Request) {
	var req SetUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "roles", "set_user_role", models.ErrInvalidRequestBody)
		return
	}
	role, err := h.svc.SetUserRole(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		sendServiceError(w, "set_user_role", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "set_user_role", map[string]interface{}{"status": "success", "user_id": chi.URLParam(r, "id"), "role": role})
}

func (h *Handler) SetMerchantDefaultRole(w http.ResponseWriter, r *http.Request) {
	var req SetDefaultRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "roles", "set_default_role", models.ErrInvalidRequestBody)
		return
	}
	role, err := h.svc.SetMerchantDefaultRole(r.Context(), req)
	if err != nil {
		sendServiceError(w, "set_default_role", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "roles", "set_default_role", map[string]interface{}{"status": "success", "role": role})
}

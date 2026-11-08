package users

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap/zapcore"
)

func (h *UsersHandler) ListMerchantUsers(w http.ResponseWriter, r *http.Request) {
	filters, err := parseMerchantUserListFilters(r)
	if err != nil {
		models.SendErrorJSON(w, "users", "list", err)
		return
	}
	items, metadata, err := h.svc.ListMerchantUsers(r.Context(), filters)
	if err != nil {
		models.SendErrorJSON(w, "users", "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "list", map[string]interface{}{"status": "success", "users": items, "pagination": metadata})
}

func (h *UsersHandler) GetMerchantUser(w http.ResponseWriter, r *http.Request) {
	item, err := h.svc.GetMerchantUser(r.Context(), h.userIDParam(r, "id"))
	if err != nil {
		models.SendErrorJSON(w, "users", "get", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "get", map[string]interface{}{"status": "success", "user": item})
}

func (h *UsersHandler) SearchLinkableUsers(w http.ResponseWriter, r *http.Request) {
	filters, err := parseLinkableUserSearchFilters(r)
	if err != nil {
		models.SendErrorJSON(w, "users", "linkable_search", err)
		return
	}
	items, metadata, err := h.svc.SearchLinkableUsers(r.Context(), filters)
	if err != nil {
		models.SendErrorJSON(w, "users", "linkable_search", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "linkable_search", map[string]interface{}{"status": "success", "users": items, "pagination": metadata})
}

func (h *UsersHandler) GetMerchantUserRights(w http.ResponseWriter, r *http.Request) {
	rights, err := h.svc.GetMerchantUserRights(r.Context(), h.userIDParam(r, "id"))
	if err != nil {
		models.SendErrorJSON(w, "users", "get_rights", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "get_rights", map[string]interface{}{"status": "success", "rights": rights})
}

func (h *UsersHandler) GetMerchantUserMember(w http.ResponseWriter, r *http.Request) {
	member, err := h.svc.GetMerchantUserMember(r.Context(), h.userIDParam(r, "id"))
	if err != nil {
		models.SendErrorJSON(w, "users", "get_member", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "get_member", map[string]interface{}{"status": "success", "member": member})
}

func (h *UsersHandler) PatchMerchantUserMember(w http.ResponseWriter, r *http.Request) {
	var req MerchantUserMemberPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "patch_member", models.ErrInvalidRequestBody)
		logger.FromContext(r.Context()).Log(zapcore.ErrorLevel, err.Error())
		return
	}
	member, err := h.svc.PatchMerchantUserMember(r.Context(), h.userIDParam(r, "id"), req)
	if err != nil {
		models.SendErrorJSON(w, "users", "patch_member", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "patch_member", map[string]interface{}{"status": "success", "member": member})
}

func (h *UsersHandler) UpdateMerchantUserRights(w http.ResponseWriter, r *http.Request) {
	var req MerchantUserRightsUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "update_rights", models.ErrInvalidRequestBody)
		return
	}
	rights, err := h.svc.UpdateMerchantUserRights(r.Context(), h.userIDParam(r, "id"), req)
	if err != nil {
		models.SendErrorJSON(w, "users", "update_rights", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "update_rights", map[string]interface{}{"status": "success", "rights": rights})
}

func (h *UsersHandler) LinkMerchantUser(w http.ResponseWriter, r *http.Request) {
	var req MerchantUserLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "link_merchant", models.ErrInvalidRequestBody)
		return
	}
	rights, err := h.svc.LinkMerchantUser(r.Context(), h.userIDParam(r, "id"), req)
	if err != nil {
		models.SendErrorJSON(w, "users", "link_merchant", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "users", "link_merchant", map[string]interface{}{"status": "success", "rights": rights})
}

// LinkMerchantUserByEmail répond 202 avec le même message que le compte existe
// ou non (voir UsersService.LinkMerchantUserByEmail) : ne jamais y ajouter
// d'information sur le compte.
func (h *UsersHandler) LinkMerchantUserByEmail(w http.ResponseWriter, r *http.Request) {
	var req MerchantUserLinkByEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "link_by_email", models.ErrInvalidRequestBody)
		return
	}
	if err := h.svc.LinkMerchantUserByEmail(r.Context(), req); err != nil {
		models.SendErrorJSON(w, "users", "link_by_email", err)
		return
	}
	models.SendJSON(w, http.StatusAccepted, "users", "link_by_email", map[string]interface{}{
		"status":  "success",
		"message": "Si un compte existe avec cette adresse, il sera rattaché à l'établissement et son titulaire en sera informé par e-mail.",
	})
}

func (h *UsersHandler) ForceResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ForceResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "force_reset_password", models.ErrInvalidRequestBody)
		return
	}
	if err := h.svc.ForceResetPassword(r.Context(), h.userIDParam(r, "id"), req); err != nil {
		models.SendErrorJSON(w, "users", "force_reset_password", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "force_reset_password", map[string]interface{}{"status": "success", "tokens_invalidated": true})
}

func (h *UsersHandler) UnlinkMerchantUser(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.UnlinkMerchantUser(r.Context(), h.userIDParam(r, "id"))
	if err != nil {
		models.SendErrorJSON(w, "users", "unlink_merchant", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "unlink_merchant", map[string]interface{}{"status": "success", "result": result})
}

func parseMerchantUserListFilters(r *http.Request) (MerchantUserListFilters, error) {
	page, pageSize, err := parseUsersPagination(r)
	if err != nil {
		return MerchantUserListFilters{}, err
	}
	return MerchantUserListFilters{
		Search:         strings.TrimSpace(r.URL.Query().Get("search")),
		Active:         parseOptionalBool(r.URL.Query().Get("active")),
		LinkedEmployee: parseOptionalBool(r.URL.Query().Get("linked_employee")),
		Admin:          parseOptionalBool(r.URL.Query().Get("admin")),
		Page:           page,
		PageSize:       pageSize,
	}, nil
}

func parseLinkableUserSearchFilters(r *http.Request) (LinkableUserSearchFilters, error) {
	page, pageSize, err := parseUsersPagination(r)
	if err != nil {
		return LinkableUserSearchFilters{}, err
	}
	return LinkableUserSearchFilters{
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func parseUsersPagination(r *http.Request) (int, int, error) {
	page := 1
	pageSize := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, models.ErrInvalidPage
		}
		page = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, models.ErrInvalidPageSize
		}
		pageSize = parsed
	}
	return page, pageSize, nil
}

func parseOptionalBool(raw string) *bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed := raw == "1" || strings.EqualFold(raw, "true")
	return &parsed
}

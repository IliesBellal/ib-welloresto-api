package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type UsersHandler struct {
	svc      *UsersService
	r2Client *r2.Client
}

func NewUsersHandler(s *UsersService, r2Client *r2.Client) *UsersHandler {
	return &UsersHandler{svc: s, r2Client: r2Client}
}

func (h *UsersHandler) GetUserLocation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "location", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	userID := chi.URLParam(r, "user_id")

	result, err := h.svc.GetUserLocation(ctx, token, userID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "location", map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		models.SendJSON(w, http.StatusNotFound, "user", "location", map[string]string{"error": "user not found"})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "location", result)
}

func (h *UsersHandler) SetUserLocation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "location", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.UpdateLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_location", map[string]string{"error": "invalid_request"})
		return
	}

	err := h.svc.SetUserLocation(ctx, token, req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "location", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "location", map[string]string{"status": "success"})
}

func (h *UsersHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "update_password", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_password", map[string]string{"error": "invalid_request"})
		return
	}

	newToken, err := h.svc.UpdatePassword(ctx, token, req.OldPassword, req.NewPassword)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "update_password", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "update_password",
		models.HandlerDefaultResponse{
			ID:   "users.reset-password",
			Data: map[string]string{"status": "success", "token": newToken},
		})
}

func (h *UsersHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.svc.GetProfile(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "user", "get_profile", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "get_profile", profile)
}

func (h *UsersHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.GetNotifications(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "users", "notifications", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "users", "notifications", data)
}

func (h *UsersHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_profile", map[string]string{"error": "invalid_request"})
		return
	}

	profile, err := h.svc.UpdateProfile(r.Context(), &req)
	if err != nil {
		if errors.Is(err, ErrInvalidPhoneFormat) {
			models.SendJSON(w, http.StatusBadRequest, "user", "update_profile", map[string]string{"error": "invalid_phone_format"})
			return
		}
		models.SendJSON(w, http.StatusInternalServerError, "user", "update_profile", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "update_profile", profile)
}

// POST /users/profile/avatar
// Multipart form-data: field "avatar" (image/jpeg, image/png, image/webp)
// Taille max: 5 MB
func (h *UsersHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	const maxSize = 5 << 20 // 5 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "upload_avatar", map[string]string{"error": "file_too_large"})
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "upload_avatar", map[string]string{"error": "missing_avatar_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "user", "upload_avatar", map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	user, err := middleware.UserFromContext(r.Context())
	if err != nil {
		models.SendJSON(w, http.StatusUnauthorized, "user", "upload_avatar", map[string]string{"error": "unauthorized"})
		return
	}

	// Supprimer l'ancien avatar (non-bloquant)
	if oldURL, err := h.svc.userRepo.GetUserAvatarURL(r.Context(), user.UserID); err == nil && oldURL != "" {
		if oldKey := h.r2Client.GetKeyFromURL(oldURL); oldKey != "" {
			_ = h.r2Client.DeleteFile(r.Context(), oldKey)
		}
	}

	ext := r2.GetExtensionFromContentType(contentType)
	key := r2.GenerateUserAvatarKey(user.UserID, ext)

	publicURL, err := h.r2Client.UploadFile(r.Context(), key, file, contentType)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "upload_avatar", map[string]string{"error": fmt.Sprintf("upload_failed: %s", err.Error())})
		return
	}

	if err := h.svc.userRepo.UpdateUserAvatar(r.Context(), user.UserID, publicURL); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "upload_avatar", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "upload_avatar", map[string]string{"avatar_url": publicURL})
}

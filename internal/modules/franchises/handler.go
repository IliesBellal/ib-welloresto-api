package franchises

// Routes back-office : /franchises (authentifiées). Les droits sont vérifiés
// par le service à partir de l'utilisateur, jamais de l'établissement
// sélectionné : un gestionnaire de deux franchises les gère depuis un seul
// compte.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

const (
	module = "franchises"

	maxImageBytes          = 5 << 20
	multipartOverheadBytes = 1 << 20
	maxJSONBodyBytes       = 64 << 10
)

type Handler struct {
	svc *Service
	r2  *r2.Client
}

func NewHandler(svc *Service, r2Client *r2.Client) *Handler {
	return &Handler{svc: svc, r2: r2Client}
}

func actorOf(r *http.Request) (Actor, bool) {
	user := middleware.GetUser(r)
	if user == nil {
		return Actor{}, false
	}
	return Actor{UserID: user.UserID, PlatformStaff: user.IsPlatformStaff}, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		models.SendErrorJSON(w, module, "decode_body", models.ErrInvalidRequestBody)
		return false
	}
	return true
}

// handle factorise : acteur, appel du service, réponse.
func handle(w http.ResponseWriter, r *http.Request, fnName string, status int, call func(actor Actor) (any, error)) {
	actor, ok := actorOf(r)
	if !ok {
		models.SendErrorJSON(w, module, fnName, models.ErrUnauthorized)
		return
	}
	resp, err := call(actor)
	if err != nil {
		models.SendErrorJSON(w, module, fnName, err)
		return
	}
	models.SendJSON(w, status, module, fnName, resp)
}

// List — GET /franchises : franchises gérées + can_create.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "list_franchises", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.List(r.Context(), a)
	})
}

// Create — POST /franchises {name, slug?}.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "create_franchise", http.StatusCreated, func(a Actor) (any, error) {
		return h.svc.Create(r.Context(), a, req)
	})
}

// SlugAvailability — GET /franchises/slug-availability?slug=&exclude_id=.
func (h *Handler) SlugAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	handle(w, r, "franchise_slug_availability", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.SlugAvailability(r.Context(), a, q.Get("slug"), q.Get("exclude_id"))
	})
}

// Get — GET /franchises/{franchise_id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "get_franchise", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.Get(r.Context(), a, chi.URLParam(r, "franchise_id"))
	})
}

// Update — PATCH /franchises/{franchise_id} {name?, slug?, description?, brand_color?}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "update_franchise", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.Update(r.Context(), a, chi.URLParam(r, "franchise_id"), req)
	})
}

// Delete — DELETE /franchises/{franchise_id} (owner).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "delete_franchise", http.StatusOK, func(a Actor) (any, error) {
		if err := h.svc.Delete(r.Context(), a, chi.URLParam(r, "franchise_id")); err != nil {
			return nil, err
		}
		return map[string]string{"status": "success"}, nil
	})
}

// UploadLogo — PUT /franchises/{franchise_id}/logo (multipart, champ photo).
func (h *Handler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	h.uploadImage(w, r, "logo")
}

// UploadBanner — PUT /franchises/{franchise_id}/banner (multipart, champ photo).
func (h *Handler) UploadBanner(w http.ResponseWriter, r *http.Request) {
	h.uploadImage(w, r, "banner")
}

// uploadImage : même circuit que les images ScanNOrder d'un établissement
// (integrations.uploadScanNOrderImage) : droits vérifiés avant de lire le
// fichier, clé R2 déterministe + cache-buster, ancien fichier supprimé quand
// sa clé diffère (extension changée).
func (h *Handler) uploadImage(w http.ResponseWriter, r *http.Request, imageType string) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	fnName := "upload_franchise_" + imageType
	franchiseID := chi.URLParam(r, "franchise_id")

	actor, ok := actorOf(r)
	if !ok {
		models.SendErrorJSON(w, module, fnName, models.ErrUnauthorized)
		return
	}
	if err := h.svc.RequireManager(ctx, actor, franchiseID); err != nil {
		models.SendErrorJSON(w, module, fnName, err)
		return
	}
	if h.r2 == nil {
		models.SendErrorJSON(w, module, fnName, fmt.Errorf("image storage not configured"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+multipartOverheadBytes)
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("photo")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, fnName, map[string]string{"error": "missing_photo_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, module, fnName, map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	key := r2.GenerateFranchiseKey(franchiseID, imageType, r2.GetExtensionFromContentType(contentType))
	publicURL, err := h.r2.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error(fnName+" UploadFile", zap.Error(err))
		models.SendErrorJSON(w, module, fnName, fmt.Errorf("failed to upload image"))
		return
	}
	// Clé déterministe : sans ce paramètre, navigateur et CDN servent l'ancienne image.
	publicURL = fmt.Sprintf("%s?v=%d", publicURL, time.Now().UnixNano())

	oldURL, err := h.svc.SetImage(ctx, actor, franchiseID, imageType, publicURL)
	if err != nil {
		models.SendErrorJSON(w, module, fnName, err)
		return
	}
	if oldKey := h.r2.GetKeyFromURL(oldURL); oldKey != "" && oldKey != key {
		if err := h.r2.DeleteFile(ctx, oldKey); err != nil {
			log.Warn(fnName+" DeleteFile (old image)", zap.Error(err))
		}
	}

	models.SendJSON(w, http.StatusOK, module, fnName, map[string]any{"status": "success", "photo_url": publicURL})
}

// Candidates — GET /franchises/{franchise_id}/candidates.
func (h *Handler) Candidates(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "list_franchise_candidates", http.StatusOK, func(a Actor) (any, error) {
		items, err := h.svc.Candidates(r.Context(), a, chi.URLParam(r, "franchise_id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"merchants": items}, nil
	})
}

// AddMerchants — POST /franchises/{franchise_id}/merchants {merchant_ids}.
func (h *Handler) AddMerchants(w http.ResponseWriter, r *http.Request) {
	var req AddMerchantsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "add_franchise_merchants", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.AddMerchants(r.Context(), a, chi.URLParam(r, "franchise_id"), req)
	})
}

// UpdateMerchant — PATCH /franchises/{franchise_id}/merchants/{merchant_id} {hub_visible}.
func (h *Handler) UpdateMerchant(w http.ResponseWriter, r *http.Request) {
	var req UpdateMerchantRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "update_franchise_merchant", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.UpdateMerchant(r.Context(), a, chi.URLParam(r, "franchise_id"), chi.URLParam(r, "merchant_id"), req)
	})
}

// RemoveMerchant — DELETE /franchises/{franchise_id}/merchants/{merchant_id}.
func (h *Handler) RemoveMerchant(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "remove_franchise_merchant", http.StatusOK, func(a Actor) (any, error) {
		return h.svc.RemoveMerchant(r.Context(), a, chi.URLParam(r, "franchise_id"), chi.URLParam(r, "merchant_id"))
	})
}

func membersResponse(members []Member, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"members": members}, nil
}

// ListMembers — GET /franchises/{franchise_id}/members.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "list_franchise_members", http.StatusOK, func(a Actor) (any, error) {
		return membersResponse(h.svc.ListMembers(r.Context(), a, chi.URLParam(r, "franchise_id")))
	})
}

// AddMember — POST /franchises/{franchise_id}/members {email, role?} (owner).
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	var req AddMemberRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "add_franchise_member", http.StatusOK, func(a Actor) (any, error) {
		return membersResponse(h.svc.AddMember(r.Context(), a, chi.URLParam(r, "franchise_id"), req))
	})
}

// UpdateMember — PATCH /franchises/{franchise_id}/members/{user_id} {role} (owner).
func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	var req UpdateMemberRequest
	if !decodeBody(w, r, &req) {
		return
	}
	handle(w, r, "update_franchise_member", http.StatusOK, func(a Actor) (any, error) {
		return membersResponse(h.svc.UpdateMember(r.Context(), a, chi.URLParam(r, "franchise_id"), chi.URLParam(r, "user_id"), req))
	})
}

// RemoveMember — DELETE /franchises/{franchise_id}/members/{user_id} (owner, ou soi-même).
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	handle(w, r, "remove_franchise_member", http.StatusOK, func(a Actor) (any, error) {
		return membersResponse(h.svc.RemoveMember(r.Context(), a, chi.URLParam(r, "franchise_id"), chi.URLParam(r, "user_id")))
	})
}

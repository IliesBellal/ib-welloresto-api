package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"welloresto-api/internal/services"
)

type MenuHandler struct {
	service *services.MenuService
}

func NewMenuHandler(s *services.MenuService) *MenuHandler {
	return &MenuHandler{service: s}
}

func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// token extraction: Authorization: Bearer X or ?token=
	auth := r.Header.Get("Authorization")
	var token string
	if strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	lastMenuParam := r.URL.Query().Get("last_menu_update") // can be empty
	var lastMenu *time.Time
	if lastMenuParam != "" {
		// parse same format as DB - we accept "2006-01-02 15:04:05"
		layout := "2006-01-02 15:04:05"
		if t, err := time.ParseInLocation(layout, lastMenuParam, time.UTC); err == nil {
			lastMenu = &t
		}
	}

	resp, err := h.service.GetMenu(ctx, token, lastMenu)
	if err != nil {
		log.Printf("[GetMenu] ERROR token=%s last_menu_update=%v -> %+v", token, lastMenu, err)
		http.Error(w, fmt.Sprintf(`{"status":"-2","error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(resp)
}

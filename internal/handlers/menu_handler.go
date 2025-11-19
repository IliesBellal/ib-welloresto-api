package handlers

import (
	"encoding/json"
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

	// token extraction
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

	// last_menu_update
	lastMenuParam := r.URL.Query().Get("last_menu_update")
	var lastMenu *time.Time
	if lastMenuParam != "" {
		layout := "2006-01-02 15:04:05"
		t, err := time.ParseInLocation(layout, lastMenuParam, time.UTC)
		if err == nil {
			lastMenu = &t
		}
	}

	resp, err := h.service.GetMenu(ctx, token, lastMenu)
	if err != nil {
		// LOG SERVER SIDE
		log.Printf("[ERROR] GetMenu token=%s last_menu=%v err=%+v", token, lastMenu, err)

		// RETURN CLEAN ERROR TO CLIENT
		http.Error(
			w,
			`{"status":"-2","error":"internal error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	// success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

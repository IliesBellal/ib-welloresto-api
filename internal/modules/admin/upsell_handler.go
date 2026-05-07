package admin

import (
	"net/http"
	"time"

	"welloresto-api/internal/models"
	"welloresto-api/internal/tasks"

	"go.uber.org/zap"
)

type AdminUpsellHandler struct {
	tasksManager *tasks.TasksManager
	logger       *zap.Logger
}

func NewAdminUpsellHandler(tm *tasks.TasksManager, logger *zap.Logger) *AdminUpsellHandler {
	return &AdminUpsellHandler{tasksManager: tm, logger: logger}
}

// RecomputePatterns handles POST /admin/upsell/recompute-patterns.
func (h *AdminUpsellHandler) RecomputePatterns(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("admin: manual upsell patterns recompute requested")

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				h.logger.Error("admin: panic in manual recompute",
					zap.Any("recover", rec),
				)
			}
		}()
		h.tasksManager.RecomputeUpsellPatterns()
	}()

	models.SendJSON(w, http.StatusAccepted, "admin", "recompute_upsell_patterns", map[string]interface{}{
		"started_at": time.Now().UTC().Format(time.RFC3339),
		"scope":      "all_merchants",
	})
}

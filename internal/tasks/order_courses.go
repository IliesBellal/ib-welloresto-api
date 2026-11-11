package tasks

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// fireDueOrderCoursesBudget borne un passage (toutes les 5 s).
const fireDueOrderCoursesBudget = 20 * time.Second

// FireDueOrderCourses envoie en cuisine les suites dont l'envoi programmé est
// échu : délai automatique après la suite précédente, ou fin des 5 s
// d'annulation après « Réclamer » (docs/order-lifecycle.md, « Suites »).
// L'envoi est fait par le serveur pour partir même si l'appareil qui l'a
// demandé s'est mis en veille. Sûr entre instances (UPDATE atomique).
func (tm *TasksManager) FireDueOrderCourses() {
	if tm.OrderService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fireDueOrderCoursesBudget)
	defer cancel()
	if err := tm.OrderService.FireDueCourses(ctx); err != nil {
		tm.logError("FireDueOrderCourses failed", zap.Error(err))
	}
}

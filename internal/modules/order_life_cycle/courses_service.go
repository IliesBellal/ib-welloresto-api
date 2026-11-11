package order_life_cycle

import (
	"context"
	"fmt"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
)

// FireCourseRequest : corps de POST /orders/{order_id}/courses/{course}/fire.
// DelaySeconds = fenêtre d'annulation (« Réclamer » côté caisse : 5 s) ;
// 0 = envoi immédiat.
type FireCourseRequest struct {
	DelaySeconds int `json:"delay_seconds"`
}

// FireCourse réclame une suite : envoi immédiat (delaySeconds = 0) ou
// programmé dans delaySeconds, exécuté côté serveur par la tâche
// FireDueOrderCourses — l'envoi part même si l'appareil qui l'a demandé se
// met en veille juste après. Les suites précédentes encore en attente
// partent avec elle.
func (s *OrdersLifeCycleService) FireCourse(ctx context.Context, orderID string, course int, req *FireCourseRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	if course <= 0 {
		return fmt.Errorf("%w: course invalide", models.ErrInvalidInput)
	}
	delay := 0
	if req != nil {
		delay = req.DelaySeconds
	}
	if delay < 0 || delay > maxCourseFireDelaySeconds {
		return fmt.Errorf("%w: delay_seconds hors bornes", models.ErrInvalidInput)
	}

	open, err := s.ordersLifeCycleRepo.OrderStillOpen(ctx, orderID)
	if err != nil {
		return err
	}
	if !open {
		return models.ErrOrderClosed
	}

	if delay == 0 {
		n, err := s.ordersLifeCycleRepo.FireOrderCoursesUpTo(ctx, user.MerchantID, orderID, course, user.UserID)
		if err != nil {
			return err
		}
		if n == 0 {
			if err := s.ordersLifeCycleRepo.courseNotPendingError(ctx, user.MerchantID, orderID, course); err != nil {
				return err
			}
		}
	} else if err := s.ordersLifeCycleRepo.ScheduleOrderCourseFire(ctx, user.MerchantID, orderID, course, delay); err != nil {
		return err
	}

	s.afterCoursesChanged(ctx, user.MerchantID, orderID)
	return nil
}

// CancelCourseFire annule l'envoi programmé d'une suite encore en attente
// (bouton « Annuler » des 5 s après « Réclamer ») ; elle repasse en manuel.
func (s *OrdersLifeCycleService) CancelCourseFire(ctx context.Context, orderID string, course int) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	if course <= 0 {
		return fmt.Errorf("%w: course invalide", models.ErrInvalidInput)
	}
	if err := s.ordersLifeCycleRepo.CancelOrderCourseFire(ctx, user.MerchantID, orderID, course); err != nil {
		return err
	}
	s.afterCoursesChanged(ctx, user.MerchantID, orderID)
	return nil
}

// FireDueCourses : passe de la tâche de fond (toutes les 5 s). Envoie les
// suites échues puis prévient les appareils de chaque commande concernée.
func (s *OrdersLifeCycleService) FireDueCourses(ctx context.Context) error {
	fired, err := s.ordersLifeCycleRepo.FireDueOrderCourses(ctx)
	if err != nil {
		return err
	}
	for _, o := range fired {
		logger.FromContext(ctx).Info("course fired by schedule",
			zap.String("merchant_id", o.MerchantID), zap.String("order_id", o.OrderID))
		s.afterCoursesChanged(ctx, o.MerchantID, o.OrderID)
	}
	return nil
}

// AssignCoursesFromCategories répartit en suites les lignes d'une commande
// sur place qui n'en a pas encore (Scan&Order) : étape de la catégorie de
// chaque produit, première suite envoyée tout de suite, suivantes en manuel
// ou au délai donné (minutes, 0 = manuel). Sans aucune étape, la commande
// reste sans suite.
func (s *OrdersLifeCycleService) AssignCoursesFromCategories(ctx context.Context, merchantID string, order *models.OrderRequest, delayMinutes int) error {
	if order == nil || order.OrderType != orderTypeIn || len(order.Products) == 0 {
		return nil
	}
	productIDs := make([]string, 0, len(order.Products))
	for _, p := range order.Products {
		productIDs = append(productIDs, p.ProductID)
	}
	stepsByProduct, err := s.ordersLifeCycleRepo.CourseStepsForProducts(ctx, merchantID, productIDs)
	if err != nil {
		return err
	}
	if len(stepsByProduct) == 0 {
		return nil
	}

	steps := make([]*int, len(order.Products))
	for i, p := range order.Products {
		if step, ok := stepsByProduct[p.ProductID]; ok {
			st := step
			steps[i] = &st
		}
	}
	courses := AssignCoursesFromSteps(steps)

	maxCourse := 0
	for i := range order.Products {
		order.Products[i].Course = courses[i]
		if courses[i] != nil && *courses[i] > maxCourse {
			maxCourse = *courses[i]
		}
	}
	if maxCourse == 0 {
		return nil
	}

	var delay *int
	if delayMinutes > 0 {
		d := delayMinutes
		delay = &d
	}
	settings := make([]models.OrderCoursePayload, 0, maxCourse)
	for c := 1; c <= maxCourse; c++ {
		setting := models.OrderCoursePayload{Course: c, DelayMinutes: delay}
		if c == 1 {
			setting.DelayMinutes = nil
			setting.FireNow = true
		}
		settings = append(settings, setting)
	}
	order.Courses = &settings
	return nil
}

// SnoCourseDelayMinutes : réglage Scan&Order (0 = manuel, aussi en cas
// d'erreur de lecture : jamais d'envoi automatique par défaut).
func (s *OrdersLifeCycleService) SnoCourseDelayMinutes(ctx context.Context, merchantID string) int {
	delay, err := s.ordersLifeCycleRepo.SnoCourseDelayMinutes(ctx, merchantID)
	if err != nil {
		logger.FromContext(ctx).Warn("sno course delay lookup failed", zap.String("merchant_id", merchantID), zap.Error(err))
		return 0
	}
	return delay
}

// afterCoursesChanged : purge du cache Redis de la commande puis
// notification OrderUpdate — les caisses rechargent la commande et
// impriment le ticket de la suite envoyée.
func (s *OrdersLifeCycleService) afterCoursesChanged(ctx context.Context, merchantID, orderID string) {
	if s.redis != nil {
		s.redis.Delete(ctx, helpers.GetRedisOrderKey(merchantID, orderID))
	}
	if s.notificationsService != nil {
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	}
}

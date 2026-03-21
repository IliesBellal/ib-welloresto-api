package service

import (
	"context"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
)

func (s *Service) HandleDeliveryStatus(ctx context.Context, brandOrderID string, status string) error {
	merchantID, orderID, err := s.ordersRepo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err != nil {
		return err
	}

	switch status {

	case "SCHEDULED":
		s.orderLifeCycleSvc.SetOrderAccepted(ctx, models.UberEatsWebhookUserID, merchantID, orderID)
		return nil

	case "EN_ROUTE_TO_PICKUP", "ARRIVED_AT_PICKUP":
		return nil

	case "EN_ROUTE_TO_DROPOFF":
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := s.ordersRepo.MarkEnRouteToDropoff(ctx, tx, brandOrderID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)

	case "ARRIVED_AT_DROPOFF":
		return nil

	case "FINISHED", "COMPLETED":
		s.orderLifeCycleSvc.DeliverOrder(ctx, models.UberEatsWebhookUserID, merchantID, orderID)
		return nil

	case "FAILED":
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := s.ordersRepo.MarkFailed(ctx, tx, brandOrderID); err != nil {
			tx.Rollback()
			return err
		}
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

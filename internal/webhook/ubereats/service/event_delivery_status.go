package service

import (
	"context"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
)

func (s *Service) HandleDeliveryStatus(ctx context.Context, brandOrderID string, status string) error {
	merchantID, orderID, err := s.ordersRepo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return err
	}

	switch status {

	case "SCHEDULED":
		s.orderLifeCycleSvc.SetOrderAccepted(ctx, models.UberEatsWebhookUserID, merchantID, orderID)
		return nil

	case "EN_ROUTE_TO_PICKUP", "ARRIVED_AT_PICKUP":
		return nil

	case "EN_ROUTE_TO_DROPOFF":
		if err := s.ordersRepo.MarkEnRouteToDropoff(ctx, brandOrderID); err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return err
		}
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)

	case "ARRIVED_AT_DROPOFF":
		return nil

	case "FINISHED", "COMPLETED":
		s.orderLifeCycleSvc.SetDeliveredExternal(ctx, merchantID, models.UberEatsWebhookUserID, orderID)
		return nil

	case "FAILED":
		if err := s.ordersRepo.MarkFailed(ctx, brandOrderID); err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return err
		}
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	}

	return nil
}

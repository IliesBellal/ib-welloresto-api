package service

import (
	"context"
	"welloresto-api/internal/modules/notification"
)

func (s *Service) HandleOrderCanceled(ctx context.Context, brandOrderID string) error {
	err := s.ordersRepo.CancelOrder(ctx, brandOrderID)
	if err != nil {
		return err
	}

	merchantID, orderID, err := s.ordersRepo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err == nil {
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	}

	return nil
}

package service

import (
	"context"
	"welloresto-api/internal/modules/notification"
)

func (s *Service) HandleOrderCanceled(ctx context.Context, brandOrderID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	err = s.ordersRepo.CancelOrder(ctx, tx, brandOrderID)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	merchantID, orderID, err := s.ordersRepo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err == nil {
		s.notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	}

	return nil
}

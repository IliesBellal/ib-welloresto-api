package service

import (
	"context"
)

func (s *Service) HandleDeliveryStatus(ctx context.Context, brandOrderID string, status string) error {
	merchantID, orderID, err := s.ordersRepo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err != nil {
		return err
	}

	switch status {

	case "SCHEDULED":
		s.orderLifeCycleSvc.SetOrderAccepted(ctx, "UBER_EATS_WEBHOOK", merchantID, orderID)
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
		s.notificationsService.SendNotificationAsync(merchantID, orderID, "UPDATE_ORDER")
		if err := tx.Commit(); err != nil {
			return err
		}

	case "ARRIVED_AT_DROPOFF":
		return nil

	case "FINISHED", "COMPLETED":
		s.orderLifeCycleSvc.DeliverOrder(ctx, "UBER_EATS_WEBHOOK", merchantID, orderID)
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
		s.notificationsService.SendNotificationAsync(merchantID, orderID, "UPDATE_ORDER")
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

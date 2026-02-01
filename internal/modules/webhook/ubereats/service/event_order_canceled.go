package service

import (
	"context"
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
		s.orderLifeCycleSvc.SendUpdateOrderNotification(merchantID, orderID)
	}

	return nil
}

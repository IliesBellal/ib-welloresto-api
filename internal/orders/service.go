package orders

import (
	"context"
	"database/sql"
	"time"
	"welloresto-api/internal/repositories"
)

type OrderService struct {
	db       *sql.DB
	repo     OrdersRepository
	custRepo repositories.CustomersRepository
	notifier Notifier
	pricing  PricingEngine
}

func NewOrderService(
	db *sql.DB,
	repo OrdersRepository,
	custRepo repositories.CustomersRepository,
	notifier Notifier,
	pricing PricingEngine,
) *OrderService {
	return &OrderService{
		db:       db,
		repo:     repo,
		custRepo: custRepo,
		notifier: notifier,
		pricing:  pricing,
	}
}

func (s *OrderService) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	// --- Étapes (appelées dans service_steps.go) -------------------------

	unavailable, err := s.validateProductAvailability(ctx, tx, req)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if len(unavailable) > 0 {
		tx.Rollback()
		return &CreateOrderResult{Status: 2}, nil
	}

	customerID, err := s.upsertCustomer(ctx, tx, req)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	orderID, orderNum, err := s.insertOrderBase(ctx, tx, req, customerID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	usedItems, err := s.insertOrderItems(ctx, tx, req, orderID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := s.insertExtrasWithoutsConfigs(ctx, tx, req, usedItems); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := s.insertPayments(ctx, tx, req, orderID); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Post-commit async
	go s.notifier.SendNewOrderNotification(orderID)

	return &CreateOrderResult{
		Status:     1,
		OrderID:    orderID,
		OrderNum:   orderNum,
		OrderItems: usedItems,
		Action:     "waiting",
	}, nil
}

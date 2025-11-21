package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type OrdersRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewOrdersRepository(db *sql.DB, log *zap.Logger) *OrdersRepository {
	return &OrdersRepository{db: db, log: log}
}

// ==================================================================================
// PUBLIC METHODS
// ==================================================================================

// GetPendingOrders : Récupère toutes les commandes en cours (Optimisé)
func (r *OrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	r.log.Info("GetPendingOrders START", zap.String("merchant_id", merchantID))

	// On a besoin du repo session pour récupérer les sessions à la fin
	deliverySessionRepo := NewDeliverySessionsRepository(r.db, r.log)

	// ========================================================================
	// ÉTAPE 1 : OPTIMISATION - Récupérer les IDs d'abord
	// ========================================================================

	// 1.a. On construit la clause WHERE complexe ici
	criteria := " AND ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING')) OR ds.id IS NOT NULL) "

	// Ajout filtre APP
	if app == "1" || app == "WR_DELIVERY" {
		criteria += " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		criteria += " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// 1.b. Requête légère pour récupérer UNIQUEMENT les IDs
	// On doit inclure les JOINs ici pour que le filtre fonctionne (alias 'o' et 'ds')
	qIDs := `SELECT DISTINCT o.order_id
             FROM orders o
             LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
             LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING')
             WHERE o.merchant_id = ? ` + criteria

	rows, err := r.db.QueryContext(ctx, qIDs, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending order ids: %w", err)
	}
	defer rows.Close()

	var orderIDs []string
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, oid)
	}

	// ========================================================================
	// CAS VIDE : Si aucune commande ne correspond, on sort tout de suite
	// ========================================================================
	if len(orderIDs) == 0 {
		// On retourne vide, mais on récupère quand même les sessions vides si nécessaire,
		// ou on retourne tout vide. Selon ton besoin métier.
		// Ici je retourne tout vide pour être rapide.
		return &models.PendingOrdersResponse{
			Orders:           []models.Order{},
			DeliverySessions: []models.DeliverySession{},
		}, nil
	}

	// ========================================================================
	// ÉTAPE 2 : Appeler le constructeur avec le filtre OPTIMISÉ (IN)
	// ========================================================================

	// Construction de la chaîne "IN ('id1', 'id2')"
	idsStr := ""
	for i, oid := range orderIDs {
		if i > 0 {
			idsStr += ","
		}
		idsStr += fmt.Sprintf("'%s'", oid)
	}

	// Le filtre magique qui va rendre les 11 requêtes suivantes instantanées
	filterOptimized := fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr)

	orders, err := r.fetchAndBuildOrders(ctx, merchantID, filterOptimized)
	if err != nil {
		return nil, err
	}

	// ========================================================================
	// ÉTAPE 3 : Récupérer les sessions et finaliser
	// ========================================================================

	// Récupérer les sessions (spécifique à cet endpoint)
	// Note : comme on est dans le même package 'repositories', on a accès aux méthodes privées (minuscule)
	sessions, err := deliverySessionRepo.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// Assemblage final
	return &models.PendingOrdersResponse{
		Orders:           orders,
		DeliverySessions: sessions,
	}, nil
}

// GetOrder : Récupère une seule commande par son ID (Réutilise toute la logique !)
func (r *OrdersRepository) GetOrder(ctx context.Context, merchantID string, orderID string) (*models.Order, error) {
	r.log.Info("GetOrder START", zap.String("order_id", orderID))

	// Filtre strict sur l'ID
	filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)

	orders, err := r.fetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return &orders[0], nil
}

func (r *OrdersRepository) GetHistory(ctx context.Context, merchantID string, req models.OrderHistoryRequest) ([]models.Order, error) {
	r.log.Info("GetHistory START", zap.String("merchant_id", merchantID))

	filter := fmt.Sprintf(
		" AND o.state = 'CLOSED' "+
			"AND o.creation_date BETWEEN '%s' AND '%s' ",
		req.DateFrom, req.DateTo,
	)

	return r.fetchAndBuildOrders(ctx, merchantID, filter)
}

func (r *OrdersRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
	r.log.Info("GetPaymentsForOrder START", zap.String("order_id", orderID))

	q := `
		SELECT order_id, payment_id, mop, amount, payment_date, enabled
		FROM payments
		WHERE order_id = ?
		ORDER BY payment_date ASC
	`

	rows, err := r.db.QueryContext(ctx, q, orderID)
	if err != nil {
		r.log.Error("GetPaymentsForOrder ERROR", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	payments := []models.Payment{}

	for rows.Next() {
		var p models.Payment
		var paymentDate sql.NullTime

		err := rows.Scan(&p.OrderID, &p.PaymentID, &p.MOP, &p.Amount, &paymentDate, &p.Enabled)
		if err != nil {
			return nil, err
		}

		if paymentDate.Valid {
			p.PaymentDate = &paymentDate.Time
		}

		payments = append(payments, p)
	}

	return payments, nil
}

func (r *OrdersRepository) DisablePayment(ctx context.Context, paymentID string) error {
	r.log.Info("DisablePayment START", zap.String("payment_id", paymentID))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Disable payment
	_, err = tx.ExecContext(ctx, `
		UPDATE payments SET enabled = 0 WHERE payment_id = ?
	`, paymentID)
	if err != nil {
		tx.Rollback()
		return err
	}

	// Refresh order as unpaid
	_, err = tx.ExecContext(ctx, `
		UPDATE orders o 
		JOIN payments p ON o.order_id = p.order_id
		SET o.isPaid = false, o.last_update = UTC_TIMESTAMP()
		WHERE p.payment_id = ?
	`, paymentID)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

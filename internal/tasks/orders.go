package tasks

import (
	"context"
	"log"
	"welloresto-api/internal/models"
)

const (
	autoDenyDelay             = 15  // minutes (à ajuster selon tes besoins PHP)
	autoCloseOtherOrdersDelay = 720 // minutes (12h)
)

// CloseOrders : Ferme les commandes payées et livrées après délai
func (tm *TasksManager) CloseOrders() {
	log.Println("[CRON] Démarrage: CloseOrders")

	query := `
		SELECT o.order_id, p.stock_management, o.merchant_id
		FROM orders o
		INNER JOIN merchant m on m.id = o.merchant_id
		INNER JOIN merchant_parameters mp on mp.merchant_id = m.id
		INNER JOIN subscriptions s on m.id = s.merchant_id
		INNER JOIN packages p on p.id = s.package_id
		WHERE mp.auto_complete_orders
		AND o.isPaid
		AND o.isDistributed
		AND (
			(o.state <> 'CLOSED' AND o.order_type NOT IN ('DELIVERY') AND o.brand = 'WELLO_RESTO' AND o.created_by <> '-1' AND TIMESTAMPDIFF(MINUTE, o.delivered_on, UTC_TIMESTAMP) >= mp.auto_complete_orders_delay)
			OR
			(o.state <> 'CLOSED' AND o.order_type IN ('TAKE_AWAY') AND o.brand_status IN ('READY_FOR_HANDOFF','READY_FOR_TAKE_AWAY') AND TIMESTAMPDIFF(MINUTE, o.creation_date, UTC_TIMESTAMP) >= ?)
		);`

	rows, err := tm.DB.Query(query, autoCloseOtherOrdersDelay)
	if err != nil {
		log.Printf("[CRON] Erreur Query CloseOrders: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var orderID, merchantID string
		var stockManagement bool // Ou int selon ton schéma
		if err := rows.Scan(&orderID, &stockManagement, &merchantID); err != nil {
			log.Printf("[CRON] Erreur Scan CloseOrders: %v", err)
			continue
		}

		log.Printf("[CRON] Clôture automatique commande: %s", orderID)
		// On appelle le service injecté (signature supposée basée sur ton PHP)
		err := tm.OrderService.DeliverOrder(context.Background(), "SYSTEM", merchantID, orderID)
		if err != nil {
			log.Printf("[CRON] Erreur SetDelivered pour %s: %v", orderID, err)
		}
	}

	log.Println("[CRON] Terminé: CloseOrders")
}

// DenyOrders : Refuse les commandes en attente depuis trop longtemps
func (tm *TasksManager) DenyOrders() {
	log.Println("[CRON] Démarrage: DenyOrders")

	query := `
		SELECT o.order_id, o.merchant_id
		FROM orders o
		WHERE o.state <> 'DONE'
		AND o.brand = 'WELLO_RESTO'
		AND o.brand_status = 'PENDING_APPROVAL'
		AND o.merchant_approval = 'PENDING_APPROVAL'
		AND o.scheduled = false
		AND TIMESTAMPDIFF(MINUTE, o.creation_date, UTC_TIMESTAMP) >= ?;`

	rows, err := tm.DB.Query(query, autoDenyDelay)
	if err != nil {
		log.Printf("[CRON] Erreur Query DenyOrders: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var orderID, merchantID string
		if err := rows.Scan(&orderID, &merchantID); err != nil {
			log.Printf("[CRON] Erreur Scan DenyOrders: %v", err)
			continue
		}

		// Signature basée sur ton PHP: setOrderDenied(id, "42", "", "SYSTEM", merchantId)
		deny_reason := models.DenyOrderRequest{
			MerchantID:       merchantID,
			UserID:           "SYSTEM",
			DeletionReasonID: "42",
		}
		err := tm.OrderService.SetOrderDenied(context.Background(), orderID, deny_reason)
		if err != nil {
			log.Printf("[CRON] Erreur SetOrderDenied pour %s: %v", orderID, err)
		}
	}

	log.Println("[CRON] Terminé: DenyOrders")
}

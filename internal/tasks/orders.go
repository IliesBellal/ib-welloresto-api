package tasks

import (
	"context"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

const (
	autoDenyDelay             = 15  // minutes (à ajuster selon tes besoins PHP)
	autoCloseOtherOrdersDelay = 720 // minutes (12h)
)

// CloseOrders : Ferme les commandes payées et livrées après délai.
//
// Contrainte pool MySQL (1 connexion max) : les résultats sont intégralement
// collectés en mémoire et le rows fermé AVANT d'appeler le service commande,
// sinon la requête suivante attendrait indéfiniment la connexion tenue par
// l'itération en cours (deadlock qui gèle toute l'API).
func (tm *TasksManager) CloseOrders() {
	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	query := `
		SELECT o.order_id, p.stock_management, o.merchant_id
		FROM orders o
		INNER JOIN merchant m on ` + tskMerchantJoinCast() + ` = o.merchant_id
		INNER JOIN merchant_parameters mp on mp.merchant_id = ` + tskMerchantJoinCast() + `
		INNER JOIN subscriptions s on ` + tskMerchantJoinCast() + ` = s.merchant_id
		INNER JOIN packages p on p.id = s.package_id
		WHERE mp.auto_complete_orders
		AND o.isPaid
		AND o.isDistributed
		AND (
			(o.state <> 'CLOSED' AND o.order_type NOT IN ('DELIVERY') AND o.brand = 'WELLO_RESTO' AND o.created_by <> '-1' AND ` + tskMinutesSince("o.delivered_on") + ` >= mp.auto_complete_orders_delay)
			OR
			(o.state <> 'CLOSED' AND o.order_type IN ('TAKE_AWAY') AND o.brand_status IN ('READY_FOR_HANDOFF','READY_FOR_TAKE_AWAY') AND ` + tskMinutesSince("o.creation_date") + ` >= ?)
		)`

	rows, err := db.QueryContext(ctx, query, autoCloseOtherOrdersDelay)
	if err != nil {
		tm.logError("[CRON] CloseOrders: requête échouée", zap.Error(err))
		return
	}

	type orderRef struct{ orderID, merchantID string }
	var toClose []orderRef
	for rows.Next() {
		var ref orderRef
		var stockManagement bool // colonne héritée du PHP, non utilisée ici
		if err := rows.Scan(&ref.orderID, &stockManagement, &ref.merchantID); err != nil {
			tm.logError("[CRON] CloseOrders: scan échoué", zap.Error(err))
			continue
		}
		toClose = append(toClose, ref)
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] CloseOrders: itération interrompue", zap.Error(err))
	}
	rows.Close()

	closed := 0
	for _, ref := range toClose {
		if err := tm.OrderService.DeliverOrder(ctx, "SYSTEM", ref.merchantID, ref.orderID); err != nil {
			tm.logError("[CRON] CloseOrders: clôture échouée",
				zap.String("order_id", ref.orderID),
				zap.String("merchant_id", ref.merchantID),
				zap.Error(err))
			continue
		}
		closed++
	}

	if len(toClose) > 0 {
		tm.logInfo("[CRON] CloseOrders: terminé",
			zap.Int("eligibles", len(toClose)),
			zap.Int("fermees", closed))
	}
}

// DenyOrders : Refuse les commandes en attente depuis trop longtemps.
// Même schéma que CloseOrders : collecte complète avant action (1 connexion max).
func (tm *TasksManager) DenyOrders() {
	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	query := `
		SELECT o.order_id, o.merchant_id
		FROM orders o
		WHERE o.state <> 'DONE'
		AND o.brand = 'WELLO_RESTO'
		AND o.brand_status = 'PENDING_APPROVAL'
		AND o.merchant_approval = 'PENDING_APPROVAL'
		AND o.scheduled = false
		AND ` + tskMinutesSince("o.creation_date") + ` >= ?`

	rows, err := db.QueryContext(ctx, query, autoDenyDelay)
	if err != nil {
		tm.logError("[CRON] DenyOrders: requête échouée", zap.Error(err))
		return
	}

	type orderRef struct{ orderID, merchantID string }
	var toDeny []orderRef
	for rows.Next() {
		var ref orderRef
		if err := rows.Scan(&ref.orderID, &ref.merchantID); err != nil {
			tm.logError("[CRON] DenyOrders: scan échoué", zap.Error(err))
			continue
		}
		toDeny = append(toDeny, ref)
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] DenyOrders: itération interrompue", zap.Error(err))
	}
	rows.Close()

	denied := 0
	for _, ref := range toDeny {
		denyReason := models.DenyOrderRequest{
			MerchantID:       ref.merchantID,
			UserID:           "SYSTEM",
			DeletionReasonID: "42",
			DeletionComment:  "Commande non approuvée dans les délais",
		}
		if err := tm.OrderService.SetOrderDenied(ctx, ref.orderID, denyReason); err != nil {
			tm.logError("[CRON] DenyOrders: refus échoué",
				zap.String("order_id", ref.orderID),
				zap.String("merchant_id", ref.merchantID),
				zap.Error(err))
			continue
		}
		denied++
	}

	if len(toDeny) > 0 {
		tm.logInfo("[CRON] DenyOrders: terminé",
			zap.Int("eligibles", len(toDeny)),
			zap.Int("refusees", denied))
	}
}

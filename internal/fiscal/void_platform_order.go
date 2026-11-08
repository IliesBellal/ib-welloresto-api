package fiscal

import (
	"context"
	"database/sql"
	"errors"

	"welloresto-api/internal/diagnostics"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// CloseVoidPlatformOrder clôture une commande plateforme encore ouverte que
// la plateforme a annulée, refusée ou dont la livraison a échoué : la
// plateforme ne la paie pas, ses paiements actifs sont donc annulés
// (CancelOrderPayments, trace d'audit) dans la même transaction que la
// clôture, comme une annulation (docs/marketplace-fiabilite-import.md, lot 6).
//
// À appeler dans la transaction de l'appelant, la commande déjà verrouillée
// (LockOpenOrdersByBrandOrderID). closeOrder écrit la clôture ; hasSaleReceipt :
// la commande a déjà un ticket de vente (rouverte après une vente), dont
// closeOrder émet l'avoir.
//
// La clôture ne doit jamais échouer à cause de l'annulation des paiements
// (avant ce lot, ces commandes étaient clôturées sans y toucher) : si
// l'annulation est refusée (paiement encaissé sur un registre déjà fermé) ou
// échoue, tout ce qu'elle a écrit est défait (point de sauvegarde) et la
// commande est clôturée comme avant, paiements intacts ; l'écart est
// signalé (diagnostic payment_cancel_skipped). Seule une erreur de cette
// clôture de repli est renvoyée.
func CloseVoidPlatformOrder(ctx context.Context, database *sql.DB, c PaymentCancellation, closeOrder func(ctx context.Context, hasSaleReceipt bool) error) error {
	if dbutils.ExtractTx(ctx) == nil {
		return ErrNoTransaction
	}
	cancelErr := dbutils.Isolated(dbutils.WithSavepoints(ctx), func() error {
		_, err := CancelOrderPayments(ctx, database, c, closeOrder)
		return err
	})
	if cancelErr == nil {
		return nil
	}
	if errors.Is(cancelErr, context.Canceled) || errors.Is(cancelErr, context.DeadlineExceeded) {
		return cancelErr
	}

	reason := "error"
	if errors.Is(cancelErr, models.ErrOrderPaymentRegisterClosed) {
		reason = "register_closed"
	}
	logger.FromContext(ctx).Error("fiscal: void platform order closed without cancelling its payments",
		zap.String("merchant_id", c.MerchantID), zap.String("order_id", c.OrderID),
		zap.String("source", c.Source), zap.String("reason", reason), zap.Error(cancelErr))
	diagnostics.Record(ctx, diagnostics.Event{
		Kind:        "payment_cancel_skipped",
		MerchantID:  c.MerchantID,
		SubjectType: "order",
		SubjectID:   c.OrderID,
		Data:        map[string]any{"source": c.Source, "reason": reason, "error": cancelErr.Error()},
	})
	return closeOrder(ctx, false)
}

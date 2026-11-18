package customer_accounts

import (
	"context"
	"fmt"
	"strings"

	"welloresto-api/internal/customeraccount"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// Remboursement d'un paiement en ligne : décidé hors de la caisse (tableau de
// bord du prestataire de paiement), l'argent est déjà rendu au client quand
// l'API l'apprend. Le compte l'enregistre donc toujours, même si le client a
// déjà consommé son avance : le solde peut passer sous le plafond, ce qui
// bloque toute nouvelle commande sur compte et déclenche l'alerte de plafond.

// onlineRefundDelta : part du cumul remboursé (total) pas encore répercutée
// sur le compte. Le cumul ne dépasse jamais le montant payé, et un événement
// rejoué ou en retard ne donne rien.
func onlineRefundDelta(total, paid, already int64) int64 {
	if total > paid {
		total = paid
	}
	if total <= already {
		return 0
	}
	return total - already
}

// HandleOnlinePaymentRefunded répercute sur le compte le remboursement d'un
// paiement en ligne (webhook). totalRefunded est le cumul remboursé du
// paiement ; rejouable.
func (s *Service) HandleOnlinePaymentRefunded(ctx context.Context, paymentIntentID string, totalRefunded int64) error {
	paymentIntentID = strings.TrimSpace(paymentIntentID)
	if paymentIntentID == "" || totalRefunded <= 0 {
		return nil
	}
	return dbutils.RunInTx(ctx, s.database, func(txCtx context.Context) error {
		p, ok, err := s.repo.LockOnlinePayment(txCtx, paymentIntentID)
		if err != nil {
			return err
		}
		if !ok {
			// Le versement n'est pas encore connu (événement arrivé avant le
			// paiement) : une erreur fait rejouer l'événement plus tard.
			return fmt.Errorf("customer_accounts: refund of unknown online payment %s", paymentIntentID)
		}
		delta := onlineRefundDelta(totalRefunded, p.Amount, p.Refunded)
		if delta == 0 {
			return nil
		}
		mop := models.AccountOnlineMOP
		comment := "Remboursement en ligne (réf. " + p.SessionID + ")"
		if len(comment) > 250 {
			comment = comment[:250]
		}
		op := customeraccount.Operation{
			MerchantID: p.MerchantID, CustomerID: p.CustomerID, Type: models.AccountOperationRefund, Amount: -delta,
			MOP: &mop, Comment: &comment, UserID: models.StripeWebhookUserID,
		}
		if err := customeraccount.ValidateOperation(op); err != nil {
			return err
		}
		if err := customeraccount.Lock(txCtx, p.MerchantID, p.CustomerID); err != nil {
			return err
		}
		id, _, err := customeraccount.Insert(txCtx, s.repo.DB(txCtx), op)
		if err != nil {
			return err
		}
		logger.FromContext(txCtx).Info("customer account: online refund recorded",
			zap.String("merchant_id", p.MerchantID), zap.Int64("customer_id", p.CustomerID),
			zap.Int64("amount", delta), zap.Int64("operation_id", id))
		return s.repo.SetOnlinePaymentRefunded(txCtx, p.SessionID, p.Refunded+delta)
	})
}

package payouts

import (
	"fmt"
	"time"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

// ErrNotReconciled : la somme des lignes du payout ne retombe pas sur son
// montant. Stripe n'a probablement pas fini de rapprocher le payout ; le
// traitement est retenté à la prochaine heure (puis envoyé tel quel avec son
// écart, voir reconcileRetries).
type ErrNotReconciled struct {
	PayoutAmount int64
	Discrepancy  int64
}

func (e *ErrNotReconciled) Error() string {
	return fmt.Sprintf("payout non rapproché : écart de %d centimes sur un payout de %d centimes", e.Discrepancy, e.PayoutAmount)
}

// isSale / isRefund classent une ligne de balance. Tout ce qui n'est ni l'un ni
// l'autre (litiges, ajustements, types inconnus) tombe dans Adjustments plutôt
// que de faire échouer le relevé.
func isSale(tx stripeclient.PayoutTransaction) bool {
	return tx.ReportingCategory == "charge" || tx.Type == "charge" || tx.Type == "payment"
}

func isRefund(tx stripeclient.PayoutTransaction) bool {
	return tx.ReportingCategory == "refund" || tx.Type == "refund" || tx.Type == "payment_refund"
}

// SalePaymentIntents renvoie les PaymentIntents des ventes d'un payout, à
// rattacher à leur canal avant Aggregate.
func SalePaymentIntents(txs []stripeclient.PayoutTransaction) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, tx := range txs {
		if !isSale(tx) || tx.PaymentIntentID == "" {
			continue
		}
		if _, ok := seen[tx.PaymentIntentID]; !ok {
			seen[tx.PaymentIntentID] = struct{}{}
			ids = append(ids, tx.PaymentIntentID)
		}
	}
	return ids
}

// span suit les dates extrêmes d'un ensemble de lignes.
type span struct{ first, last time.Time }

func (s *span) add(t time.Time) {
	if s.first.IsZero() || t.Before(s.first) {
		s.first = t
	}
	if t.After(s.last) {
		s.last = t
	}
}

// Aggregate additionne les lignes d'un payout en un relevé. channels donne le
// canal de chaque PaymentIntent ; une vente inconnue va dans ChannelOther.
// Le relevé est toujours produit : si les lignes ne retombent pas sur
// payoutAmount, l'écart est dans Summary.Discrepancy (à signaler, jamais caché).
func Aggregate(txs []stripeclient.PayoutTransaction, channels map[string]Channel, payoutAmount int64) *Summary {
	totals := map[Channel]*ChannelTotal{
		ChannelScanNOrder: {Channel: ChannelScanNOrder},
		ChannelKiosk:      {Channel: ChannelKiosk},
		ChannelOther:      {Channel: ChannelOther},
	}

	s := &Summary{Net: payoutAmount}
	var salesSpan, allSpan span

	for _, tx := range txs {
		if tx.Type == "payout" {
			continue // la ligne du payout lui-même, pas une de ses composantes
		}
		s.Commission += tx.CommissionFee
		s.StripeFees += tx.StripeFee
		allSpan.add(tx.Created)

		switch {
		case isSale(tx):
			channel, ok := channels[tx.PaymentIntentID]
			if !ok {
				channel = ChannelOther
			}
			totals[channel].Count++
			totals[channel].Amount += tx.Amount
			s.Sales += tx.Amount
			salesSpan.add(tx.Created)
		case isRefund(tx):
			s.Refunds += tx.Amount
			s.RefundCount++
		default:
			s.Adjustments += tx.Amount
		}
	}

	s.Discrepancy = payoutAmount - (s.Sales + s.Refunds + s.Adjustments - s.Commission - s.StripeFees)

	period := salesSpan
	if period.first.IsZero() {
		period = allSpan
	}
	s.PeriodStart, s.PeriodEnd = period.first, period.last

	for _, c := range []Channel{ChannelScanNOrder, ChannelKiosk, ChannelOther} {
		s.Channels = append(s.Channels, *totals[c])
	}
	return s
}

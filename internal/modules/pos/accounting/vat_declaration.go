package accounting

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	cashregisters "welloresto-api/internal/modules/cash_registers"

	"go.uber.org/zap"
)

// Déclaration de TVA (/accounting/vat/calculate, /accounting/vat/export-csv),
// alignée sur l'export comptable (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md) :
//   - même méthode, mois par mois selon le mode de clôture de l'établissement :
//     en MANUAL, TVA sur les lignes, remises de caisse déduites (catégories
//     affichées) ; en AUTO, TVA ventilée à partir des encaissements (toutes
//     catégories, total TTC = encaissements) ;
//   - mêmes canaux que l'export (orders.order_source), tous les canaux
//     déclarables (la déclaration couvre toutes les ventes, pas seulement le
//     périmètre historique de l'export manuel) ;
//   - dates de calendrier dans le fuseau de l'établissement, comme l'export ;
//   - commandes closes et vendues (models.VoidOrderBrandStatusesSQL), TVA figée
//     à la vente (migration 164).

// vatDeclarationRow est un total de la déclaration pour un mois, un canal, un
// type de commande et un taux.
type vatDeclarationRow struct {
	Month       string // YYYY-MM, calendrier de l'établissement
	ClosingMode string
	Channel     string // orders.order_source ("" si inconnu)
	OrderType   string // in, take_away, delivery
	Rate        float64
	TTCCents    int64
	HTCents     int64
	VATCents    int64
}

// declarationChannelAliases : anciennes valeurs de canal de la déclaration
// (avant 2026-10-05), toujours acceptées. « restaurant » couvrait tout ce qui
// n'était ni ScanNOrder, ni Uber Eats, ni Deliveroo : caisse et borne.
var declarationChannelAliases = map[string][]string{
	"restaurant": {"WELLO_RESTO_POS", "KIOSK"},
	"scannorder": {"SCANNORDER"},
	"ubereats":   {"UBER_EATS"},
	"deliveroo":  {"DELIVEROO"},
}

// normalizeDeclarationChannels valide les canaux demandés (valeurs de
// orders.order_source, ou anciennes valeurs, cf. declarationChannelAliases).
// Renvoie le filtre (nil = tous les canaux, commandes sans canal comprises) et
// les canaux à détailler dans by_channel.
func normalizeDeclarationChannels(values []string) (filter []string, keys []string, err error) {
	if len(values) == 0 {
		return nil, append([]string(nil), accountingOrderSources...), nil
	}
	known := map[string]bool{}
	for _, s := range accountingOrderSources {
		known[s] = true
	}
	seen := map[string]bool{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if aliases, ok := declarationChannelAliases[strings.ToLower(v)]; ok {
			for _, a := range aliases {
				seen[a] = true
			}
			continue
		}
		v = strings.ToUpper(v)
		if !known[v] {
			return nil, nil, fmt.Errorf("%w: canal %q", models.ErrInvalidInput, raw)
		}
		seen[v] = true
	}
	for _, s := range accountingOrderSources {
		if seen[s] {
			keys = append(keys, s)
		}
	}
	if len(keys) == len(accountingOrderSources) {
		return nil, keys, nil
	}
	return keys, keys, nil
}

// parseCalendarDate lit une date de calendrier (YYYY-MM-DD, ou un horodatage
// dont seule la date est gardée) dans le fuseau de l'établissement.
func parseCalendarDate(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 10 {
		raw = raw[:10]
	}
	return parseLocalDate(raw, loc)
}

// declarationSegment est une tranche de la période de même mois et de même
// mode de clôture.
type declarationSegment struct {
	from, to time.Time // [from, to[
}

// declarationSegments découpe [fromLocal, toExclusive[ aux débuts de mois et
// aux dates de changement de mode de clôture (changeDates, normalement
// toujours un 1er du mois — découpées quand même par prudence).
func declarationSegments(fromLocal, toExclusive time.Time, changeDates []time.Time) []declarationSegment {
	loc := fromLocal.Location()
	bounds := map[int64]time.Time{fromLocal.Unix(): fromLocal, toExclusive.Unix(): toExclusive}
	for m := time.Date(fromLocal.Year(), fromLocal.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0); m.Before(toExclusive); m = m.AddDate(0, 1, 0) {
		if m.After(fromLocal) {
			bounds[m.Unix()] = m
		}
	}
	for _, d := range changeDates {
		if d.After(fromLocal) && d.Before(toExclusive) {
			bounds[d.Unix()] = d
		}
	}
	sorted := make([]time.Time, 0, len(bounds))
	for _, b := range bounds {
		sorted = append(sorted, b)
	}
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Before(sorted[b]) })
	segments := make([]declarationSegment, 0, len(sorted)-1)
	for i := 0; i+1 < len(sorted); i++ {
		segments = append(segments, declarationSegment{from: sorted[i], to: sorted[i+1]})
	}
	return segments
}

// aggregateDeclarationShares totalise des parts nettes par (canal, type de
// commande, taux) ; HT arrondi par total, TVA = TTC − HT.
func aggregateDeclarationShares(month, closingMode string, shares []netVATShare) []vatDeclarationRow {
	type key struct {
		channel, orderType string
		rate               float64
	}
	totals := map[key]int64{}
	for _, s := range shares {
		totals[key{channel: s.Source, orderType: s.OrderType, rate: s.Rate}] += s.TTC
	}
	rows := make([]vatDeclarationRow, 0, len(totals))
	for k, ttc := range totals {
		ht := ttc
		if k.rate != 0 {
			ht = int64(math.Round(float64(ttc) * 100.0 / (100.0 + k.rate)))
		}
		rows = append(rows, vatDeclarationRow{
			Month: month, ClosingMode: closingMode, Channel: k.channel, OrderType: k.orderType,
			Rate: k.rate, TTCCents: ttc, HTCents: ht, VATCents: ttc - ht,
		})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].Channel != rows[b].Channel {
			return rows[a].Channel < rows[b].Channel
		}
		if rows[a].OrderType != rows[b].OrderType {
			return rows[a].OrderType < rows[b].OrderType
		}
		return rows[a].Rate < rows[b].Rate
	})
	return rows
}

// vatDeclarationRows calcule la déclaration de TVA de l'établissement sur
// [fromLocal, toExclusive[ pour le périmètre donné (canaux, types de
// commande), tranche par tranche selon le mode de clôture.
func (s *AccountingService) vatDeclarationRows(ctx context.Context, merchantID string, fromLocal, toExclusive time.Time, scope orderScope) ([]vatDeclarationRow, error) {
	history, err := s.cashRegistersRepo.ListClosingModes(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	loc := fromLocal.Location()
	var changeDates []time.Time
	for _, change := range history {
		if d, err := time.ParseInLocation("2006-01-02", change.EffectiveFrom, loc); err == nil {
			changeDates = append(changeDates, d)
		}
	}

	var rows []vatDeclarationRow
	for _, seg := range declarationSegments(fromLocal, toExclusive, changeDates) {
		mode, err := s.cashRegistersRepo.ResolveClosingMode(ctx, merchantID, seg.from)
		if err != nil {
			return nil, err
		}
		lines, err := s.repo.GetOrderVATLines(ctx, merchantID, seg.from, seg.to, scope)
		if err != nil {
			return nil, err
		}
		payments, err := s.repo.GetOrderPayments(ctx, merchantID, seg.from, seg.to, scope)
		if err != nil {
			return nil, err
		}

		var shares []netVATShare
		if mode == cashregisters.ClosingModeAuto {
			net := autoNetShares(lines, payments)
			if len(net.UnallocatedOrders) > 0 {
				logger.FromContext(ctx).Warn("VAT declaration: paid orders without VAT lines excluded",
					zap.String("merchant_id", merchantID),
					zap.Int64s("order_ids", net.UnallocatedOrders))
			}
			shares = net.Shares
		} else {
			all, _ := manualNetShares(lines, payments)
			for _, sh := range all {
				if sh.Reported {
					shares = append(shares, sh)
				}
			}
		}
		rows = append(rows, aggregateDeclarationShares(seg.from.Format("2006-01"), mode, shares)...)
	}
	return rows, nil
}

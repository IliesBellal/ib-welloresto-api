package accounting

import (
	"sort"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Calcul des montants de l'export comptable et de la déclaration de TVA à
// partir des lignes et des paiements de chaque commande — sans accès base,
// testable unitairement. Règles : docs/EXPORT_COMPTABLE_MODES_CLOTURE.md.
//
// Deux étapes : les parts TTC nettes de chaque commande (manualNetShares /
// autoNetShares), puis leur agrégation (par catégorie pour l'export, par mois,
// canal, type de commande et taux pour la déclaration).

// Les remises de caisse (models.IsDiscountMOP) ne sont pas de l'argent
// encaissé : déduites de la base TVA, exclues des encaissements.

// orderVATLine est une part TTC d'une commande à un taux donné : une ligne de
// commande (prix + suppléments) × quantité, ou les frais de livraison.
type orderVATLine struct {
	OrderID   int64
	Title     string
	Rate      float64
	Reported  bool   // catégorie affichée au rapport (tva_categories.show_in_report ; toujours vrai pour les frais)
	Source    string // orders.order_source ("" si inconnu)
	OrderType string // orders.order_type en minuscules (in, take_away, delivery)
	TTC       int64
}

// orderPaymentLine est le total d'un moyen de paiement actif sur une commande.
type orderPaymentLine struct {
	OrderID int64
	MOP     string
	Label   string
	Amount  int64
}

// DiscountSummary alimente la ligne d'information placée sous le tableau TVA,
// hors de tout total : « CA TTC avant remises · Remises accordées · CA TTC ».
type DiscountSummary struct {
	TTCBeforeDiscounts int64
	Discounts          int64
	TTC                int64
}

// netVATShare est une part TTC nette d'une commande à un taux : après déduction
// de sa remise (mode manuel), ou part de ses encaissements (mode automatique).
type netVATShare struct {
	Title     string
	Rate      float64
	Reported  bool
	Source    string
	OrderType string
	TTC       int64
}

type vatBucketKey struct {
	title    string
	rate     float64
	reported bool
}

type vatBucket struct {
	key       vatBucketKey
	source    string
	orderType string
	ttc       int64
}

// allocateLargestRemainder : cf. helpers.AllocateLargestRemainder (partagé
// avec le registre de caisse).
func allocateLargestRemainder(total int64, weights []int64) []int64 {
	return helpers.AllocateLargestRemainder(total, weights)
}

// bucketsByOrder regroupe les parts TTC par commande et par (titre, taux,
// affiché), dans un ordre déterministe.
func bucketsByOrder(lines []orderVATLine) (map[int64][]vatBucket, []int64) {
	idx := map[int64]map[vatBucketKey]int{}
	out := map[int64][]vatBucket{}
	var orderIDs []int64
	for _, l := range lines {
		if _, ok := out[l.OrderID]; !ok {
			idx[l.OrderID] = map[vatBucketKey]int{}
			orderIDs = append(orderIDs, l.OrderID)
		}
		key := vatBucketKey{title: l.Title, rate: l.Rate, reported: l.Reported}
		if i, ok := idx[l.OrderID][key]; ok {
			out[l.OrderID][i].ttc += l.TTC
			continue
		}
		idx[l.OrderID][key] = len(out[l.OrderID])
		out[l.OrderID] = append(out[l.OrderID], vatBucket{key: key, source: l.Source, orderType: l.OrderType, ttc: l.TTC})
	}
	for _, id := range orderIDs {
		buckets := out[id]
		sort.SliceStable(buckets, func(a, b int) bool {
			if buckets[a].key.title != buckets[b].key.title {
				return buckets[a].key.title < buckets[b].key.title
			}
			return buckets[a].key.rate < buckets[b].key.rate
		})
	}
	sort.Slice(orderIDs, func(a, b int) bool { return orderIDs[a] < orderIDs[b] })
	return out, orderIDs
}

func bucketWeights(buckets []vatBucket) []int64 {
	w := make([]int64, len(buckets))
	for i, b := range buckets {
		w[i] = b.ttc
	}
	return w
}

func shareOf(b vatBucket, ttc int64) netVATShare {
	return netVATShare{Title: b.key.title, Rate: b.key.rate, Reported: b.key.reported, Source: b.source, OrderType: b.orderType, TTC: ttc}
}

// vatRowsFromTotals convertit des totaux TTC par (titre, taux) en lignes TVA
// (HT et TVA déduits du TTC), triées par titre puis taux.
func vatRowsFromTotals(totals map[vatBucketKey]int64) []TVARow {
	rows := make([]TVARow, 0, len(totals))
	for key, ttc := range totals {
		row := TVARow{TVATitle: key.title, Rate: key.rate, TTC: float64(ttc)}
		if key.rate == 0 {
			row.HT = row.TTC
		} else {
			row.HT = row.TTC * (100.0 / (100.0 + key.rate))
			row.TVA = row.TTC - row.HT
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].TVATitle != rows[b].TVATitle {
			return rows[a].TVATitle < rows[b].TVATitle
		}
		return rows[a].Rate < rows[b].Rate
	})
	return rows
}

// manualNetShares calcule les parts du mode de clôture manuel : la TVA reste
// calculée sur les lignes (le vendu), mais les remises de caisse de chaque
// commande en sont déduites, réparties entre ses taux au prorata de leur TTC
// (plafonnées au total de la commande). La répartition se fait sur toutes les
// parts de la commande, catégories masquées comprises, pour ne pas reporter sur
// les catégories affichées la remise d'une catégorie masquée. Toutes les parts
// sont renvoyées (Reported indique si elles s'affichent) ; le résumé des
// remises ne porte que sur les parts affichées.
func manualNetShares(lines []orderVATLine, payments []orderPaymentLine) ([]netVATShare, DiscountSummary) {
	discountByOrder := map[int64]int64{}
	for _, p := range payments {
		if models.IsDiscountMOP(p.MOP) {
			discountByOrder[p.OrderID] += p.Amount
		}
	}

	buckets, orderIDs := bucketsByOrder(lines)
	var shares []netVATShare
	var summary DiscountSummary
	for _, id := range orderIDs {
		orderBuckets := buckets[id]
		var alloc []int64
		if discount := discountByOrder[id]; discount > 0 {
			weights := bucketWeights(orderBuckets)
			var orderTTC int64
			for _, w := range weights {
				if w > 0 {
					orderTTC += w
				}
			}
			if discount > orderTTC {
				discount = orderTTC
			}
			alloc = allocateLargestRemainder(discount, weights)
		}
		for i, b := range orderBuckets {
			share := int64(0)
			if alloc != nil {
				share = alloc[i]
			}
			shares = append(shares, shareOf(b, b.ttc-share))
			if b.key.reported {
				summary.TTCBeforeDiscounts += b.ttc
				summary.Discounts += share
			}
		}
	}
	summary.TTC = summary.TTCBeforeDiscounts - summary.Discounts
	return shares, summary
}

// buildManualVAT calcule le tableau TVA du mode de clôture manuel (catégories
// affichées seulement), cf. manualNetShares.
func buildManualVAT(lines []orderVATLine, payments []orderPaymentLine) ([]TVARow, DiscountSummary) {
	shares, summary := manualNetShares(lines, payments)
	totals := map[vatBucketKey]int64{}
	for _, s := range shares {
		if s.Reported {
			totals[vatBucketKey{title: s.Title, rate: s.Rate, reported: true}] += s.TTC
		}
	}
	return vatRowsFromTotals(totals), summary
}

// autoNetResult est le résultat de la ventilation des encaissements.
type autoNetResult struct {
	Shares []netVATShare
	// PaymentTotals : encaissements par libellé de moyen de paiement (remises
	// exclues), des seules commandes ventilées.
	PaymentTotals map[string]int64
	Summary       DiscountSummary
	// UnallocatedOrders : commandes payées sans aucune part TTC positive sur
	// laquelle répartir leurs paiements (aucune ligne, ou lignes toutes à 0 /
	// négatives). Exclues de tout pour garder l'égalité ; l'appelant les
	// journalise.
	UnallocatedOrders []int64
}

// autoNetShares calcule les parts du mode de clôture automatique : les
// paiements réels (remises exclues) de chaque commande sont répartis entre ses
// taux au prorata du TTC de ses lignes et frais de livraison. Le total des
// parts est donc, par construction, égal au total des encaissements. Toutes les
// catégories comptent, y compris celles masquées d'ordinaire (show_in_report) :
// chaque euro encaissé doit tomber dans un taux.
func autoNetShares(lines []orderVATLine, payments []orderPaymentLine) autoNetResult {
	type orderPayments struct {
		real     int64
		discount int64
		byLabel  map[string]int64
		labels   []string
	}
	byOrder := map[int64]*orderPayments{}
	var paidOrderIDs []int64
	for _, p := range payments {
		op, ok := byOrder[p.OrderID]
		if !ok {
			op = &orderPayments{byLabel: map[string]int64{}}
			byOrder[p.OrderID] = op
			paidOrderIDs = append(paidOrderIDs, p.OrderID)
		}
		if models.IsDiscountMOP(p.MOP) {
			op.discount += p.Amount
			continue
		}
		op.real += p.Amount
		if _, seen := op.byLabel[p.Label]; !seen {
			op.labels = append(op.labels, p.Label)
		}
		op.byLabel[p.Label] += p.Amount
	}
	sort.Slice(paidOrderIDs, func(a, b int) bool { return paidOrderIDs[a] < paidOrderIDs[b] })

	buckets, _ := bucketsByOrder(lines)
	result := autoNetResult{PaymentTotals: map[string]int64{}}
	for _, id := range paidOrderIDs {
		op := byOrder[id]
		if op.real == 0 {
			continue
		}
		orderBuckets := buckets[id]
		alloc := allocateLargestRemainder(op.real, bucketWeights(orderBuckets))
		if alloc == nil {
			result.UnallocatedOrders = append(result.UnallocatedOrders, id)
			continue
		}
		for i, b := range orderBuckets {
			if alloc[i] == 0 {
				continue
			}
			result.Shares = append(result.Shares, shareOf(b, alloc[i]))
		}
		for _, label := range op.labels {
			result.PaymentTotals[label] += op.byLabel[label]
		}
		result.Summary.TTC += op.real
		result.Summary.Discounts += op.discount
	}
	result.Summary.TTCBeforeDiscounts = result.Summary.TTC + result.Summary.Discounts
	return result
}

// autoReport est le résultat du mode de clôture automatique pour l'export.
type autoReport struct {
	TVARows           []TVARow
	Payments          []PaymentRow
	Summary           DiscountSummary
	UnallocatedOrders []int64
}

// buildAutoReport calcule l'export du mode de clôture automatique, cf.
// autoNetShares : total TTC du tableau TVA = total des encaissements.
func buildAutoReport(lines []orderVATLine, payments []orderPaymentLine) autoReport {
	net := autoNetShares(lines, payments)
	totals := map[vatBucketKey]int64{}
	for _, s := range net.Shares {
		totals[vatBucketKey{title: s.Title, rate: s.Rate, reported: true}] += s.TTC
	}
	report := autoReport{
		TVARows:           vatRowsFromTotals(totals),
		Summary:           net.Summary,
		UnallocatedOrders: net.UnallocatedOrders,
	}
	labels := make([]string, 0, len(net.PaymentTotals))
	for label := range net.PaymentTotals {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(a, b int) bool { return strings.ToLower(labels[a]) < strings.ToLower(labels[b]) })
	for _, label := range labels {
		report.Payments = append(report.Payments, PaymentRow{Label: label, Amount: net.PaymentTotals[label]})
	}
	return report
}

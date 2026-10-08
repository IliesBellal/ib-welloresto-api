package fiscal

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Ticket complet (lot D conformité caisse : docs/attestation-conformite-05-lot-D-brief.md,
// constat 2). Les lignes figées d'un ticket portaient le seul prix de chaque
// article : ni options payantes, ni suppléments, ni frais de livraison, ni
// remises de caisse, alors que sa TVA ventilée les compte. Le §50 du BOI
// demande le détail des articles avec le total HT de chaque ligne.

// SaleLine est une part de la vente d'une commande, lue en base au moment du
// ticket : un article, une option payante ou un supplément d'un article, ou
// les frais de livraison. Montants en centimes.
type SaleLine struct {
	Kind     string // models.SnapshotKind*
	Item     string // order_item_id de l'article (ou de l'article de l'option / du supplément) ; "" pour la livraison
	Label    string
	Quantity int64 // quantité facturée (pour une option : quantité de l'option × quantité de l'article)
	UnitTTC  int64
	Rate     float64
}

// TTC est le montant de la part.
func (l SaleLine) TTC() int64 { return l.UnitTTC * l.Quantity }

// SaleTaxLines ramène les parts à la forme attendue par BuildTaxDetails.
func SaleTaxLines(lines []SaleLine) []TaxLine {
	out := make([]TaxLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, TaxLine{Rate: l.Rate, TTC: l.TTC()})
	}
	return out
}

// BuildReceiptItems construit les lignes figées d'un ticket et sa TVA
// ventilée à partir des parts de la vente et du total des remises de caisse
// (même calcul que BuildTaxDetails, dont il reprend le résultat) :
//   - une ligne par part, dans l'ordre reçu ; les options et suppléments
//     suivent leur article et pointent vers lui (Parent) ;
//   - une ligne de remise par taux remisé (montant négatif), quand il y a une
//     remise de caisse ;
//   - à chaque taux, le HT des lignes est réparti au plus grand reste : la
//     somme des HT des lignes vaut exactement le HT de la TVA ventilée, et la
//     somme des TTC son TTC.
func BuildReceiptItems(lines []SaleLine, discount int64) ([]models.SnapshotItem, TaxDetails) {
	details := BuildTaxDetails(SaleTaxLines(lines), discount)
	net := map[float64]TaxDetail{}
	for _, d := range details.Lines {
		net[d.Rate] = d
	}

	// Parts par taux, dans l'ordre d'apparition des taux.
	byRate := map[float64][]int{}
	var rates []float64
	for i, l := range lines {
		if _, ok := byRate[l.Rate]; !ok {
			rates = append(rates, l.Rate)
		}
		byRate[l.Rate] = append(byRate[l.Rate], i)
	}
	sort.Float64s(rates)

	lineHT := make([]int64, len(lines))
	type discountLine struct {
		rate    float64
		ttc, ht int64
	}
	var discounts []discountLine
	for _, r := range rates {
		idx := byRate[r]
		var grossTTC int64
		for _, i := range idx {
			grossTTC += lines[i].TTC()
		}
		grossHT := htOf(grossTTC, r)

		// Parts négatives (rares) : HT propre ; le reste au prorata des parts
		// positives.
		weights := make([]int64, len(idx))
		remaining := grossHT
		for k, i := range idx {
			ttc := lines[i].TTC()
			if ttc < 0 {
				lineHT[i] = htOf(ttc, r)
				remaining -= lineHT[i]
				continue
			}
			weights[k] = ttc
		}
		if alloc := helpers.AllocateLargestRemainder(remaining, weights); alloc != nil {
			for k, i := range idx {
				if weights[k] > 0 {
					lineHT[i] = alloc[k]
				}
			}
		}

		n := net[r] // zéro si la remise absorbe tout le taux
		if d := grossTTC - n.TTC; d != 0 {
			discounts = append(discounts, discountLine{rate: r, ttc: -d, ht: n.HT - grossHT})
		}
	}

	items := make([]models.SnapshotItem, 0, len(lines)+len(discounts))
	articleRank := map[string]int{}
	for i, l := range lines {
		item := models.SnapshotItem{
			Name:      l.Label,
			Quantity:  int(l.Quantity),
			PriceTTC:  l.UnitTTC,
			TaxRate:   int64(math.Round(l.Rate * 100)),
			TaxAmount: l.UnitTTC - htOf(l.UnitTTC, l.Rate),
			Kind:      l.Kind,
			TotalTTC:  l.TTC(),
			TotalHT:   lineHT[i],
			TotalTVA:  l.TTC() - lineHT[i],
		}
		switch l.Kind {
		case models.SnapshotKindArticle:
			articleRank[l.Item] = len(items)
		case models.SnapshotKindOption, models.SnapshotKindSupplement:
			if rank, ok := articleRank[l.Item]; ok {
				item.Parent = &rank
			}
		}
		items = append(items, item)
	}
	for _, d := range discounts {
		name := "Remise"
		if len(discounts) > 1 {
			name = "Remise (TVA " + formatRate(d.rate) + " %)"
		}
		items = append(items, models.SnapshotItem{
			Name:      name,
			Quantity:  1,
			PriceTTC:  d.ttc,
			TaxRate:   int64(math.Round(d.rate * 100)),
			TaxAmount: d.ttc - d.ht,
			Kind:      models.SnapshotKindDiscount,
			TotalTTC:  d.ttc,
			TotalHT:   d.ht,
			TotalTVA:  d.ttc - d.ht,
		})
	}
	return items, details
}

// htOf déduit le HT d'un TTC à un taux, arrondi au centime (même formule que
// taxDetail).
func htOf(ttc int64, rate float64) int64 {
	return int64(math.Round(float64(ttc) * 100 / (100 + rate)))
}

// formatRate écrit un taux à la française (5,5 ; 10 ; 20).
func formatRate(rate float64) string {
	return strings.Replace(strconv.FormatFloat(rate, 'f', -1, 64), ".", ",", 1)
}

// SaleLinesTTCMismatch décrit l'écart entre le TTC de la commande (transmis
// par la caisse) et celui de la vente reconstituée, ou "" s'il n'y en a pas.
func SaleLinesTTCMismatch(orderTTC int64, details TaxDetails) string {
	if got := details.TotalTTC(); got != orderTTC {
		return fmt.Sprintf("TTC de la commande %d, lignes de vente nettes %d (écart %d)", orderTTC, got, orderTTC-got)
	}
	return ""
}

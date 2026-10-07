package fiscal

import (
	"encoding/json"
	"math"
	"sort"

	"welloresto-api/internal/helpers"
)

// Ventilation de TVA d'un ticket ou d'un avoir (constat C9, lot B conformité
// caisse) : receipts.tax_details valait '{}' sur tous les tickets, et l'avoir
// portait une seule ligne à TVA 0 avec HT = TTC.
//
// Même calcul que l'export comptable (pos/accounting, mode manuel) : parts TTC
// de la commande par taux — chaque ligne (prix + suppléments) × quantité au
// taux figé, les frais de livraison à leur propre taux —, remises de caisse
// (models.IsDiscountMOP) déduites au prorata des parts, au plus grand reste et
// plafonnées au total ; HT et TVA déduits du TTC net à chaque taux.

// TaxLine est une part TTC d'une commande à un taux (centimes).
type TaxLine struct {
	Rate float64
	TTC  int64
}

// TaxDetail est la ventilation d'un ticket à un taux, nette des remises de
// caisse (centimes).
type TaxDetail struct {
	Rate float64 `json:"rate"`
	TTC  int64   `json:"ttc"`
	HT   int64   `json:"ht"`
	TVA  int64   `json:"tva"`
}

// TaxDetails est le contenu de receipts.tax_details : ventilation par taux
// (triée par taux) et remise de caisse déduite (0 pour un avoir).
type TaxDetails struct {
	Lines    []TaxDetail `json:"lines"`
	Discount int64       `json:"discount"`
}

// TotalTTC, TotalHT et TotalTVA somment la ventilation.
func (d TaxDetails) TotalTTC() int64 { return d.sum(func(l TaxDetail) int64 { return l.TTC }) }
func (d TaxDetails) TotalHT() int64  { return d.sum(func(l TaxDetail) int64 { return l.HT }) }
func (d TaxDetails) TotalTVA() int64 { return d.sum(func(l TaxDetail) int64 { return l.TVA }) }

func (d TaxDetails) sum(f func(TaxDetail) int64) int64 {
	var t int64
	for _, l := range d.Lines {
		t += f(l)
	}
	return t
}

// BuildTaxDetails regroupe les parts par taux et en déduit la remise de
// caisse, plafonnée au total des parts, répartie au plus grand reste.
func BuildTaxDetails(lines []TaxLine, discount int64) TaxDetails {
	byRate := map[float64]int64{}
	for _, l := range lines {
		byRate[l.Rate] += l.TTC
	}
	rates := make([]float64, 0, len(byRate))
	for r := range byRate {
		rates = append(rates, r)
	}
	sort.Float64s(rates)

	weights := make([]int64, len(rates))
	var total int64
	for i, r := range rates {
		weights[i] = byRate[r]
		if weights[i] > 0 {
			total += weights[i]
		}
	}
	if discount < 0 {
		discount = 0
	}
	if discount > total {
		discount = total
	}
	var alloc []int64
	if discount > 0 {
		alloc = helpers.AllocateLargestRemainder(discount, weights)
	}

	out := TaxDetails{Lines: []TaxDetail{}, Discount: discount}
	for i, r := range rates {
		ttc := weights[i]
		if alloc != nil {
			ttc -= alloc[i]
		}
		if ttc == 0 {
			continue
		}
		out.Lines = append(out.Lines, taxDetail(r, ttc))
	}
	return out
}

// ProrateTaxDetails ventile amount (négatif pour un avoir) au prorata des
// parts TTC de la ventilation d'origine, au plus grand reste : la somme vaut
// exactement amount.
func ProrateTaxDetails(orig TaxDetails, amount int64) TaxDetails {
	out := TaxDetails{Lines: []TaxDetail{}}
	if amount == 0 || len(orig.Lines) == 0 {
		return out
	}
	weights := make([]int64, len(orig.Lines))
	for i, l := range orig.Lines {
		weights[i] = l.TTC
	}
	alloc := helpers.AllocateLargestRemainder(amount, weights)
	if alloc == nil {
		return out
	}
	for i, l := range orig.Lines {
		if alloc[i] == 0 {
			continue
		}
		out.Lines = append(out.Lines, taxDetail(l.Rate, alloc[i]))
	}
	return out
}

// ParseTaxDetails lit receipts.tax_details. ok vaut false pour une ventilation
// absente (tickets antérieurs au lot B : '{}') ou illisible.
func ParseTaxDetails(raw []byte) (TaxDetails, bool) {
	var d TaxDetails
	if err := json.Unmarshal(raw, &d); err != nil || len(d.Lines) == 0 {
		return TaxDetails{}, false
	}
	return d, true
}

// taxDetail déduit HT et TVA d'un TTC à un taux (arrondi au centime, la TVA
// étant le complément : HT + TVA = TTC exactement).
func taxDetail(rate float64, ttc int64) TaxDetail {
	ht := int64(math.Round(float64(ttc) * 100 / (100 + rate)))
	return TaxDetail{Rate: rate, TTC: ttc, HT: ht, TVA: ttc - ht}
}

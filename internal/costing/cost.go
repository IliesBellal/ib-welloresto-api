// Package costing holds the pure arithmetic for turning a recipe/option
// ingredient requirement (components.purchase_price, purchase_price_quantity,
// unit_of_measure, unit_of_measure_convert) into a cost in cents.
//
// Before this package existed the same three-line formula was duplicated in
// menu.GetAllProducts, menu.GetAllComponents and stocks.GetComponentsList
// (all display-only, admin-facing). Those call sites compute a price-per-unit
// they never persist, so a wrong/missing price only ever produces a wrong
// number on a screen. order_life_cycle's write path is different: its result
// is frozen onto orderitems.cost_price_unit forever, so this package refuses
// to guess — every function returns ok=false rather than silently falling
// back to a 1:1 conversion or a 0 cost when the data doesn't support a real
// answer. See docs/decisions.md (PROMPT 07 lot 1, B2) for why "NULL is not 0"
// is the central rule here.
package costing

import "math"

// Reason values for orderitems.cost_price_reason — why cost_price_unit is
// NULL. Kept here (not in order_life_cycle) since they're the vocabulary of
// what this package's callers can conclude, mirrored verbatim by the
// migration's CHECK constraint (114_write_path_instrumentation.up.sql).
const (
	ReasonNoRecipe         = "NO_RECIPE"
	ReasonIncompleteRecipe = "INCOMPLETE_RECIPE"
)

// UnitCost combines a unit conversion (requiredQty, in requiredUOM, into the
// component's own unit) with a purchase price (in the same unit) into a cost
// in cents. ok is false whenever the result can't be trusted: no usable
// purchase price/quantity, or requiredUOM/componentUOM differ with no
// unit_of_measure_convert ratio available (ratio nil or <= 0) — the caller
// must not assume a 1:1 fallback in that case.
func UnitCost(requiredQty float64, requiredUOM, componentUOM int, ratio *float64, purchasePriceCents int, purchasePriceQty float64) (costCents float64, ok bool) {
	converted, ok := ConvertedQuantity(requiredQty, requiredUOM, componentUOM, ratio)
	if !ok {
		return 0, false
	}
	perUnit, ok := PricePerUnit(purchasePriceCents, purchasePriceQty)
	if !ok {
		return 0, false
	}
	cost := converted * perUnit
	if math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, false
	}
	return cost, true
}

// ConvertedQuantity converts requiredQty (expressed in requiredUOM) into
// componentUOM. ratio is the unit_of_measure_convert row for
// (id_from=requiredUOM, id_to=componentUOM), matching the convention already
// used by menu.GetAllProducts (divide by ratio), not the opposite convention
// used by stocks.ConsumeOrderStock (which queries the reverse pair and
// multiplies) — both directions exist as reciprocal rows in
// unit_of_measure_convert, so either works, but a caller must not mix them.
func ConvertedQuantity(requiredQty float64, requiredUOM, componentUOM int, ratio *float64) (converted float64, ok bool) {
	if requiredUOM == componentUOM {
		return requiredQty, true
	}
	if ratio == nil || *ratio <= 0 {
		return 0, false
	}
	return requiredQty / *ratio, true
}

// PricePerUnit divides a purchase price (cents) by the quantity it was
// purchased for, both already in the same unit. purchase_price is
// NOT NULL DEFAULT 0 on components, so a price of 0 is indistinguishable in
// the schema from "never set" — this function treats <= 0 as "no usable
// price" rather than "free", per the same NULL-not-0 rule.
func PricePerUnit(purchasePriceCents int, purchasePriceQty float64) (centsPerUnit float64, ok bool) {
	if purchasePriceCents <= 0 || purchasePriceQty <= 0 {
		return 0, false
	}
	return float64(purchasePriceCents) / purchasePriceQty, true
}

// RoundToCents rounds a cost expressed in cents (typically the sum of
// several UnitCost results) to the nearest integer cent, for storage in an
// integer column.
func RoundToCents(costCents float64) int {
	return int(math.Round(costCents))
}

package analytics

import (
	"sort"
	"strings"

	"welloresto-api/internal/models"
)

// Order source keys accepted by OrderFilter.Sources — the exact values of
// orders.order_source (migrations/done/114_write_path_instrumentation.up.sql,
// written by order_life_cycle.resolveOrderSource and back-filled for the
// history). Reusing the column's own vocabulary rather than inventing a
// second one means the filter is a plain `= ANY(?)` on a real column, never a
// re-derivation from brand × created_by that could drift from the write path.
const (
	OrderSourcePOS        = "WELLO_RESTO_POS"
	OrderSourceKiosk      = "KIOSK"
	OrderSourceScanNOrder = "SCANNORDER"
	OrderSourceUberEats   = models.BrandUberEats
	OrderSourceDeliveroo  = models.BrandDeliveroo
)

// OrderSources lists every accepted source key in a fixed display order.
var OrderSources = []string{
	OrderSourcePOS,
	OrderSourceKiosk,
	OrderSourceScanNOrder,
	OrderSourceUberEats,
	OrderSourceDeliveroo,
}

// OrderTypes lists every accepted orders.order_type value in a fixed display
// order (models.OrderTypeIn/TakeAway/Delivery — sur place, à emporter,
// livraison).
var OrderTypes = []string{
	models.OrderTypeIn,
	models.OrderTypeTakeAway,
	models.OrderTypeDelivery,
}

// OrderFilter is the source × order-type filter the CA, Commandes, Produits,
// Options, Annulations and Vente additionnelle tabs accept. It is orthogonal
// to ChannelFilter (channels.go), which filters on the brand × order_type
// derivation the Clients/Remises tabs expose.
//
// A nil slice means "no restriction on this dimension". NewOrderFilter
// normalizes "every value selected" to nil too, so that a request with every
// box ticked is byte-for-byte the unfiltered query — including the rows whose
// order_source/order_type is NULL (≈0.01% of order_source after the
// back-fill, historical orders only for order_type), which no `= ANY(?)`
// could ever match. As soon as the caller actually narrows a dimension, those
// NULL rows fall out of it: they can't be attributed to the source/type the
// caller asked for, and guessing one would be worse than leaving them out.
type OrderFilter struct {
	Sources    []string
	OrderTypes []string
}

// NewOrderFilter validates the requested sources and order types — same
// validate-or-reject shape as ChannelFilter: an unknown value is a 400, never
// a silent drop. Empty or complete selections become nil (see OrderFilter).
func NewOrderFilter(sources, orderTypes []string) (OrderFilter, bool) {
	s, ok := normalizeFilterValues(sources, OrderSources)
	if !ok {
		return OrderFilter{}, false
	}
	t, ok := normalizeFilterValues(orderTypes, OrderTypes)
	if !ok {
		return OrderFilter{}, false
	}
	return OrderFilter{Sources: s, OrderTypes: t}, true
}

func normalizeFilterValues(requested, referential []string) ([]string, bool) {
	if len(requested) == 0 {
		return nil, true
	}
	valid := make(map[string]bool, len(referential))
	for _, v := range referential {
		valid[v] = true
	}
	seen := make(map[string]bool, len(requested))
	for _, v := range requested {
		if !valid[v] {
			return nil, false
		}
		seen[v] = true
	}
	if len(seen) == len(referential) {
		return nil, true
	}
	out := make([]string, 0, len(seen))
	for _, v := range referential {
		if seen[v] {
			out = append(out, v)
		}
	}
	return out, true
}

// IsZero reports whether the filter restricts nothing.
func (f OrderFilter) IsZero() bool {
	return len(f.Sources) == 0 && len(f.OrderTypes) == 0
}

// predicate returns the extra `AND ...` fragment (against alias `o`, like
// every scope in scope.go) and its args, or "" and nil when the filter is
// zero.
func (f OrderFilter) predicate() (string, []interface{}) {
	var b strings.Builder
	var args []interface{}
	if len(f.Sources) > 0 {
		b.WriteString("\n\tAND o.order_source = ANY(?)")
		args = append(args, f.Sources)
	}
	if len(f.OrderTypes) > 0 {
		b.WriteString("\n\tAND o.order_type = ANY(?)")
		args = append(args, f.OrderTypes)
	}
	return b.String(), args
}

// cacheKeySuffix is appended to a tab's cache key. Empty for a zero filter,
// so every pre-existing cache key (and the non-regression tests pinned on
// them) is unchanged when no filter is applied.
func (f OrderFilter) cacheKeySuffix() string {
	if f.IsZero() {
		return ""
	}
	sources := append([]string(nil), f.Sources...)
	sort.Strings(sources)
	types := append([]string(nil), f.OrderTypes...)
	sort.Strings(types)
	return ":src=" + strings.Join(sources, ",") + ":type=" + strings.Join(types, ",")
}

// upsellSuggestionChannels maps the source filter onto
// upsell_suggestions.channel (POS/SNO/KIOSK — internal/modules/upsell's
// Channel* constants). Marketplace sources have no suggestion channel: a
// filter restricted to Uber Eats/Deliveroo only yields an empty (non-nil)
// list, i.e. zero suggestions, which is the truth — neither platform ever
// shows our upsell UI. Returns nil when the source dimension is unrestricted.
func (f OrderFilter) upsellSuggestionChannels() []string {
	if len(f.Sources) == 0 {
		return nil
	}
	out := []string{}
	for _, s := range f.Sources {
		switch s {
		case OrderSourcePOS:
			out = append(out, "POS")
		case OrderSourceScanNOrder:
			out = append(out, "SNO")
		case OrderSourceKiosk:
			out = append(out, "KIOSK")
		}
	}
	return out
}

// WithOrderFilter returns a shallow copy of r whose every scope-based query
// also applies f (see applyOrderFilter). The receiver is never mutated, so the
// shared *Repository the Service holds stays unfiltered for every other
// request and tab.
func (r *Repository) WithOrderFilter(f OrderFilter) *Repository {
	cp := *r
	cp.orderFilter = f
	return &cp
}

// applyOrderFilter appends the repository's OrderFilter predicate to a scope
// WHERE fragment and its args — called right after every AnalyticsOrdersScope*/
// AnalyticsCancellationsScope*/AnalyticsAllOrdersCreatedScope* call (and the
// upsell line scopes), so the pair stays positionally consistent however the
// caller then composes the rest of the query. A no-op for a zero filter.
func (r *Repository) applyOrderFilter(where string, args []interface{}) (string, []interface{}) {
	pred, predArgs := r.orderFilter.predicate()
	if pred == "" {
		return where, args
	}
	out := make([]interface{}, 0, len(args)+len(predArgs))
	out = append(out, args...)
	return where + pred, append(out, predArgs...)
}

// OrderFilterRequest is embedded in the request body of every endpoint that
// accepts an OrderFilter (its fields are flattened into the JSON object).
// Both lists are optional: empty means every value — see NewOrderFilter.
type OrderFilterRequest struct {
	// Sources filters on orders.order_source (OrderSources): WELLO_RESTO_POS
	// (caisse), KIOSK (borne), SCANNORDER, UBER_EATS, DELIVEROO.
	Sources []string `json:"sources,omitempty"`
	// OrderTypes filters on orders.order_type (OrderTypes): IN (sur place),
	// TAKE_AWAY (à emporter), DELIVERY (livraison).
	OrderTypes []string `json:"order_types,omitempty"`
}
